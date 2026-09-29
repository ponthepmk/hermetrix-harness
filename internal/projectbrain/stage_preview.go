package projectbrain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/taskengine"
)

// StageEligibility contains only metadata the local task owner needs to
// choose a passing check and immutable evidence. It never includes CAS bytes.
type StageEligibility struct {
	TaskID       string                  `json:"task_id"`
	Title        string                  `json:"title"`
	State        string                  `json:"state"`
	TaskRevision int                     `json:"task_revision"`
	Eligible     bool                    `json:"eligible"`
	Reason       string                  `json:"reason,omitempty"`
	Validations  []StageValidationChoice `json:"validations"`
}

type StageValidationChoice struct {
	ID              string                `json:"id"`
	RequirementID   string                `json:"requirement_id"`
	CheckID         string                `json:"check_id"`
	SubjectRevision string                `json:"subject_revision"`
	ObservedAt      string                `json:"observed_at"`
	Artifacts       []StageArtifactChoice `json:"artifacts"`
}

type StageArtifactChoice struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ContentDigest   string `json:"content_digest"`
	Visibility      string `json:"visibility"`
	ExportPolicy    string `json:"export_policy"`
	SharingRevision int    `json:"sharing_revision"`
	Eligible        bool   `json:"eligible"`
}

// StagePreview is an exact, owner-reviewable draft and approval template.
// Returning it does not approve, queue, submit, or promote any knowledge.
type StagePreview struct {
	Candidate           KnowledgeCandidate      `json:"candidate"`
	Approval            CandidateExportApproval `json:"approval"`
	SelectedArtifactIDs []string                `json:"selected_artifact_ids"`
}

// Eligibility exposes the current owner-bound completed task and the latest
// passing validations of its active requirements. Source changes may race this
// preview; Stage always rechecks the committed state before queueing.
func (s StageService) Eligibility(ctx context.Context, taskID string) (StageEligibility, error) {
	actor := identity.Principal(ctx)
	if s.Store == nil || !candidateAtom(actor) || !candidateAtom(taskID) ||
		!candidateAtom(s.LocalProjectID) || !candidateAtom(s.PiProjectID) {
		return StageEligibility{}, errors.New("owner and Project Brain binding are required")
	}
	tx, err := s.Store.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return StageEligibility{}, err
	}
	defer tx.Rollback()
	var projectID, title, state, taskVisibility, taskExport, projectState, projectVisibility, projectExport, projectOwner string
	var revision, activeRequirement int
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(t.project_id,''),t.title,t.state,t.revision,
		t.active_requirement_revision,t.visibility,t.export_policy,p.state,p.visibility,p.export_policy,
		p.owner_principal_id FROM durable_tasks t JOIN projects p ON p.id=t.project_id
		WHERE t.id=? AND t.owner_principal_id=?`, taskID, actor).Scan(&projectID, &title, &state,
		&revision, &activeRequirement, &taskVisibility, &taskExport, &projectState,
		&projectVisibility, &projectExport, &projectOwner)
	if err != nil {
		return StageEligibility{}, fmt.Errorf("owner task unavailable: %w", err)
	}
	if projectOwner != actor {
		return StageEligibility{}, ErrExportRevoked
	}
	result := StageEligibility{TaskID: taskID, Title: title, State: state,
		TaskRevision: revision, Validations: []StageValidationChoice{}}
	if projectID != s.LocalProjectID {
		result.Reason = "Task belongs to another local project"
	} else if state != taskengine.StateCompleted {
		result.Reason = "Task is not completed"
	} else if projectState != "active" {
		result.Reason = "Project is not active"
	} else if projectVisibility != "project_shared" || projectExport != "explicit_selection" {
		result.Reason = "Project export is disabled"
	} else if taskVisibility != "project_shared" || taskExport != "explicit_selection" {
		result.Reason = "Task export is disabled"
	} else if activeRequirement < 1 {
		result.Reason = "Task has no active requirement revision"
	} else {
		result.Eligible = true
	}
	if !result.Eligible {
		if err := tx.Commit(); err != nil {
			return StageEligibility{}, err
		}
		return result, nil
	}
	var criteriaJSON string
	if err := tx.QueryRowContext(ctx, `SELECT criteria_json FROM task_requirement_revisions
		WHERE task_id=? AND revision=?`, taskID, activeRequirement).Scan(&criteriaJSON); err != nil {
		return StageEligibility{}, err
	}
	var criteria []taskengine.Criterion
	if err := json.Unmarshal([]byte(criteriaJSON), &criteria); err != nil {
		return StageEligibility{}, err
	}
	criteriaIDs := make(map[string]bool, len(criteria))
	for _, criterion := range criteria {
		criteriaIDs[criterion.ID] = true
	}
	subject := taskengine.RequirementSubjectRevision(taskengine.Task{
		ID: taskID, ActiveRequirementRevision: activeRequirement})
	rows, err := tx.QueryContext(ctx, `SELECT v.id,v.requirement_id,v.check_id,v.subject_revision,
		v.evidence_refs_json,v.created_at FROM task_validations v WHERE v.task_id=?
		AND v.status='pass' AND v.subject_revision=? AND COALESCE(v.requirement_id,'')<>''
		AND NOT EXISTS (SELECT 1 FROM task_validations newer WHERE newer.task_id=v.task_id
		AND newer.requirement_id=v.requirement_id AND newer.check_id=v.check_id
		AND newer.subject_revision=v.subject_revision AND
		(newer.created_at>v.created_at OR
		(newer.created_at=v.created_at AND newer.rowid>v.rowid)))
		ORDER BY v.created_at DESC,v.rowid DESC LIMIT 101`, taskID, subject)
	if err != nil {
		return StageEligibility{}, err
	}
	artifactChoices := 0
	for rows.Next() {
		var choice StageValidationChoice
		var refsJSON string
		if err := rows.Scan(&choice.ID, &choice.RequirementID, &choice.CheckID,
			&choice.SubjectRevision, &refsJSON, &choice.ObservedAt); err != nil {
			rows.Close()
			return StageEligibility{}, err
		}
		if !criteriaIDs[choice.RequirementID] {
			continue
		}
		var refs []string
		if err := json.Unmarshal([]byte(refsJSON), &refs); err != nil {
			rows.Close()
			return StageEligibility{}, err
		}
		if len(refs) > 100 {
			rows.Close()
			return StageEligibility{}, errors.New("validation evidence selection exceeds limit")
		}
		choice.Artifacts = make([]StageArtifactChoice, 0, len(refs))
		seenArtifacts := map[string]bool{}
		for _, ref := range refs {
			id, ok := strings.CutPrefix(ref, "artifact:")
			if !ok || !candidateAtom(id) || seenArtifacts[id] {
				continue
			}
			seenArtifacts[id] = true
			artifactChoices++
			if artifactChoices > 100 {
				rows.Close()
				return StageEligibility{}, errors.New("artifact selection exceeds limit")
			}
			// A separate read connection cannot be used inside the same SQLite
			// cursor on a single-connection store. Collect IDs first below.
			choice.Artifacts = append(choice.Artifacts, StageArtifactChoice{ID: id})
		}
		result.Validations = append(result.Validations, choice)
		if len(result.Validations) > 100 {
			rows.Close()
			return StageEligibility{}, errors.New("passing validation selection exceeds limit")
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return StageEligibility{}, err
	}
	rows.Close()
	for vi := range result.Validations {
		for ai := range result.Validations[vi].Artifacts {
			artifact := &result.Validations[vi].Artifacts[ai]
			var project, checksum, owner string
			err := tx.QueryRowContext(ctx, `SELECT COALESCE(project_id,''),name,checksum,
				owner_principal_id,visibility,export_policy,sharing_revision FROM artifacts
				WHERE id=? AND owner_principal_id=?`, artifact.ID, actor).Scan(&project,
				&artifact.Name, &checksum, &owner, &artifact.Visibility,
				&artifact.ExportPolicy, &artifact.SharingRevision)
			if err != nil {
				// Missing or foreign artifacts are excluded from a selectable
				// export instead of exposing their names or storage details.
				artifact.ID = ""
				continue
			}
			artifact.ContentDigest = "sha256:" + checksum
			artifact.Eligible = project == projectID && owner == actor &&
				artifact.Visibility == "project_shared" &&
				artifact.ExportPolicy == "explicit_selection" &&
				candidateDigestPattern.MatchString(artifact.ContentDigest)
		}
		filtered := result.Validations[vi].Artifacts[:0]
		for _, artifact := range result.Validations[vi].Artifacts {
			if artifact.ID != "" {
				filtered = append(filtered, artifact)
			}
		}
		result.Validations[vi].Artifacts = filtered
	}
	selectable := false
	for _, validation := range result.Validations {
		for _, artifact := range validation.Artifacts {
			if artifact.Eligible {
				selectable = true
				break
			}
		}
	}
	if !selectable {
		result.Eligible = false
		result.Reason = "No current passed validation cites exportable immutable evidence"
	}
	if err := tx.Commit(); err != nil {
		return StageEligibility{}, err
	}
	return result, nil
}

// Preview generates the exact selected payload and approval template without
// writing to the outbox. The owner must explicitly submit StageInput later.
func (s StageService) Preview(ctx context.Context, taskID, validationID string,
	selectedArtifactIDs []string, solution, verificationKind string) (StagePreview, error) {
	actor := identity.Principal(ctx)
	if !candidateAtom(actor) || !candidateAtom(validationID) || !verificationKinds[verificationKind] ||
		len(selectedArtifactIDs) == 0 || len(selectedArtifactIDs) > 8 {
		return StagePreview{}, errors.New("invalid owner preview selection")
	}
	eligibility, err := s.Eligibility(ctx, taskID)
	if err != nil {
		return StagePreview{}, err
	}
	if !eligibility.Eligible {
		return StagePreview{}, ErrExportRevoked
	}
	var selectedValidation *StageValidationChoice
	for i := range eligibility.Validations {
		if eligibility.Validations[i].ID == validationID {
			selectedValidation = &eligibility.Validations[i]
			break
		}
	}
	if selectedValidation == nil {
		return StagePreview{}, errors.New("selected validation is not a current passing check")
	}
	artifactsByID := make(map[string]StageArtifactChoice, len(selectedValidation.Artifacts))
	for _, artifact := range selectedValidation.Artifacts {
		artifactsByID[artifact.ID] = artifact
	}
	approval := CandidateExportApproval{Actor: actor, ProjectID: s.PiProjectID,
		HarnessTaskID: taskID, TaskRevision: eligibility.TaskRevision,
		ValidationID: validationID, CheckID: selectedValidation.CheckID,
		SubjectRevision: selectedValidation.SubjectRevision, VerificationKind: verificationKind,
		EvidenceDigests: map[string]string{}, EvidenceRevisions: map[string]int{},
		ApprovedAt: time.Now().UTC()}
	selectedSet := map[string]bool{}
	for _, id := range selectedArtifactIDs {
		artifact, ok := artifactsByID[id]
		if !ok || !artifact.Eligible || selectedSet[id] {
			return StagePreview{}, errors.New("selected artifact is unavailable for explicit export")
		}
		selectedSet[id] = true
		approval.EvidenceDigests[id] = artifact.ContentDigest
		approval.EvidenceRevisions[id] = artifact.SharingRevision
	}
	// Only task text is read here; readSelectedFacts below verifies every
	// snapshot field and the exact CAS bytes before this preview is returned.
	var title, problem, objective string
	var taskRevision, projectSharing, taskSharing int
	if err := s.Store.DB.QueryRowContext(ctx, `SELECT t.title,t.original_request,t.objective,
		t.revision,p.sharing_revision,t.sharing_revision FROM durable_tasks t
		JOIN projects p ON p.id=t.project_id WHERE t.id=? AND t.owner_principal_id=?`,
		taskID, actor).Scan(&title, &problem, &objective, &taskRevision,
		&projectSharing, &taskSharing); err != nil {
		return StagePreview{}, err
	}
	if strings.TrimSpace(problem) == "" {
		problem = objective
	}
	approval.TaskRevision = taskRevision
	approval.ProjectSharingRevision = projectSharing
	approval.TaskSharingRevision = taskSharing
	approval.ContentDigest, err = CandidateExportContentDigest(CommittedTaskFacts{
		ProjectID: s.PiProjectID, HarnessTaskID: taskID, TaskRevision: taskRevision,
		Title: title, Problem: problem, Solution: solution})
	if err != nil {
		return StagePreview{}, err
	}
	task, check, err := s.readSelectedFacts(ctx, taskID, validationID,
		selectedArtifactIDs, solution, verificationKind, approval)
	if err != nil {
		return StagePreview{}, err
	}
	candidate, err := BuildKnowledgeCandidate(task, check, approval)
	if err != nil {
		return StagePreview{}, err
	}
	return StagePreview{Candidate: candidate, Approval: approval,
		SelectedArtifactIDs: append([]string(nil), selectedArtifactIDs...)}, nil
}

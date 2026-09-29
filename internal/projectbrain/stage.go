package projectbrain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

// StageService binds one local project to one Pi project and a single
// credential identity. It never executes work or contacts Pi. The caller must
// be the trusted owner principal bound by the ingress boundary.
type StageService struct {
	Store          *store.Store
	Outbox         *CandidateOutbox
	LocalProjectID string
	PiProjectID    string
	OriginNodeID   string
	AgentID        string
}

// StageInput is one owner-selected export, not a background learning trigger.
// Title/problem come from the committed task; the owner supplies the concise
// solution and explicitly selects the immutable artifacts and check to cite.
type StageInput struct {
	TaskID              string
	ValidationID        string
	SelectedArtifactIDs []string
	Solution            string
	VerificationKind    string
	Approval            CandidateExportApproval
}

// Stage constructs and queues only an inert KnowledgeCandidate. The outbox
// keeps the exact approval for AuthorizeQueued to recheck before each send.
func (s StageService) Stage(ctx context.Context, input StageInput) (CandidateDelivery, error) {
	if s.Outbox == nil || s.Outbox.DB == nil || s.Store == nil || s.Outbox.DB != s.Store.DB {
		return CandidateDelivery{}, errors.New("Project Brain staging and outbox require the same local store")
	}
	if identity.Principal(ctx) == "" || identity.Principal(ctx) != input.Approval.Actor {
		return CandidateDelivery{}, errors.New("explicit Project Brain export requires the bound owner principal")
	}
	if input.ValidationID != input.Approval.ValidationID {
		return CandidateDelivery{}, errors.New("selected validation does not match owner approval")
	}
	// A Preview is a draft. The explicit owner POST is the approval event.
	// Persist its actual time, independent of when the draft was displayed.
	approval := input.Approval
	approval.ApprovedAt = time.Now().UTC()
	task, check, err := s.readSelectedFacts(ctx, input.TaskID, input.ValidationID,
		input.SelectedArtifactIDs, input.Solution, input.VerificationKind, approval)
	if err != nil {
		return CandidateDelivery{}, err
	}
	candidate, err := BuildKnowledgeCandidate(task, check, approval)
	if err != nil {
		return CandidateDelivery{}, err
	}
	payload, err := json.Marshal(candidate)
	if err != nil {
		return CandidateDelivery{}, err
	}
	message := CandidateMessage{CandidateID: candidate.CandidateID, ProjectID: candidate.ProjectID,
		TaskID: task.HarnessTaskID, PayloadDigest: candidate.PayloadDigest,
		Payload: payload, Approval: approval}
	if err := s.AuthorizeQueued(ctx, message); err != nil {
		return CandidateDelivery{}, err
	}
	return s.Outbox.Queue(ctx, message)
}

// AuthorizeQueued is the durable outbox's pre-send callback. A later privacy
// revocation, sharing revision change, stale validation, missing CAS object,
// or altered payload blocks delivery even after the candidate was queued.
func (s StageService) AuthorizeQueued(ctx context.Context, message CandidateMessage) error {
	if s.Store == nil || message.TaskID != message.Approval.HarnessTaskID ||
		message.ProjectID != s.PiProjectID || !json.Valid(message.Payload) {
		return ErrExportRevoked
	}
	var persisted KnowledgeCandidate
	if err := json.Unmarshal(message.Payload, &persisted); err != nil {
		return ErrExportRevoked
	}
	selected := make([]string, 0, len(message.Approval.EvidenceDigests))
	for id := range message.Approval.EvidenceDigests {
		selected = append(selected, id)
	}
	sort.Strings(selected)
	task, check, err := s.readSelectedFacts(ctx, message.TaskID, message.Approval.ValidationID,
		selected, persisted.Solution, persisted.Verification.Kind, message.Approval)
	if err != nil {
		return fmt.Errorf("%w: current source no longer matches selection", ErrExportRevoked)
	}
	rebuilt, err := BuildKnowledgeCandidate(task, check, message.Approval)
	if err != nil || rebuilt.CandidateID != message.CandidateID || rebuilt.PayloadDigest != message.PayloadDigest ||
		rebuilt.CandidateID != persisted.CandidateID || rebuilt.PayloadDigest != persisted.PayloadDigest {
		return ErrExportRevoked
	}
	return nil
}

func (s StageService) readSelectedFacts(ctx context.Context, taskID, validationID string,
	selectedIDs []string, solution, verificationKind string, approval CandidateExportApproval) (CommittedTaskFacts, CommittedVerification, error) {
	if s.Store == nil || !candidateAtom(s.LocalProjectID) || !candidateAtom(s.PiProjectID) ||
		!candidateAtom(s.OriginNodeID) || !candidateAtom(s.AgentID) || !candidateAtom(taskID) ||
		!candidateAtom(validationID) || approval.ProjectID != s.PiProjectID ||
		approval.HarnessTaskID != taskID || !candidateAtom(approval.Actor) ||
		len(selectedIDs) == 0 || len(selectedIDs) > 8 {
		return CommittedTaskFacts{}, CommittedVerification{}, errors.New("incomplete Project Brain project binding or selection")
	}
	tx, err := s.Store.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return CommittedTaskFacts{}, CommittedVerification{}, err
	}
	defer tx.Rollback()
	var localProject, title, objective, originalRequest, taskState, updated, taskOwner string
	var taskVisibility, taskExport, projectState, projectVisibility, projectExport, projectOwner string
	var taskRevision, activeRequirement, taskSharing, projectSharing int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(t.project_id,''),t.title,t.objective,t.original_request,
		t.state,t.revision,t.active_requirement_revision,t.updated_at,t.owner_principal_id,
		t.visibility,t.export_policy,t.sharing_revision,p.state,p.visibility,p.export_policy,
		p.sharing_revision,p.owner_principal_id FROM durable_tasks t JOIN projects p ON p.id=t.project_id
		WHERE t.id=? AND t.owner_principal_id=?`, taskID, approval.Actor).Scan(&localProject,
		&title, &objective, &originalRequest, &taskState, &taskRevision, &activeRequirement,
		&updated, &taskOwner, &taskVisibility, &taskExport, &taskSharing, &projectState,
		&projectVisibility, &projectExport, &projectSharing, &projectOwner); err != nil {
		return CommittedTaskFacts{}, CommittedVerification{}, fmt.Errorf("selected task and project are unavailable: %w", err)
	}
	if localProject != s.LocalProjectID || taskOwner != approval.Actor || projectOwner != approval.Actor ||
		projectState != "active" || taskState != taskengine.StateCompleted ||
		taskVisibility != "project_shared" || taskExport != "explicit_selection" ||
		projectVisibility != "project_shared" || projectExport != "explicit_selection" ||
		taskRevision != approval.TaskRevision || taskSharing != approval.TaskSharingRevision ||
		projectSharing != approval.ProjectSharingRevision || activeRequirement < 1 {
		return CommittedTaskFacts{}, CommittedVerification{}, ErrExportRevoked
	}
	completedAt, err := time.Parse(time.RFC3339Nano, updated)
	if err != nil || approval.ApprovedAt.Before(completedAt) {
		return CommittedTaskFacts{}, CommittedVerification{}, ErrExportRevoked
	}
	problem := originalRequest
	if strings.TrimSpace(problem) == "" {
		problem = objective
	}
	task := CommittedTaskFacts{HarnessTaskID: taskID, TaskRevision: taskRevision, State: taskState,
		ProjectID: s.PiProjectID, OriginNodeID: s.OriginNodeID, AgentID: s.AgentID,
		Title: title, Problem: problem, Solution: solution, CompletedAt: completedAt,
		ProjectVisibility: projectVisibility, ProjectExportPolicy: projectExport,
		ProjectSharingRevision: projectSharing, Visibility: taskVisibility,
		ExportPolicy: taskExport, SharingRevision: taskSharing}
	var criteriaJSON string
	if err := tx.QueryRowContext(ctx, `SELECT criteria_json FROM task_requirement_revisions
		WHERE task_id=? AND revision=?`, taskID, activeRequirement).Scan(&criteriaJSON); err != nil {
		return CommittedTaskFacts{}, CommittedVerification{}, err
	}
	var criteria []taskengine.Criterion
	if json.Unmarshal([]byte(criteriaJSON), &criteria) != nil {
		return CommittedTaskFacts{}, CommittedVerification{}, errors.New("active task criteria are invalid")
	}
	var requirementID, checkID, subjectRevision, status, evidenceJSON, observed string
	if err := tx.QueryRowContext(ctx, `SELECT requirement_id,check_id,subject_revision,status,
		evidence_refs_json,created_at FROM task_validations WHERE id=? AND task_id=?`, validationID, taskID).
		Scan(&requirementID, &checkID, &subjectRevision, &status, &evidenceJSON, &observed); err != nil {
		return CommittedTaskFacts{}, CommittedVerification{}, fmt.Errorf("selected validation is unavailable: %w", err)
	}
	activeCriterion := false
	for _, criterion := range criteria {
		if criterion.ID == requirementID {
			activeCriterion = true
			break
		}
	}
	if !activeCriterion || subjectRevision != taskengine.RequirementSubjectRevision(taskengine.Task{
		ID: taskID, ActiveRequirementRevision: activeRequirement}) || status != taskengine.ValidationPass ||
		approval.ValidationID != validationID || approval.CheckID != checkID || approval.SubjectRevision != subjectRevision {
		return CommittedTaskFacts{}, CommittedVerification{}, ErrExportRevoked
	}
	var newestID, newestStatus string
	if err := tx.QueryRowContext(ctx, `SELECT id,status FROM task_validations WHERE task_id=? AND
		requirement_id=? AND check_id=? AND subject_revision=? ORDER BY created_at DESC,rowid DESC LIMIT 1`,
		taskID, requirementID, checkID, subjectRevision).Scan(&newestID, &newestStatus); err != nil ||
		newestID != validationID || newestStatus != taskengine.ValidationPass {
		return CommittedTaskFacts{}, CommittedVerification{}, ErrExportRevoked
	}
	observedAt, err := time.Parse(time.RFC3339Nano, observed)
	if err != nil || observedAt.After(completedAt) {
		return CommittedTaskFacts{}, CommittedVerification{}, ErrExportRevoked
	}
	var cited []string
	if json.Unmarshal([]byte(evidenceJSON), &cited) != nil {
		return CommittedTaskFacts{}, CommittedVerification{}, errors.New("validation citations are invalid")
	}
	citedSet := make(map[string]bool, len(cited))
	for _, ref := range cited {
		citedSet[ref] = true
	}
	selectedSet := make(map[string]bool, len(selectedIDs))
	evidence := make([]CommittedEvidence, 0, len(selectedIDs))
	for _, id := range selectedIDs {
		if !candidateAtom(id) || selectedSet[id] || !citedSet["artifact:"+id] {
			return CommittedTaskFacts{}, CommittedVerification{}, errors.New("selected artifact is not uniquely cited by current passing validation")
		}
		selectedSet[id] = true
		var artifactProject, blobRef, checksum, artifactCreated, artifactOwner, visibility, exportPolicy string
		var sharingRevision int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(project_id,''),blob_ref,checksum,created_at,
			owner_principal_id,visibility,export_policy,sharing_revision FROM artifacts WHERE id=? AND owner_principal_id=?`,
			id, approval.Actor).Scan(&artifactProject, &blobRef, &checksum, &artifactCreated,
			&artifactOwner, &visibility, &exportPolicy, &sharingRevision); err != nil {
			return CommittedTaskFacts{}, CommittedVerification{}, fmt.Errorf("selected artifact is unavailable: %w", err)
		}
		if artifactProject != localProject || artifactOwner != approval.Actor ||
			visibility != "project_shared" || exportPolicy != "explicit_selection" ||
			blobRef != checksum || len(checksum) != 64 ||
			approval.EvidenceDigests[id] != "sha256:"+checksum ||
			approval.EvidenceRevisions[id] != sharingRevision {
			return CommittedTaskFacts{}, CommittedVerification{}, ErrExportRevoked
		}
		createdAt, parseErr := time.Parse(time.RFC3339Nano, artifactCreated)
		if parseErr != nil || createdAt.After(observedAt) {
			return CommittedTaskFacts{}, CommittedVerification{}, ErrExportRevoked
		}
		content, readErr := s.Store.Blobs.Get(blobRef)
		if readErr != nil {
			return CommittedTaskFacts{}, CommittedVerification{}, fmt.Errorf("selected immutable artifact is unavailable: %w", readErr)
		}
		evidence = append(evidence, CommittedEvidence{ID: id, Type: "artifact", Content: content,
			ContentDigest: "sha256:" + checksum, CreatedAt: createdAt,
			HarnessRef: "artifact:" + id, Visibility: visibility,
			ExportPolicy: exportPolicy, SharingRevision: sharingRevision})
	}
	if len(approval.EvidenceDigests) != len(selectedSet) || len(approval.EvidenceRevisions) != len(selectedSet) {
		return CommittedTaskFacts{}, CommittedVerification{}, ErrExportRevoked
	}
	if err := tx.Commit(); err != nil {
		return CommittedTaskFacts{}, CommittedVerification{}, err
	}
	check := CommittedVerification{CheckID: checkID, SubjectRevision: subjectRevision,
		Kind: verificationKind, Outcome: status, ObservedAt: observedAt, Evidence: evidence}
	return task, check, nil
}

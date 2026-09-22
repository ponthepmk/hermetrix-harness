package taskengine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

const PlanningClassifierRevision = 1

type PlanningDecision struct {
	ID                 string         `json:"id"`
	TaskID             string         `json:"task_id"`
	TaskRevision       int            `json:"task_revision"`
	ClassifierRevision int            `json:"classifier_revision"`
	Decision           string         `json:"decision"`
	Evidence           map[string]any `json:"evidence"`
	CreatedAt          time.Time      `json:"created_at"`
}

type ClassifyPlanningInput struct {
	TaskID               string   `json:"task_id"`
	ExpectedTaskRevision int      `json:"expected_task_revision"`
	Actor                string   `json:"actor"`
	AllowedFileScope     []string `json:"allowed_file_scope,omitempty"`
	Checks               []string `json:"checks,omitempty"`
	EffectScope          []string `json:"effect_scope,omitempty"`
}

// ClassifyPlanning is deliberately rule based. Its persisted evidence is safe
// to show to a user and never contains model reasoning.
func (s *Service) ClassifyPlanning(ctx context.Context, input ClassifyPlanningInput) (PlanningDecision, Task, error) {
	if strings.TrimSpace(input.Actor) == "" {
		return PlanningDecision{}, Task{}, fmt.Errorf("planning actor is required")
	}
	task, err := s.Get(ctx, input.TaskID)
	if err != nil {
		return PlanningDecision{}, Task{}, err
	}
	if task.Revision != input.ExpectedTaskRevision {
		return PlanningDecision{}, task, ErrStaleRevision
	}
	files, checks, effects := cleanStrings(input.AllowedFileScope), cleanStrings(input.Checks), cleanStrings(input.EffectScope)
	decision := "planner_required"
	reasons := []string{}
	if validActivePlan(task) {
		decision = "existing_plan"
		reasons = append(reasons, "active plan covers every acceptance criterion and has bounded checks")
	} else if len(task.Requirement.Unknowns) == 0 && len(files) > 0 && len(files) <= 8 && len(checks) > 0 && len(checks) <= 8 && len(effects) <= 8 {
		decision = "deterministic_plan"
		reasons = append(reasons, "no declared unknowns", "explicit bounded file scope", "explicit bounded checks")
	} else {
		if len(task.Requirement.Unknowns) > 0 {
			reasons = append(reasons, "requirements contain unresolved unknowns")
		}
		if len(files) == 0 || len(files) > 8 {
			reasons = append(reasons, "file scope is missing or unbounded")
		}
		if len(checks) == 0 || len(checks) > 8 {
			reasons = append(reasons, "acceptance checks are missing or unbounded")
		}
	}
	evidence := map[string]any{"reasons": reasons, "allowed_file_scope": files, "checks": checks,
		"effect_scope": effects, "requirement_revision": task.ActiveRequirementRevision, "active_plan_revision": task.ActivePlanRevision}
	encoded, _ := json.Marshal(evidence)
	now := time.Now().UTC()
	item := PlanningDecision{ID: identity.New("planclass"), TaskID: task.ID, TaskRevision: task.Revision,
		ClassifierRevision: PlanningClassifierRevision, Decision: decision, Evidence: evidence, CreatedAt: now}
	inserted, err := s.store.DB.ExecContext(ctx, `INSERT INTO task_planning_decisions
		(id,task_id,task_revision,classifier_revision,decision,evidence_json,created_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(task_id,task_revision,classifier_revision) DO NOTHING`, item.ID, item.TaskID, item.TaskRevision,
		item.ClassifierRevision, item.Decision, string(encoded), formatTime(now))
	if err != nil {
		return PlanningDecision{}, task, err
	}
	if changed, _ := inserted.RowsAffected(); changed == 0 {
		var storedEvidence, created string
		err = s.store.DB.QueryRowContext(ctx, `SELECT id,decision,evidence_json,created_at FROM task_planning_decisions
			WHERE task_id=? AND task_revision=? AND classifier_revision=?`, task.ID, task.Revision, PlanningClassifierRevision).
			Scan(&item.ID, &item.Decision, &storedEvidence, &created)
		if err != nil {
			return PlanningDecision{}, task, err
		}
		_ = json.Unmarshal([]byte(storedEvidence), &item.Evidence)
		item.CreatedAt, _ = parseTime(created)
		return item, task, nil
	}
	if decision != "deterministic_plan" {
		return item, task, nil
	}
	requirementIDs := make([]string, 0, len(task.Requirement.Criteria))
	for _, criterion := range task.Requirement.Criteria {
		requirementIDs = append(requirementIDs, criterion.ID)
	}
	planned, err := s.CreatePlan(ctx, CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Reason: "deterministic classifier revision 1: explicit bounded scope and checks",
		Actor: strings.TrimSpace(input.Actor), Steps: []StepSpec{{Key: "execute", Title: task.Title,
			Instructions: task.Objective + "\nAllowed files: " + strings.Join(files, ", "), RequirementIDs: requirementIDs,
			Checks: checks, EffectScope: effects}}})
	return item, planned, err
}

func validActivePlan(task Task) bool {
	if task.ActivePlanRevision < 1 || len(task.Plan.Steps) == 0 {
		return false
	}
	covered := map[string]bool{}
	for _, step := range task.Plan.Steps {
		if len(step.Checks) == 0 || len(step.Checks) > 16 || len(step.EffectScope) > 16 {
			return false
		}
		for _, id := range step.RequirementIDs {
			covered[id] = true
		}
	}
	for _, criterion := range task.Requirement.Criteria {
		if !covered[criterion.ID] {
			return false
		}
	}
	return true
}

type ConfirmedFailureInput struct {
	AttemptID       string   `json:"attempt_id"`
	FailureKind     string   `json:"failure_kind"`
	Message         string   `json:"message"`
	DiffArtifactID  string   `json:"diff_artifact_id,omitempty"`
	EvidenceRefs    []string `json:"evidence_refs,omitempty"`
	PriorChangeRefs []string `json:"prior_change_refs,omitempty"`
}

type EscalationDecision struct {
	FailureID           string `json:"failure_id"`
	NormalizedSignature string `json:"normalized_signature"`
	ConsecutiveCount    int    `json:"consecutive_count"`
	EscalationID        string `json:"escalation_id,omitempty"`
	EscalationOrdinal   int    `json:"escalation_ordinal,omitempty"`
	Action              string `json:"action"`
}

var volatileFailureText = regexp.MustCompile(`(?i)([a-z]:\\[^\s:]+|/[^\s:]+|0x[0-9a-f]+|\b\d+\b)`)

func NormalizeFailureSignature(kind, message string) string {
	stable := strings.ToLower(strings.TrimSpace(kind)) + ":" + strings.Join(strings.Fields(volatileFailureText.ReplaceAllString(message, "#")), " ")
	sum := sha256.Sum256([]byte(stable))
	return hex.EncodeToString(sum[:])
}

// RecordConfirmedFailure records execution evidence exactly once per attempt.
// Transport/configuration failures block immediately. Uncertain effects must be
// reconciled before the failure is eligible for escalation.
func (s *Service) RecordConfirmedFailure(ctx context.Context, input ConfirmedFailureInput) (EscalationDecision, error) {
	kind := strings.TrimSpace(input.FailureKind)
	if kind != "execution" && kind != "test" && kind != "transport" && kind != "configuration" && kind != "uncertain_effect" {
		return EscalationDecision{}, fmt.Errorf("unsupported failure kind %q", kind)
	}
	if strings.TrimSpace(input.AttemptID) == "" || strings.TrimSpace(input.Message) == "" {
		return EscalationDecision{}, fmt.Errorf("attempt and failure message are required")
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return EscalationDecision{}, err
	}
	defer tx.Rollback()
	var taskID, stepID string
	if err = tx.QueryRowContext(ctx, `SELECT task_id,step_id FROM task_step_attempts WHERE id=? AND state='failed'`, input.AttemptID).Scan(&taskID, &stepID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EscalationDecision{}, fmt.Errorf("attempt is not a confirmed failure")
		}
		return EscalationDecision{}, err
	}
	var unresolved int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_effect_intents WHERE attempt_id=? AND state IN ('dispatched','uncertain')`, input.AttemptID).Scan(&unresolved); err != nil {
		return EscalationDecision{}, err
	}
	if unresolved > 0 || kind == "uncertain_effect" {
		return EscalationDecision{Action: "reconcile_required"}, fmt.Errorf("attempt has unresolved effects; reconcile before escalation")
	}
	signature := NormalizeFailureSignature(kind, input.Message)
	evidenceJSON, _ := json.Marshal(cleanStrings(input.EvidenceRefs))
	priorJSON, _ := json.Marshal(cleanStrings(input.PriorChangeRefs))
	now := time.Now().UTC()
	failureID := identity.New("stepfail")
	_, err = tx.ExecContext(ctx, `INSERT INTO task_step_failures(id,task_id,step_id,attempt_id,normalized_signature,failure_kind,
		diff_artifact_id,evidence_refs_json,prior_change_refs_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, failureID, taskID,
		stepID, input.AttemptID, signature, kind, nullable(input.DiffArtifactID), string(evidenceJSON), string(priorJSON), formatTime(now))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return EscalationDecision{}, fmt.Errorf("failure evidence already recorded for attempt")
		}
		return EscalationDecision{}, err
	}
	result := EscalationDecision{FailureID: failureID, NormalizedSignature: signature, ConsecutiveCount: 1, Action: "retry_worker"}
	if kind == "transport" || kind == "configuration" {
		result.Action = "blocked_resolution"
		_, err = tx.ExecContext(ctx, `UPDATE durable_tasks SET state='paused',pause_reason=?,revision=revision+1,updated_at=? WHERE id=?`,
			kind+"_resolution_required:"+signature, formatTime(now), taskID)
		if err == nil {
			err = tx.Commit()
		}
		return result, err
	}
	var previous, previousID string
	_ = tx.QueryRowContext(ctx, `SELECT normalized_signature FROM task_step_failures
		WHERE step_id=? AND id<>? AND created_at > COALESCE((SELECT MAX(created_at) FROM task_step_escalations WHERE step_id=?),'')
		ORDER BY created_at DESC,id DESC LIMIT 1`, stepID, failureID, stepID).Scan(&previous)
	if previous != signature {
		if err = tx.Commit(); err != nil {
			return result, err
		}
		return result, nil
	}
	result.ConsecutiveCount = 2
	var escalations int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_step_escalations WHERE step_id=?`, stepID).Scan(&escalations); err != nil {
		return result, err
	}
	if escalations >= 2 {
		result.Action = "escalation_limit"
		_, err = tx.ExecContext(ctx, `UPDATE durable_tasks SET state='paused',pause_reason=?,revision=revision+1,updated_at=? WHERE id=?`,
			"planner_escalation_limit:"+signature, formatTime(now), taskID)
		if err == nil {
			err = tx.Commit()
		}
		return result, err
	}
	result.EscalationID, result.EscalationOrdinal, result.Action = identity.New("escalation"), escalations+1, "planner_required"
	_ = tx.QueryRowContext(ctx, `SELECT id FROM task_step_failures WHERE step_id=? AND normalized_signature=? AND id<>?
		AND created_at > COALESCE((SELECT MAX(created_at) FROM task_step_escalations WHERE step_id=?),'')
		ORDER BY created_at DESC,id DESC LIMIT 1`, stepID, signature, failureID, stepID).Scan(&previousID)
	evidence := map[string]any{"failure_ids": []string{previousID, failureID}, "evidence_refs": cleanStrings(input.EvidenceRefs),
		"prior_change_refs": cleanStrings(input.PriorChangeRefs), "diff_artifact_id": strings.TrimSpace(input.DiffArtifactID)}
	escalationEvidence, _ := json.Marshal(evidence)
	_, err = tx.ExecContext(ctx, `INSERT INTO task_step_escalations(id,task_id,step_id,normalized_signature,ordinal,state,evidence_json,created_at)
		VALUES(?,?,?,?,?,'awaiting_plan',?,?)`, result.EscalationID, taskID, stepID, signature, result.EscalationOrdinal,
		string(escalationEvidence), formatTime(now))
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE durable_tasks SET state='paused',pause_reason=?,revision=revision+1,updated_at=? WHERE id=?`,
			fmt.Sprintf("planner_escalation_required:%d:%s", result.EscalationOrdinal, signature), formatTime(now), taskID)
	}
	if err == nil {
		err = tx.Commit()
	}
	return result, err
}

type PlannerEscalationContext struct {
	ID                  string   `json:"id"`
	StepID              string   `json:"step_id"`
	NormalizedSignature string   `json:"normalized_signature"`
	Ordinal             int      `json:"ordinal"`
	FailureIDs          []string `json:"failure_ids"`
	EvidenceRefs        []string `json:"evidence_refs"`
	PriorChangeRefs     []string `json:"prior_change_refs"`
	DiffArtifactID      string   `json:"diff_artifact_id,omitempty"`
}

func (s *Service) PendingEscalation(ctx context.Context, taskID string) (*PlannerEscalationContext, error) {
	var item PlannerEscalationContext
	var evidence string
	err := s.store.DB.QueryRowContext(ctx, `SELECT id,step_id,normalized_signature,ordinal,evidence_json FROM task_step_escalations
		WHERE task_id=? AND state='awaiting_plan' ORDER BY created_at DESC LIMIT 1`, taskID).
		Scan(&item.ID, &item.StepID, &item.NormalizedSignature, &item.Ordinal, &evidence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var decoded struct {
		FailureIDs      []string `json:"failure_ids"`
		EvidenceRefs    []string `json:"evidence_refs"`
		PriorChangeRefs []string `json:"prior_change_refs"`
		DiffArtifactID  string   `json:"diff_artifact_id"`
	}
	if err = json.Unmarshal([]byte(evidence), &decoded); err != nil {
		return nil, fmt.Errorf("decode escalation evidence: %w", err)
	}
	item.FailureIDs, item.EvidenceRefs, item.PriorChangeRefs, item.DiffArtifactID = decoded.FailureIDs,
		decoded.EvidenceRefs, decoded.PriorChangeRefs, decoded.DiffArtifactID
	return &item, nil
}

func (s *Service) ResolvePendingEscalation(ctx context.Context, taskID, plannerRunID string, planRevision int) error {
	result, err := s.store.DB.ExecContext(ctx, `UPDATE task_step_escalations SET state='resolved',planner_run_id=?,plan_revision=?,resolved_at=?
		WHERE id=(SELECT id FROM task_step_escalations WHERE task_id=? AND state='awaiting_plan' ORDER BY created_at DESC LIMIT 1)`,
		plannerRunID, planRevision, formatTime(time.Now().UTC()), taskID)
	if err != nil {
		return err
	}
	_, _ = result.RowsAffected()
	return nil
}

// MarkReviewerRequired exposes the blocked post-review gate without changing
// the run authority that an eventual eligible reviewer must still use.
func (s *Service) MarkReviewerRequired(ctx context.Context, proposalID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "reviewer_required"
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE durable_tasks SET pause_reason=?,updated_at=? WHERE id=(
		SELECT task_id FROM task_code_proposals WHERE id=? AND state='awaiting_post_review')`, reason,
		formatTime(time.Now().UTC()), proposalID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("proposal is not awaiting post-review")
	}
	return nil
}

func (s *Service) ClearReviewerRequired(ctx context.Context, proposalID string) error {
	_, err := s.store.DB.ExecContext(ctx, `UPDATE durable_tasks SET pause_reason='',updated_at=? WHERE id=(
		SELECT task_id FROM task_code_proposals WHERE id=?) AND pause_reason LIKE 'reviewer_required%'`,
		formatTime(time.Now().UTC()), proposalID)
	return err
}

package taskengine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

func planSubjectHash(plan PlanRevision) (string, error) {
	specs := make([]StepSpec, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		specs = append(specs, step.StepSpec)
	}
	body, err := json.Marshal(struct {
		ID                  string
		TaskID              string
		Revision            int
		RequirementRevision int
		Steps               []StepSpec
	}{plan.ID, plan.TaskID, plan.Revision, plan.RequirementRevision, specs})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum[:]), nil
}

func planReviewDecision(ctx context.Context, reader completionReader, task Task) (string, string, error) {
	if !task.Requirement.Contract.RequirePlanReview {
		return "not_required", "", nil
	}
	var verdict, reason string
	err := reader.QueryRowContext(ctx, `SELECT verdict,reason FROM task_plan_reviews WHERE task_id=? AND plan_revision=? AND requirement_revision=? AND subject_hash=? ORDER BY rowid DESC LIMIT 1`,
		task.ID, task.ActivePlanRevision, task.ActiveRequirementRevision, task.Plan.SubjectHash).Scan(&verdict, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return "pending", "", nil
	}
	return verdict, reason, err
}

func requireApprovedPlan(ctx context.Context, tx *sql.Tx, taskID string, planRevision int) error {
	var contractJSON string
	if err := tx.QueryRowContext(ctx, `SELECT r.contract_json FROM task_plan_revisions p JOIN task_requirement_revisions r
		ON r.task_id=p.task_id AND r.revision=p.requirement_revision WHERE p.task_id=? AND p.revision=?`, taskID, planRevision).Scan(&contractJSON); err != nil {
		return err
	}
	var contract GoalContract
	if err := json.Unmarshal([]byte(contractJSON), &contract); err != nil {
		return err
	}
	if !contract.RequirePlanReview {
		return nil
	}
	var verdict string
	err := tx.QueryRowContext(ctx, `SELECT verdict FROM task_plan_reviews WHERE task_id=? AND plan_revision=? ORDER BY rowid DESC LIMIT 1`,
		taskID, planRevision).Scan(&verdict)
	if errors.Is(err, sql.ErrNoRows) || verdict != "approved" {
		return fmt.Errorf("the current plan requires approval before work starts")
	}
	return err
}

// RejectedPlanFeedback supplies the latest review note for the active plan.
func (s *Service) RejectedPlanFeedback(ctx context.Context, taskID string, planRevision int) (string, error) {
	var reason string
	err := s.store.DB.QueryRowContext(ctx, `SELECT reason FROM task_plan_reviews WHERE task_id=? AND plan_revision=? AND verdict='rejected' ORDER BY rowid DESC LIMIT 1`, taskID, planRevision).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return reason, err
}

func (s *Service) ReviewPlan(ctx context.Context, taskID string, expectedRevision int, subjectHash, actor, verdict, reason string) (Task, error) {
	actor, verdict, reason = strings.TrimSpace(actor), strings.TrimSpace(verdict), strings.TrimSpace(reason)
	if actor == "" || reason == "" || len([]rune(reason)) > 2000 || (verdict != "approved" && verdict != "rejected") {
		return Task{}, fmt.Errorf("plan review requires actor, reason and approve/reject verdict")
	}
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	if task.Revision != expectedRevision {
		return Task{}, ErrStaleRevision
	}
	if !task.Requirement.Contract.RequirePlanReview || task.ActivePlanRevision == 0 ||
		task.Plan.ReviewStatus != "pending" || task.State != StateWaitingForInput {
		return Task{}, fmt.Errorf("the current plan is not awaiting review")
	}
	if subjectHash == "" || subjectHash != task.Plan.SubjectHash {
		return Task{}, ErrStaleRevision
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	state, pauseReason := StateReady, ""
	if verdict == "rejected" {
		state, pauseReason = StateDraft, "plan rejected; revise the goal or create a new plan"
	}
	now := formatTime(time.Now().UTC())
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_plan_reviews(id,task_id,plan_revision,requirement_revision,subject_hash,verdict,actor,reason,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, identity.New("planreview"), task.ID, task.ActivePlanRevision, task.ActiveRequirementRevision,
		subjectHash, verdict, actor, reason, now); err != nil {
		return Task{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE durable_tasks SET state=?,pause_reason=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND active_plan_revision=?`,
		state, pauseReason, now, task.ID, expectedRevision, task.ActivePlanRevision)
	if err != nil {
		return Task{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Task{}, ErrStaleRevision
	}
	if err := tx.Commit(); err != nil {
		return Task{}, err
	}
	return s.Get(ctx, taskID)
}

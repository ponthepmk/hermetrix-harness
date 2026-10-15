package taskengine

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type PauseRunInput struct {
	RunID                string `json:"run_id"`
	LeaseToken           string `json:"lease_token"`
	ExpectedTaskRevision int    `json:"expected_task_revision"`
	Actor                string `json:"actor"`
	Reason               string `json:"reason"`
}

// PauseIdleRun is a boundary stop, never cancellation of an uncertain effect.
// It permits a deliberate goal revision after a completed step without waiting
// for an otherwise renewable lease to expire or inventing a failed attempt.
func (s *Service) PauseIdleRun(ctx context.Context, input PauseRunInput) (Task, error) {
	input.Actor, input.Reason = strings.TrimSpace(input.Actor), strings.TrimSpace(input.Reason)
	if input.RunID == "" || input.LeaseToken == "" || input.ExpectedTaskRevision < 1 || input.Actor == "" || len([]rune(input.Actor)) > 200 || input.Reason == "" || len([]rune(input.Reason)) > 2000 {
		return Task{}, fmt.Errorf("run authority, expected revision, actor and bounded reason are required")
	}
	run, err := s.getRun(ctx, input.RunID)
	if err != nil {
		return Task{}, err
	}
	task, err := s.Get(ctx, run.TaskID)
	if err != nil {
		return Task{}, err
	} // owner scope before mutation
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	var revision, plan int
	var runState, taskState, token, expiry string
	err = tx.QueryRowContext(ctx, `SELECT t.revision,t.active_plan_revision,t.state,r.state,r.lease_token,r.lease_expires_at FROM task_runs r JOIN durable_tasks t ON t.id=r.task_id WHERE r.id=? AND t.owner_principal_id=?`, run.ID, task.OwnerPrincipalID).Scan(&revision, &plan, &taskState, &runState, &token, &expiry)
	if err != nil {
		return Task{}, err
	}
	if revision != input.ExpectedTaskRevision {
		return Task{}, ErrStaleRevision
	}
	expires, _ := parseTime(expiry)
	if taskState != StateRunning || runState != RunRunning || token != input.LeaseToken || !expires.After(time.Now().UTC()) || plan != run.PlanRevision {
		return Task{}, fmt.Errorf("run authority is stale or expired")
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM task_step_attempts WHERE run_id=? AND state='running'`,
		`SELECT COUNT(*) FROM task_effect_intents WHERE run_id=? AND state IN ('planned','dispatched','uncertain')`,
		`SELECT COUNT(*) FROM task_code_proposals p JOIN task_step_attempts a ON a.id=p.attempt_id WHERE a.run_id=? AND p.state NOT IN ('verified','rejected','post_review_rejected','apply_failed','verification_failed')`,
	} {
		var count int
		if err = tx.QueryRowContext(ctx, query, run.ID).Scan(&count); err != nil {
			return Task{}, err
		}
		if count > 0 {
			return Task{}, fmt.Errorf("finish or reconcile the active step before pausing at the boundary")
		}
	}
	now := formatTime(time.Now().UTC())
	reason := input.Reason + " · " + input.Actor
	if _, err = tx.ExecContext(ctx, `UPDATE task_runs SET state='paused',stop_reason=?,lease_generation=lease_generation+1,lease_expires_at=?,updated_at=? WHERE id=?`, reason, now, now, run.ID); err != nil {
		return Task{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE durable_tasks SET state='paused',pause_reason=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, reason, now, task.ID, revision)
	if err != nil {
		return Task{}, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return Task{}, ErrStaleRevision
	}
	if err = tx.Commit(); err != nil {
		return Task{}, err
	}
	return s.Get(ctx, task.ID)
}

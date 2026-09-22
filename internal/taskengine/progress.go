package taskengine

import (
	"context"
	"fmt"
)

const (
	MaxTaskAttempts        = 20
	MaxPlannerEscalations  = 2
	MaxConsecutiveFailures = 2
)

// ProgressSnapshot derives the bounded-loop budget from durable receipts. It
// is observability only: it cannot start, retry, or escalate work.
type ProgressSnapshot struct {
	TaskID                        string   `json:"task_id"`
	TaskRevision                  int      `json:"task_revision"`
	Attempts                      int      `json:"attempts"`
	FailedAttempts                int      `json:"failed_attempts"`
	ConfirmedFailures             int      `json:"confirmed_failures"`
	ConsecutiveEquivalentFailures int      `json:"consecutive_equivalent_failures"`
	PlannerEscalations            int      `json:"planner_escalations"`
	UnresolvedEffects             int      `json:"unresolved_effects"`
	RemainingAttempts             int      `json:"remaining_attempts"`
	RemainingPlannerEscalations   int      `json:"remaining_planner_escalations"`
	NoProgress                    bool     `json:"no_progress"`
	BudgetExhausted               bool     `json:"budget_exhausted"`
	BlockedReasons                []string `json:"blocked_reasons"`
}

func (s *Service) TaskProgress(ctx context.Context, taskID string, expectedRevision int) (ProgressSnapshot, error) {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return ProgressSnapshot{}, err
	}
	if expectedRevision > 0 && task.Revision != expectedRevision {
		return ProgressSnapshot{}, ErrStaleRevision
	}
	item := ProgressSnapshot{TaskID: task.ID, TaskRevision: task.Revision}
	queries := []struct {
		query string
		dest  *int
	}{
		{`SELECT COUNT(*) FROM task_step_attempts WHERE task_id=?`, &item.Attempts},
		{`SELECT COUNT(*) FROM task_step_attempts WHERE task_id=? AND state='failed'`, &item.FailedAttempts},
		{`SELECT COUNT(*) FROM task_step_failures WHERE task_id=?`, &item.ConfirmedFailures},
		{`SELECT COUNT(*) FROM task_step_escalations WHERE task_id=?`, &item.PlannerEscalations},
		{`SELECT COUNT(*) FROM task_effect_intents WHERE task_id=? AND state IN ('dispatched','uncertain')`, &item.UnresolvedEffects},
	}
	for _, entry := range queries {
		if err = s.store.DB.QueryRowContext(ctx, entry.query, task.ID).Scan(entry.dest); err != nil {
			return ProgressSnapshot{}, fmt.Errorf("derive task progress: %w", err)
		}
	}
	item.ConsecutiveEquivalentFailures, err = s.consecutiveFailureCount(ctx, task)
	if err != nil {
		return ProgressSnapshot{}, err
	}
	item.RemainingAttempts = max(0, MaxTaskAttempts-item.Attempts)
	item.RemainingPlannerEscalations = max(0, MaxPlannerEscalations-item.PlannerEscalations)
	item.NoProgress = item.ConsecutiveEquivalentFailures >= MaxConsecutiveFailures
	item.BudgetExhausted = item.RemainingAttempts == 0 || item.RemainingPlannerEscalations == 0 && item.NoProgress
	if item.UnresolvedEffects > 0 {
		item.BlockedReasons = append(item.BlockedReasons, "unresolved_effects")
	}
	if item.NoProgress {
		item.BlockedReasons = append(item.BlockedReasons, "repeated_equivalent_failure")
	}
	if item.RemainingAttempts == 0 {
		item.BlockedReasons = append(item.BlockedReasons, "attempt_budget_exhausted")
	}
	if item.RemainingPlannerEscalations == 0 {
		item.BlockedReasons = append(item.BlockedReasons, "planner_budget_exhausted")
	}
	return item, nil
}

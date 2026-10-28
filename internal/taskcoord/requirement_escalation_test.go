package taskcoord

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hermetrix-harness/internal/taskengine"
)

func failTwiceForRequirementEscalation(t *testing.T, ts *taskengine.Service, task taskengine.Task) taskengine.Task {
	t.Helper()
	ctx := context.Background()
	var last taskengine.EscalationDecision
	for index := 1; index <= 2; index++ {
		run, err := ts.BeginRun(ctx, taskengine.BeginRunInput{
			TaskID:               task.ID,
			ExpectedTaskRevision: task.Revision,
			Owner:                "runner",
			LeaseDuration:        time.Minute,
		})
		if err != nil {
			t.Fatalf("begin run %d: %v", index, err)
		}
		task, err = ts.Get(ctx, task.ID)
		if err != nil {
			t.Fatalf("reload task after begin run %d: %v", index, err)
		}
		step := task.Plan.Steps[0]
		attempt, err := ts.BeginStepAttempt(ctx, taskengine.BeginAttemptInput{
			RunID:                run.ID,
			LeaseToken:           run.LeaseToken,
			StepKey:              step.Key,
			ExpectedTaskRevision: task.Revision,
			ExpectedStepRevision: step.Revision,
			InputHash:            fmt.Sprintf("packet-%d", index),
		})
		if err != nil {
			t.Fatalf("begin attempt %d: %v", index, err)
		}
		if _, err = ts.FailAttempt(ctx, attempt.ID, fmt.Sprintf("test failed at C:\\tmp\\run-%d line %d", index, index)); err != nil {
			t.Fatalf("fail attempt %d: %v", index, err)
		}
		task, err = ts.Get(ctx, task.ID)
		if err != nil {
			t.Fatalf("reload task after fail attempt %d: %v", index, err)
		}
		step = task.Plan.Steps[0]
		task, err = ts.FailStep(ctx, task.ID, step.Key, task.Revision, step.Revision, "mandatory test failed")
		if err != nil {
			t.Fatalf("fail step %d: %v", index, err)
		}
		last, err = ts.RecordConfirmedFailure(ctx, taskengine.ConfirmedFailureInput{
			AttemptID:    attempt.ID,
			FailureKind:  "test",
			Message:      fmt.Sprintf("test failed at C:\\tmp\\run-%d line %d", index, index),
			EvidenceRefs: []string{"job:fixture"},
		})
		if err != nil {
			t.Fatalf("record confirmed failure %d: %v", index, err)
		}
		task, err = ts.Get(ctx, task.ID)
		if err != nil {
			t.Fatalf("reload task after record confirmed failure %d: %v", index, err)
		}
	}
	if last.Action != "planner_required" {
		t.Fatalf("final escalation action=%q", last.Action)
	}
	if last.EscalationID == "" {
		t.Fatal("final escalation id empty")
	}
	return task
}

func TestAutoPlanRequirementRevisionRetainsOldEscalation(t *testing.T) {
	ctx := context.Background()
	adapter := &goalAdapter{}
	c, ts, task, input, _, db := newGoalFixtureWithStore(t, adapter, taskengine.GoalContract{})

	planned, err := c.AutoPlan(ctx, AutoPlanInput{
		TaskID:               task.ID,
		ExpectedTaskRevision: task.Revision,
		Actor:                "fixture",
		ProviderID:           input.ProviderID,
	})
	if err != nil {
		t.Fatalf("initial AutoPlan: %v", err)
	}
	task = planned.Task
	if adapter.plans != 1 {
		t.Fatalf("initial adapter plans=%d", adapter.plans)
	}

	task = failTwiceForRequirementEscalation(t, ts, task)

	before, err := ts.PendingEscalation(ctx, task.ID)
	if err != nil || before == nil {
		t.Fatalf("missing current pending escalation: %v", err)
	}

	var rawBefore string
	if err = db.DB.QueryRowContext(ctx, `SELECT state || '|' || evidence_json || '|' || COALESCE(planner_run_id, '') || '|' || COALESCE(CAST(plan_revision AS TEXT), '') || '|' || COALESCE(resolved_at, '') FROM task_step_escalations WHERE id=?`, before.ID).Scan(&rawBefore); err != nil {
		t.Fatalf("snapshot old escalation row: %v", err)
	}

	contract := task.Requirement.Contract
	revised, err := ts.ReviseRequirements(ctx, taskengine.ReviseRequirementsInput{
		TaskID:               task.ID,
		ExpectedTaskRevision: task.Revision,
		Constraints:          task.Requirement.Constraints,
		Unknowns:             task.Requirement.Unknowns,
		Criteria:             task.Requirement.Criteria,
		Contract:             &contract,
		Actor:                "fixture-new-contract",
	})
	if err != nil {
		t.Fatalf("ReviseRequirements: %v", err)
	}
	if revised.ActiveRequirementRevision != task.ActiveRequirementRevision+1 {
		t.Fatalf("old=%d new=%d", task.ActiveRequirementRevision, revised.ActiveRequirementRevision)
	}

	after, err := ts.PendingEscalation(ctx, task.ID)
	if err != nil {
		t.Fatalf("PendingEscalation after revision: %v", err)
	}
	if after != nil {
		t.Fatalf("expected NIL pending escalation after revision, got %+v", after)
	}

	_, planErr := c.AutoPlan(ctx, AutoPlanInput{
		TaskID:               revised.ID,
		ExpectedTaskRevision: revised.Revision,
		Actor:                "fixture",
		ProviderID:           input.ProviderID,
	})
	if planErr != nil {
		t.Fatalf("AutoPlan after revision: %v", planErr)
	}
	if adapter.plans != 2 {
		t.Fatalf("adapter plans=%d", adapter.plans)
	}
	revisedTask, err := ts.Get(ctx, revised.ID)
	if err != nil {
		t.Fatalf("reload revised task: %v", err)
	}
	if revisedTask.ActiveRequirementRevision != task.ActiveRequirementRevision+1 || revisedTask.ActivePlanRevision != 2 {
		t.Fatalf("revised task=%+v", revisedTask)
	}

	var rawAfter string
	if err = db.DB.QueryRowContext(ctx, `SELECT state || '|' || evidence_json || '|' || COALESCE(planner_run_id, '') || '|' || COALESCE(CAST(plan_revision AS TEXT), '') || '|' || COALESCE(resolved_at, '') FROM task_step_escalations WHERE id=?`, before.ID).Scan(&rawAfter); err != nil {
		t.Fatalf("re-snapshot old escalation row: %v", err)
	}
	if rawBefore != rawAfter {
		t.Fatalf("old escalation row changed: before=%q after=%q", rawBefore, rawAfter)
	}

	task = failTwiceForRequirementEscalation(t, ts, revisedTask)

	now, err := ts.PendingEscalation(ctx, task.ID)
	if err != nil || now == nil {
		t.Fatalf("missing new pending escalation: %v", err)
	}
	if now.ID == before.ID {
		t.Fatalf("pending escalation returned old row id=%q", now.ID)
	}

	_, planErr = c.AutoPlan(ctx, AutoPlanInput{
		TaskID:               task.ID,
		ExpectedTaskRevision: task.Revision,
		Actor:                "fixture",
		ProviderID:           input.ProviderID,
	})
	if planErr != nil {
		t.Fatalf("AutoPlan after second failure: %v", planErr)
	}
	if adapter.plans != 3 {
		t.Fatalf("adapter plans=%d", adapter.plans)
	}
	finalTask, err := ts.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload final task: %v", err)
	}
	if finalTask.ActiveRequirementRevision != task.ActiveRequirementRevision || finalTask.ActivePlanRevision != 3 {
		t.Fatalf("final task=%+v", finalTask)
	}

	finalRow, err := ts.PendingEscalation(ctx, task.ID)
	if err != nil || finalRow != nil {
		t.Fatalf("expected NIL pending escalation after final AutoPlan: %+v", finalRow)
	}

	var req2State string
	if err = db.DB.QueryRowContext(ctx, `SELECT state FROM task_step_escalations WHERE id=?`, now.ID).Scan(&req2State); err != nil {
		t.Fatalf("query req2 escalation row: %v", err)
	}
	if req2State != "resolved" {
		t.Fatalf("req2 escalation row state=%q, expected resolved", req2State)
	}

	var rawFinal string
	if err = db.DB.QueryRowContext(ctx, `SELECT state || '|' || evidence_json || '|' || COALESCE(planner_run_id, '') || '|' || COALESCE(CAST(plan_revision AS TEXT), '') || '|' || COALESCE(resolved_at, '') FROM task_step_escalations WHERE id=?`, before.ID).Scan(&rawFinal); err != nil {
		t.Fatalf("final re-snapshot old escalation row: %v", err)
	}
	if rawBefore != rawFinal {
		t.Fatalf("old req1 escalation row changed after req2 resolution: before=%q final=%q", rawBefore, rawFinal)
	}
}

package taskengine

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestPlanningClassifierCreatesOnlyBoundedDeterministicPlan(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task, err := service.Create(ctx, CreateTaskInput{Title: "Rename symbol", Objective: "rename one symbol",
		OriginalRequest: "rename Foo to Bar", Criteria: []Criterion{{ID: "AC-1", Description: "targeted test passes"}}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	decision, planned, err := service.ClassifyPlanning(ctx, ClassifyPlanningInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision, Actor: "owner", AllowedFileScope: []string{"pkg/a.go"},
		Checks: []string{"go test ./pkg"}, EffectScope: []string{"workspace.run", "write"}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "deterministic_plan" || planned.ActivePlanRevision != 1 || len(planned.Plan.Steps) != 1 {
		t.Fatalf("decision=%+v task=%+v", decision, planned)
	}
	if got := planned.Plan.Steps[0].RequirementIDs; len(got) != 1 || got[0] != "AC-1" {
		t.Fatalf("criterion coverage=%v", got)
	}

	uncertain, err := service.Create(ctx, CreateTaskInput{Title: "Investigate", Objective: "find the cause",
		OriginalRequest: "unknown intermittent issue", Unknowns: []string{"reproduction"},
		Criteria: []Criterion{{ID: "AC-1", Description: "cause is proven"}}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	decision, _, err = service.ClassifyPlanning(ctx, ClassifyPlanningInput{TaskID: uncertain.ID,
		ExpectedTaskRevision: uncertain.Revision, Actor: "owner", AllowedFileScope: []string{"pkg/a.go"}, Checks: []string{"go test ./pkg"}})
	if err != nil || decision.Decision != "planner_required" {
		t.Fatalf("uncertain decision=%+v err=%v", decision, err)
	}
}

func TestConfirmedFailureEscalatesAfterTwoAndCapsAtTwo(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	var last EscalationDecision
	for index := 1; index <= 6; index++ {
		run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
			Owner: "runner", LeaseDuration: time.Minute})
		if err != nil {
			t.Fatalf("begin run %d: %v", index, err)
		}
		task, _ = service.Get(ctx, task.ID)
		step := task.Plan.Steps[0]
		attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
			StepKey: step.Key, ExpectedTaskRevision: task.Revision, ExpectedStepRevision: step.Revision,
			InputHash: fmt.Sprintf("packet-%d", index)})
		if err != nil {
			t.Fatalf("begin attempt %d: %v", index, err)
		}
		if _, err = service.FailAttempt(ctx, attempt.ID, fmt.Sprintf("test failed at C:\\tmp\\run-%d line %d", index, index)); err != nil {
			t.Fatal(err)
		}
		task, _ = service.Get(ctx, task.ID)
		step = task.Plan.Steps[0]
		task, err = service.FailStep(ctx, task.ID, step.Key, task.Revision, step.Revision, "mandatory test failed")
		if err != nil {
			t.Fatal(err)
		}
		last, err = service.RecordConfirmedFailure(ctx, ConfirmedFailureInput{AttemptID: attempt.ID, FailureKind: "test",
			Message: fmt.Sprintf("test failed at C:\\tmp\\run-%d line %d", index, index), EvidenceRefs: []string{"job:test"}})
		if err != nil {
			t.Fatal(err)
		}
		task, _ = service.Get(ctx, task.ID)
		if index == 2 || index == 4 {
			if last.Action != "planner_required" {
				t.Fatalf("failure %d action=%+v", index, last)
			}
			// Simulate a completed causal replan before the next worker attempt.
			if _, err = service.store.DB.ExecContext(ctx, `UPDATE task_step_escalations SET state='resolved',plan_revision=? WHERE id=?`,
				task.ActivePlanRevision, last.EscalationID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if last.Action != "escalation_limit" || task.PauseReason == "" {
		t.Fatalf("last=%+v task=%+v", last, task)
	}
}

func TestTransportFailureBlocksWithoutPlannerEscalation(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "runner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, _ = service.Get(ctx, task.ID)
	step := task.Plan.Steps[0]
	attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken, StepKey: step.Key,
		ExpectedTaskRevision: task.Revision, ExpectedStepRevision: step.Revision, InputHash: "packet"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.FailAttempt(ctx, attempt.ID, "connection refused"); err != nil {
		t.Fatal(err)
	}
	task, _ = service.Get(ctx, task.ID)
	step = task.Plan.Steps[0]
	if task, err = service.FailStep(ctx, task.ID, step.Key, task.Revision, step.Revision, "connection refused"); err != nil {
		t.Fatal(err)
	}
	decision, err := service.RecordConfirmedFailure(ctx, ConfirmedFailureInput{AttemptID: attempt.ID, FailureKind: "transport", Message: "connection refused"})
	if err != nil || decision.Action != "blocked_resolution" {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
}

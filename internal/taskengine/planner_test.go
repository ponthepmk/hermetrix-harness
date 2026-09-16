package taskengine

import (
	"context"
	"testing"
)

func TestPlannerRunBindsRevisionAndBecomesUncertainWithoutReplay(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task, err := service.Create(ctx, CreateTaskInput{Title: "Plan", Objective: "make a plan", OriginalRequest: "plan this",
		Criteria: []Criterion{{ID: "AC-1", Description: "has evidence"}}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.BeginPlannerRun(ctx, task.ID, task.Revision, "provider-1", "rev-1", "input-hash")
	if err != nil || run.State != PlannerPlanned || run.RequirementRevision != task.ActiveRequirementRevision {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	if _, err = service.DispatchPlannerRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	run, err = service.PlannerRun(ctx, run.ID)
	if err != nil || run.State != PlannerUncertain {
		t.Fatalf("recovered planner run=%+v err=%v", run, err)
	}
	if _, err = service.DispatchPlannerRun(ctx, run.ID); err == nil {
		t.Fatal("uncertain planner request was replayed")
	}
}

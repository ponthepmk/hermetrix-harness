package taskengine

import (
	"context"
	"fmt"
	"testing"
)

func TestStepOutputScopeLarge(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()

	paths := make([]string, 33)
	for i := 0; i < 33; i++ {
		paths[i] = fmt.Sprintf("p%02d", i)
	}

	task, err := service.Create(ctx, CreateTaskInput{
		Title: "Large scope", Objective: "test large writable paths", OriginalRequest: "build 33 paths",
		Criteria: []Criterion{{ID: "AC-1", Description: "scope enforced"}},
		Contract: GoalContract{WritablePaths: paths}, Actor: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}

	rev := task.Revision

	_, err = service.CreatePlan(ctx, CreatePlanInput{
		TaskID: task.ID, ExpectedTaskRevision: rev, RequirementRevision: task.ActiveRequirementRevision,
		Reason: "blank outputs", Actor: "owner",
		Steps: []StepSpec{{Key: "step", Title: "Step", Instructions: "do work", RequirementIDs: []string{"AC-1"}, Checks: []string{"check"}}},
	})
	if err == nil {
		t.Fatal("blank OutputPaths accepted with large writable scope")
	}

	got, err := service.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != rev {
		t.Fatalf("task revision changed: got %d want %d", got.Revision, rev)
	}

	task, err = service.CreatePlan(ctx, CreatePlanInput{
		TaskID: task.ID, ExpectedTaskRevision: rev, RequirementRevision: task.ActiveRequirementRevision,
		Reason: "bounded outputs", Actor: "owner",
		Steps: []StepSpec{{Key: "step", Title: "Step", Instructions: "do work", RequirementIDs: []string{"AC-1"}, Checks: []string{"check"}, OutputPaths: []string{"p00"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Plan.Steps[0].OutputPaths) != 1 || task.Plan.Steps[0].OutputPaths[0] != "p00" {
		t.Fatalf("OutputPaths = %v, want [p00]", task.Plan.Steps[0].OutputPaths)
	}
}

func TestStepOutputScopeLegacy(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()

	task, err := service.Create(ctx, CreateTaskInput{
		Title: "Legacy scope", Objective: "test legacy writable paths", OriginalRequest: "small global scope",
		Criteria: []Criterion{{ID: "AC-1", Description: "scope enforced"}},
		Contract: GoalContract{WritablePaths: []string{"a", "b"}}, Actor: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}

	task, err = service.CreatePlan(ctx, CreatePlanInput{
		TaskID: task.ID, ExpectedTaskRevision: task.Revision, RequirementRevision: task.ActiveRequirementRevision,
		Reason: "legacy blank outputs", Actor: "owner",
		Steps: []StepSpec{{Key: "step", Title: "Step", Instructions: "do work", RequirementIDs: []string{"AC-1"}, Checks: []string{"check"}}},
	})
	if err != nil {
		t.Fatalf("legacy blank OutputPaths rejected: %v", err)
	}
}

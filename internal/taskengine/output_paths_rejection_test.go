package taskengine

import (
	"context"
	"testing"
)

func TestStepOutputScopeRejection(t *testing.T) {
	cases := []struct {
		name        string
		outputPaths []string
	}{
		{"unauthorized", []string{"x"}},
		{"duplicates", []string{"a", "a"}},
		{"traversal", []string{"../a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, dataStore := testService(t)
			ctx := context.Background()
			task, err := service.Create(ctx, CreateTaskInput{
				Title: "Scope", Objective: "test output scope", OriginalRequest: "scope test",
				Criteria: []Criterion{{ID: "AC1", Description: "output scope enforced"}},
				Contract: GoalContract{WritablePaths: []string{"a", "b"}},
				Actor:    "owner",
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.CreatePlan(ctx, CreatePlanInput{
				TaskID: task.ID, ExpectedTaskRevision: task.Revision,
				RequirementRevision: task.ActiveRequirementRevision,
				Reason:              "test plan", Actor: "planner",
				Steps: []StepSpec{{
					Key: "s1", Title: "Step", Instructions: "do work",
					OutputPaths:    tc.outputPaths,
					RequirementIDs: []string{"AC1"},
					Checks:         []string{"check"},
				}},
			})
			if err == nil {
				t.Fatal("CreatePlan accepted invalid output paths")
			}
			got, err := service.Get(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Revision != task.Revision || got.ActivePlanRevision != 0 {
				t.Fatalf("task mutated after rejection: revision=%d activePlan=%d", got.Revision, got.ActivePlanRevision)
			}
			var planCount, stepCount int
			if err = dataStore.DB.QueryRow("SELECT COUNT(*) FROM task_plan_revisions WHERE task_id=?", task.ID).Scan(&planCount); err != nil {
				t.Fatal(err)
			}
			if err = dataStore.DB.QueryRow("SELECT COUNT(*) FROM task_steps WHERE task_id=?", task.ID).Scan(&stepCount); err != nil {
				t.Fatal(err)
			}
			if planCount != 0 || stepCount != 0 {
				t.Fatalf("rows persisted after rejection: plans=%d steps=%d", planCount, stepCount)
			}
		})
	}
}

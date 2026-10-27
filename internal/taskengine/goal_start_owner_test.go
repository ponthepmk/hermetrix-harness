package taskengine

import (
	"context"
	"reflect"
	"testing"

	"hermetrix-harness/internal/identity"
)

func TestGoalStartOwnedReplay(t *testing.T) {
	service, _ := testService(t)
	task := createPlannedTask(t, service)
	ctx := context.Background()
	goalID := "goal-1"
	config := goalConfig()

	run, err := service.CreateGoalAtID(ctx, goalID, task.ID, task.Revision, "owner", config)
	if err != nil {
		t.Fatalf("owner create: %v", err)
	}
	if run == (GoalRun{}) {
		t.Fatal("expected nonnil GoalRun for owner")
	}
	original := run

	for _, principal := range []string{"foreign", "unknown"} {
		pctx := identity.WithPrincipal(ctx, principal)
		run, err := service.CreateGoalAtID(pctx, goalID, task.ID, task.Revision, "owner", config)
		if err == nil {
			t.Fatalf("expected nonnil error for %s", principal)
		}
		if run != (GoalRun{}) {
			t.Fatalf("expected zero GoalRun for %s", principal)
		}
		stored, err := service.GoalsForTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("GoalsForTask: %v", err)
		}
		if len(stored) != 1 {
			t.Fatalf("expected 1 goal, got %d", len(stored))
		}
		if !reflect.DeepEqual(stored[0], original) {
			t.Fatalf("record should be unchanged for %s", principal)
		}
		if stored[0].Actor != "owner" {
			t.Fatalf("actor should be owner, got %q", stored[0].Actor)
		}
		if stored[0].RequirementRevision != task.ActiveRequirementRevision {
			t.Fatalf("revision should be original, got %d", stored[0].RequirementRevision)
		}
	}

	run, err = service.CreateGoalAtID(ctx, goalID, task.ID, task.Revision, "owner", config)
	if err != nil {
		t.Fatalf("owner replay: %v", err)
	}
	if run == (GoalRun{}) {
		t.Fatal("expected nonnil GoalRun for owner replay")
	}
	stored, err := service.GoalsForTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GoalsForTask: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("expected 1 goal, got %d", len(stored))
	}
	if !reflect.DeepEqual(stored[0], original) {
		t.Fatalf("owner replay record mismatch: %+v", stored[0])
	}
	if stored[0].Actor != "owner" {
		t.Fatalf("actor should be owner, got %q", stored[0].Actor)
	}
	if stored[0].RequirementRevision != task.ActiveRequirementRevision {
		t.Fatalf("revision should be original, got %d", stored[0].RequirementRevision)
	}
}

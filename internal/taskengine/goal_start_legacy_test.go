package taskengine

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestGoalStartLegacyMarker(t *testing.T) {
	service, _ := testService(t)
	task := createPlannedTask(t, service)
	ctx := context.Background()
	cfg := goalConfig()
	cfg.StartInputSHA256 = "abc123"
	goal, err := service.CreateGoal(ctx, task.ID, task.Revision, "owner", cfg)
	if err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	if goal.ID == "" || goal.ID == "goal" {
		t.Fatalf("unexpected goal ID: %q", goal.ID)
	}
	stored, err := service.Goal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("Goal: %v", err)
	}
	if stored.Config.StartInputSHA256 != "" {
		t.Fatalf("marker not empty: %q", stored.Config.StartInputSHA256)
	}
	rejected, err := service.CreateGoalAtID(ctx, goal.ID, task.ID, task.Revision, "owner", cfg)
	if !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("expected ErrStaleRevision, got %v", err)
	}
	if !reflect.DeepEqual(rejected, GoalRun{}) {
		t.Fatalf("rejected GoalRun not zero: %+v", rejected)
	}
	stored, err = service.Goal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("Goal after: %v", err)
	}
	if !reflect.DeepEqual(stored, goal) {
		t.Fatalf("stored goal changed")
	}
	goals, err := service.GoalsForTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GoalsForTask: %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("expected 1 goal, got %d", len(goals))
	}
}

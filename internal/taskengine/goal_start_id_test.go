package taskengine

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestGoalStartIDValidation(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	cfg := goalConfig()

	invalidIDs := []string{
		"",
		"  a",
		"a  ",
		string([]byte{255}),
		strings.Repeat("a", 161),
		strings.Repeat("é", 81),
	}
	for _, id := range invalidIDs {
		task := createPlannedTask(t, service)
		rejected, err := service.CreateGoalAtID(ctx, id, task.ID, task.Revision, "owner", cfg)
		if err == nil {
			t.Fatalf("invalid ID %q: expected error", id)
		}
		if !reflect.DeepEqual(rejected, GoalRun{}) {
			t.Fatalf("invalid ID %q: expected zero GoalRun, got %+v", id, rejected)
		}
		got, err := service.GoalsForTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("invalid ID %q: goals: %v", id, err)
		}
		if len(got) != 0 {
			t.Fatalf("invalid ID %q: expected 0 goals, got %d", id, len(got))
		}
	}

	validIDs := []string{
		strings.Repeat("a", 160),
		strings.Repeat("é", 80),
	}
	for _, id := range validIDs {
		task := createPlannedTask(t, service)
		got, err := service.CreateGoalAtID(ctx, id, task.ID, task.Revision, "owner", cfg)
		if err != nil {
			t.Fatalf("valid ID %q: %v", id, err)
		}
		if got.ID != id {
			t.Fatalf("valid ID %q: expected ID %q, got %q", id, id, got.ID)
		}
		replay, err := service.CreateGoalAtID(ctx, id, task.ID, task.Revision, "owner", cfg)
		if err != nil {
			t.Fatalf("valid ID %q replay: %v", id, err)
		}
		if !reflect.DeepEqual(got, replay) {
			t.Fatalf("valid ID %q: replay mismatch", id)
		}
		goals, err := service.GoalsForTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("valid ID %q: goals: %v", id, err)
		}
		if len(goals) != 1 {
			t.Fatalf("valid ID %q: expected 1 goal, got %d", id, len(goals))
		}
	}
}

package taskengine

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestGoalStartPreservesCreateFailure(t *testing.T) {
	t.Run("stale_revision", func(t *testing.T) {
		s, db := testService(t)
		defer db.Close()
		task := createPlannedTask(t, s)
		fixedID := "goal-stale"
		cfg := goalConfig()
		rejected, err := s.CreateGoalAtID(context.Background(), fixedID, task.ID, task.Revision+1, "tester", cfg)
		if !errors.Is(err, ErrStaleRevision) {
			t.Fatalf("expected ErrStaleRevision, got %v", err)
		}
		if !reflect.DeepEqual(rejected, GoalRun{}) {
			t.Fatal("expected zero GoalRun")
		}
		if _, err := s.Goal(context.Background(), fixedID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected no goal record, got %v", err)
		}
		goals, err := s.GoalsForTask(context.Background(), task.ID)
		if err != nil {
			t.Fatalf("GoalsForTask: %v", err)
		}
		if len(goals) != 0 {
			t.Fatalf("expected no goals, got %d", len(goals))
		}
		created, err := s.CreateGoalAtID(context.Background(), fixedID, task.ID, task.Revision, "tester", cfg)
		if err != nil {
			t.Fatalf("retry: %v", err)
		}
		if created.ID != fixedID {
			t.Fatalf("created ID %q", created.ID)
		}
		if _, err := s.GoalActions(context.Background(), fixedID, 100); err != nil {
			t.Fatalf("GoalActions: %v", err)
		}
		goals, err = s.GoalsForTask(context.Background(), task.ID)
		if err != nil {
			t.Fatalf("GoalsForTask: %v", err)
		}
		if len(goals) != 1 {
			t.Fatalf("expected 1 goal, got %d", len(goals))
		}
	})
	t.Run("max_actions_zero", func(t *testing.T) {
		s, db := testService(t)
		defer db.Close()
		task := createPlannedTask(t, s)
		fixedID := "goal-maxzero"
		cfg := goalConfig()
		cfg.MaxActions = 0
		rejected, err := s.CreateGoalAtID(context.Background(), fixedID, task.ID, task.Revision, "tester", cfg)
		if err == nil {
			t.Fatal("expected error for MaxActions 0")
		}
		if !reflect.DeepEqual(rejected, GoalRun{}) {
			t.Fatal("expected zero GoalRun")
		}
		if errors.Is(err, ErrNotFound) {
			t.Fatalf("expected not ErrNotFound, got %v", err)
		}
		if _, err := s.Goal(context.Background(), fixedID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected no goal record, got %v", err)
		}
		goals, err := s.GoalsForTask(context.Background(), task.ID)
		if err != nil {
			t.Fatalf("GoalsForTask: %v", err)
		}
		if len(goals) != 0 {
			t.Fatalf("expected no goals, got %d", len(goals))
		}
		cfg.MaxActions = 10
		created, err := s.CreateGoalAtID(context.Background(), fixedID, task.ID, task.Revision, "tester", cfg)
		if err != nil {
			t.Fatalf("retry: %v", err)
		}
		if created.ID != fixedID {
			t.Fatalf("created ID %q", created.ID)
		}
		if _, err := s.GoalActions(context.Background(), fixedID, 100); err != nil {
			t.Fatalf("GoalActions: %v", err)
		}
		goals, err = s.GoalsForTask(context.Background(), task.ID)
		if err != nil {
			t.Fatalf("GoalsForTask: %v", err)
		}
		if len(goals) != 1 {
			t.Fatalf("expected 1 goal, got %d", len(goals))
		}
	})
}

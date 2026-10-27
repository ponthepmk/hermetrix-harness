package taskengine

import (
	"context"
	"os"
	"reflect"
	"testing"

	"hermetrix-harness/internal/store"
)

func TestGoalStartAtIDReopen(t *testing.T) {
	ctx := context.Background()
	service, dataStore := testService(t)
	task := createPlannedTask(t, service)
	goalID := "goal-1"
	idFile := t.TempDir() + "/goal-id"
	if err := os.WriteFile(idFile, []byte(goalID), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := service.CreateGoalAtID(ctx, goalID, task.ID, task.Revision, "owner", goalConfig())
	if err != nil {
		t.Fatal(err)
	}
	_, action, err := service.ClaimGoalAction(ctx, goalID, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if action.Action != "plan" {
		t.Fatalf("action: %q", action.Action)
	}
	if err := dataStore.Close(); err != nil {
		t.Fatal(err)
	}
	dataStore, err = store.Open(ctx, dataStore.Root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	service = NewService(dataStore)
	if err := service.RecoverGoals(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := os.ReadFile(idFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(id) != goalID {
		t.Fatalf("id file mismatch: %q", id)
	}
	initial, err := service.Goal(ctx, goalID)
	if err != nil {
		t.Fatal(err)
	}
	if initial.State != GoalWaiting {
		t.Fatalf("state: %q", initial.State)
	}
	actions, err := service.GoalActions(ctx, goalID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions: %d", len(actions))
	}
	if initial.ActionsUsed != 1 || initial.PlansUsed != 1 {
		t.Fatalf("used: %d/%d", initial.ActionsUsed, initial.PlansUsed)
	}
	deadline := initial.Deadline
	replayed, err := service.CreateGoalAtID(ctx, goalID, task.ID, task.Revision, "owner", goalConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(initial, replayed) {
		t.Fatalf("replay changed: %+v", replayed)
	}
	if replayed.Deadline != deadline {
		t.Fatalf("deadline changed")
	}
	if replayed.CreatedAt != initial.CreatedAt {
		t.Fatalf("created changed")
	}
	if _, err := service.StopGoal(ctx, goalID); err != nil {
		t.Fatal(err)
	}
	stopped, err := service.Goal(ctx, goalID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != GoalStopped {
		t.Fatalf("stopped state: %q", stopped.State)
	}
	replayedStopped, err := service.CreateGoalAtID(ctx, goalID, task.ID, task.Revision, "owner", goalConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stopped, replayedStopped) {
		t.Fatalf("stopped replay changed")
	}
	if replayedStopped.Deadline != stopped.Deadline {
		t.Fatalf("stopped deadline changed")
	}
	if replayedStopped.CreatedAt != stopped.CreatedAt {
		t.Fatalf("stopped created changed")
	}
	if _, err := service.GoalsForTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions after replay: %d", len(actions))
	}
}

package taskengine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestGoalStartStoredConfigIntegrity(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, svc)
	cfg := goalConfig()
	cfg.StartInputSHA256 = "marker"
	original, err := svc.CreateGoalAtID(ctx, "goal-1", task.ID, task.Revision, "owner", cfg)
	if err != nil {
		t.Fatalf("CreateGoalAtID: %v", err)
	}
	if len(original.Config.StartInputSHA256) != 64 {
		t.Fatalf("original StartInputSHA256 length: %d", len(original.Config.StartInputSHA256))
	}
	if original.Config.StartInputSHA256 == "marker" {
		t.Fatalf("original StartInputSHA256 equals caller literal marker")
	}
	tampered := original.Config
	tampered.MaxActions++
	data, err := json.Marshal(tampered)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := db.DB.ExecContext(ctx, "UPDATE task_goal_runs SET config_json = ? WHERE id = ?", data, original.ID)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if res == nil {
		t.Fatal("exec returned nil result")
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("rows affected: %v", err)
	}
	if n != 1 {
		t.Fatalf("rows affected: %d", n)
	}
	loaded, err := svc.Goal(ctx, original.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Config.StartInputSHA256 != original.Config.StartInputSHA256 {
		t.Fatalf("marker changed: %q", loaded.Config.StartInputSHA256)
	}
	if loaded.Config.MaxActions != tampered.MaxActions {
		t.Fatalf("MaxActions: %d", loaded.Config.MaxActions)
	}
	if reflect.DeepEqual(loaded.Config, tampered) == false {
		t.Fatalf("loaded config mismatch")
	}
	replay, err := svc.CreateGoalAtID(ctx, "goal-1", task.ID, task.Revision, "owner", original.Config)
	if !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("replay: %v", err)
	}
	if !reflect.DeepEqual(replay, GoalRun{}) {
		t.Fatalf("replay goal: %+v", replay)
	}
	all, err := svc.GoalsForTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("goals: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("goals len: %d", len(all))
	}
	if !reflect.DeepEqual(all[0], loaded) {
		t.Fatalf("owned goal mismatch")
	}
}

package taskcoord

import (
	"context"
	"reflect"
	"testing"

	"hermetrix-harness/internal/store"
)

func TestCodingOperationUpgradeFrom79(t *testing.T) {
	f := newCodingOperationFixture(t, nil)
	ctx := context.Background()

	initialTask, err := f.tasks.Get(ctx, f.initialTaskID)
	if err != nil {
		t.Fatalf("get initial task: %v", err)
	}

	_, err = f.data.DB.ExecContext(ctx, "DROP TABLE IF EXISTS coding_goal_operations")
	if err != nil {
		t.Fatalf("drop coding_goal_operations: %v", err)
	}
	_, err = f.data.DB.ExecContext(ctx, "PRAGMA user_version = 79")
	if err != nil {
		t.Fatalf("set user_version 79: %v", err)
	}

	f = reopenCodingOperationFixture(t, f)
	version, err := f.data.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version != store.CurrentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, store.CurrentSchemaVersion)
	}
	reopenedTask, err := f.tasks.Get(ctx, f.initialTaskID)
	if err != nil {
		t.Fatalf("get reopened task: %v", err)
	}
	if !reflect.DeepEqual(initialTask, reopenedTask) {
		t.Fatalf("legacy task changed after migration")
	}

	var count int
	err = f.data.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='behavioral_eval_runs'").Scan(&count)
	if err != nil {
		t.Fatalf("query behavioral_eval_runs: %v", err)
	}
	if count != 1 {
		t.Fatalf("behavioral_eval_runs table count = %d, want 1", count)
	}

	result, err := f.operations.Start(ctx, f.input)
	if err != nil {
		t.Fatalf("start coding operation: %v", err)
	}
	if result.Goal == nil {
		t.Fatal("goal is nil after start")
	}
	assertCodingOperationCounts(t, f.data, 2, 1, 1)

}

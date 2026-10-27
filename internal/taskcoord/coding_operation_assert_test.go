package taskcoord

import (
	"testing"

	"hermetrix-harness/internal/store"
)

type codingOperationCount struct {
	table string
	want  int
	label string
}

func assertCodingOperationCounts(t *testing.T, db *store.Store, wantTasks, wantOperations, wantGoals int) {
	t.Helper()
	rows := []codingOperationCount{
		{table: "durable_tasks", want: wantTasks, label: "durable_tasks"},
		{table: "coding_goal_operations", want: wantOperations, label: "coding_goal_operations"},
		{table: "task_goal_runs", want: wantGoals, label: "task_goal_runs"},
	}
	for _, row := range rows {
		var got int
		err := db.DB.QueryRow("SELECT COUNT(*) FROM " + row.table).Scan(&got)
		if err != nil {
			t.Fatalf("query %s: %v", row.table, err)
		}
		if got != row.want {
			t.Fatalf("%s count = %d, want %d", row.label, got, row.want)
		}
	}
}

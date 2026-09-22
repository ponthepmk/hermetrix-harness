package taskengine

import (
	"context"
	"errors"
	"testing"
)

func TestTaskProgressIsRevisionBoundAndDerivedFromReceipts(t *testing.T) {
	service, _ := testService(t)
	task := createPlannedTask(t, service)
	if _, err := service.TaskProgress(context.Background(), task.ID, task.Revision-1); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale progress error=%v", err)
	}
	progress, err := service.TaskProgress(context.Background(), task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Attempts != 0 || progress.RemainingAttempts != MaxTaskAttempts || progress.BudgetExhausted || progress.NoProgress {
		t.Fatalf("progress=%+v", progress)
	}
}

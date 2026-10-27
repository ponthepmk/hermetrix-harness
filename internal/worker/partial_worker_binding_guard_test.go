package worker

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestPartialWorkerRejectsUnknownFileViewBeforeProvider(t *testing.T) {
	task, _, _ := partialWorkerFixture(t)
	task.FileViews["unknown.go"] = task.FileViews["big.go"]
	f := &fake{arguments: partialWorkerArguments}
	_, err := Run(context.Background(), f, providers.Profile{}, "key", task)
	if err == nil {
		t.Fatal("expected Run to reject unknown file view")
	}
	if f.calls != 0 {
		t.Fatalf("expected zero provider calls, got %d", f.calls)
	}
}

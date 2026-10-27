package worker

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestPartialWorkerContentGuardRejectsBeforeProvider(t *testing.T) {
	task, _, _ := partialWorkerFixture(t)
	task.FileViews["big.go"][0].Content = "return a+b"
	f := &fake{arguments: partialWorkerArguments}
	_, err := Run(context.Background(), f, providers.Profile{}, "key", task)
	if err == nil {
		t.Fatal("expected Run to reject before provider")
	}
	if f.calls != 0 {
		t.Fatalf("expected zero provider calls, got %d", f.calls)
	}
}

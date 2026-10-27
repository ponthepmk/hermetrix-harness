package worker

import (
	"context"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestPartialWorkerRejectsHashMismatch(t *testing.T) {
	task, _, _ := partialWorkerFixture(t)
	view := task.FileViews["big.go"][0]
	view.FullSHA256 = strings.Repeat("0", 64)
	task.FileViews["big.go"][0] = view

	f := &fake{arguments: partialWorkerArguments}
	_, err := Run(context.Background(), f, providers.Profile{}, "key", task)
	if err == nil {
		t.Fatal("expected Run to reject hash mismatch")
	}
	if f.calls != 0 {
		t.Fatalf("expected zero provider calls, got %d", f.calls)
	}
}

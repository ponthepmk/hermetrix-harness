package worker

import (
	"context"
	"errors"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestPartialWorkerRejectsCredentialBeforeProvider(t *testing.T) {
	task, original, _ := partialWorkerFixture(t)
	full := original + "SECRETKEY"
	task.Files["big.go"] = full
	view, err := NewBoundedFileView("big.go", full, Hash(full), task.FileViews["big.go"][0].Start, task.FileViews["big.go"][0].End)
	if err != nil {
		t.Fatalf("NewBoundedFileView: %v", err)
	}
	task.FileViews["big.go"] = []BoundedFileView{view}
	f := &fake{arguments: partialWorkerArguments}
	_, err = Run(context.Background(), f, providers.Profile{}, "SECRETKEY", task)
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrorCredentialPresent {
		t.Fatalf("expected ErrorCredentialPresent, got %v", err)
	}
	if f.calls != 0 {
		t.Fatalf("provider calls = %d", f.calls)
	}
}

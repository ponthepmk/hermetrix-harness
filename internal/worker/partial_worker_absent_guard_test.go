package worker

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestPartialWorkerRejectsAbsentFileBeforeProvider(t *testing.T) {
	task, _, _ := partialWorkerFixture(t)
	task.Files["new.txt"] = ""
	task.WritablePaths = append(task.WritablePaths, "new.txt")
	task.AbsentFiles = append(task.AbsentFiles, "new.txt")
	view, err := NewBoundedFileView("new.txt", "", Hash(""), 0, 0)
	if err != nil {
		t.Fatalf("NewBoundedFileView: %v", err)
	}
	task.FileViews["new.txt"] = []BoundedFileView{view}
	f := &fake{arguments: partialWorkerArguments}
	_, err = Run(context.Background(), f, providers.Profile{}, "key", task)
	if err == nil {
		t.Fatal("Run should reject absent file")
	}
	if f.calls != 0 {
		t.Fatalf("provider calls = %d", f.calls)
	}
}

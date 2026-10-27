package worker

import (
	"context"
	"encoding/json"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestProgressCallbackMutatesOnlyFirstEvent(t *testing.T) {
	task, _, afterFull := partialWorkerFixture(t)
	called := 0
	fake := &fake{arguments: partialWorkerArguments}
	options := Options{Progress: func(ProgressEvent) {
		if called == 0 {
			called++
			task.Files["big.go"] = afterFull
			task.FileViews["big.go"][0].Content = afterFull
			task.WritablePaths[0] = "wrongpath"
		}
	}}
	precallJSON, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	expectedTask := Task{}
	if err := json.Unmarshal(precallJSON, &expectedTask); err != nil {
		t.Fatalf("unmarshal expected task: %v", err)
	}
	result, err := RunWithOptions(context.Background(), fake, providers.Profile{}, "", task, options)
	if err != nil {
		t.Fatalf("RunWithOptions: %v", err)
	}
	if result.InputSHA256 != Hash(string(precallJSON)) {
		t.Fatalf("InputSHA256 mismatch")
	}
	if err := ValidateResult(expectedTask, result); err != nil {
		t.Fatalf("ValidateResult expected: %v", err)
	}
	if err := ValidateResult(task, result); err == nil {
		t.Fatal("mutated original task should fail validation")
	}
	if result.Changes == nil || len(result.Changes) != 1 || result.Changes[0].Content != afterFull {
		t.Fatalf("expected single change Content equals afterFull, got %+v", result.Changes)
	}
	if called < 1 || fake.calls != 1 {
		t.Fatalf("called=%d fake.calls=%d", called, fake.calls)
	}
}

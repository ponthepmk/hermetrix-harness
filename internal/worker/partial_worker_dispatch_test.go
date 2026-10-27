package worker

import (
	"context"
	"encoding/json"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestDispatchCallbackRunsBeforeProvider(t *testing.T) {
	task, _, afterFull := partialWorkerFixture(t)
	precallJSON, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	expectedTask := Task{}
	if err := json.Unmarshal(precallJSON, &expectedTask); err != nil {
		t.Fatalf("unmarshal expected task: %v", err)
	}
	var called int
	f := &fake{
		arguments: partialWorkerArguments,
		completion: &providers.Completion{
			FinishReason: "tool_calls",
			ToolCalls: []providers.ToolCall{{
				Name:      "submit_changes",
				Arguments: partialWorkerArguments,
			}},
		},
	}
	options := Options{
		BeforeProviderRequest: func() error {
			called++
			task.Files["big.go"] = afterFull
			task.FileViews["big.go"][0].Content = afterFull
			task.WritablePaths[0] = "wrongpath"
			return nil
		},
	}
	result, err := RunWithOptions(context.Background(), f, providers.Profile{}, "", task, options)
	if err != nil {
		t.Fatalf("RunWithOptions: %v", err)
	}
	if result.InputSHA256 != Hash(string(precallJSON)) {
		t.Fatalf("InputSHA256 mismatch")
	}
	if err := ValidateResult(expectedTask, result); err != nil {
		t.Fatalf("expected task invalid: %v", err)
	}
	if err := ValidateResult(task, result); err == nil {
		t.Fatalf("mutated task should be invalid")
	}
	if result.Changes == nil || len(result.Changes) != 1 || result.Changes[0].Content != afterFull {
		t.Fatalf("expected single change Content equals afterFull, got %+v", result.Changes)
	}
	if called < 1 || f.calls != 1 {
		t.Fatalf("callback or provider call count wrong: %d %d", called, f.calls)
	}
}

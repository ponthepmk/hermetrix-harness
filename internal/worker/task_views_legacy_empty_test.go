package worker

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestLegacyTaskViewsEmptyFileViews(t *testing.T) {
	raw, err := os.ReadFile("task_views_legacy_fixture.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixtures []legacyFixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if len(fixtures) != 4 {
		t.Fatalf("expected 4 fixture records, got %d", len(fixtures))
	}
	for i, f := range fixtures {
		var task Task
		if err := json.Unmarshal([]byte(f.TaskJSON), &task); err != nil {
			t.Fatalf("record %d: unmarshal task_json: %v", i, err)
		}
		task.FileViews = map[string][]BoundedFileView{}
		adapter := &fake{arguments: f.Arguments}
		result, err := Run(context.Background(), adapter, providers.Profile{}, "", task)
		if err != nil {
			t.Fatalf("record %d: run: %v", i, err)
		}
		if err := ValidateResult(task, result); err != nil {
			t.Fatalf("record %d: validate: %v", i, err)
		}
		if adapter.calls != 1 || len(adapter.request.Messages) != 2 {
			t.Fatalf("record %d: expected one call and two messages, got calls=%d messages=%d", i, adapter.calls, len(adapter.request.Messages))
		}
		if adapter.request.Messages[1].Content != f.TaskJSON {
			t.Fatalf("record %d: wire content mismatch", i)
		}
		if result.InputSHA256 != f.ExpectedInputSHA256 {
			t.Fatalf("record %d: input sha mismatch", i)
		}
		if result.ProposalID != f.ExpectedProposalID {
			t.Fatalf("record %d: proposal id mismatch", i)
		}
		if len(result.Changes) != 1 {
			t.Fatalf("record %d: expected 1 change, got %d", i, len(result.Changes))
		}
		result.Changes[0].Edits = []TextEdit{}
		if proposalID(result.InputSHA256, result) != f.ExpectedProposalID {
			t.Fatalf("record %d: proposal id changed after empty edits", i)
		}
		if err := ValidateResult(task, result); err != nil {
			t.Fatalf("record %d: validate after empty edits: %v", i, err)
		}
	}
}

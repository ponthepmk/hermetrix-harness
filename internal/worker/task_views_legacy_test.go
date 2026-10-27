package worker

import (
	"context"
	"encoding/json"
	"hermetrix-harness/internal/providers"
	"os"
	"testing"
)

type legacyFixture struct {
	Name                 string `json:"name"`
	ExpectedProposalID   string `json:"expected_proposal_id"`
	ExpectedBeforeSHA256 string `json:"expected_before_sha256"`
	Arguments            string `json:"arguments"`
	ExpectedStrategy     string `json:"expected_strategy"`
	ExpectedInputSHA256  string `json:"expected_input_sha256"`
	TaskJSON             string `json:"task_json"`
	ExpectedContent      string `json:"expected_content"`
}

func TestLegacyTaskViewsNilFileViews(t *testing.T) {
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
		c := result.Changes[0]
		if c.BeforeSHA256 != f.ExpectedBeforeSHA256 {
			t.Fatalf("record %d: before sha mismatch", i)
		}
		if c.Content != f.ExpectedContent {
			t.Fatalf("record %d: content mismatch", i)
		}
		if c.Strategy != f.ExpectedStrategy {
			t.Fatalf("record %d: strategy mismatch", i)
		}
	}
}

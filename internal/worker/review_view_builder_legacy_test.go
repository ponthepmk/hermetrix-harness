package worker

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestLegacyTaskViewsBuildReviewFiles(t *testing.T) {
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
	for _, f := range fixtures {
		var task Task
		if err := json.Unmarshal([]byte(f.TaskJSON), &task); err != nil {
			t.Errorf("%s: unmarshal task_json: %v", f.Name, err)
			continue
		}
		adapter := &fake{arguments: f.Arguments}
		result, err := Run(context.Background(), adapter, providers.Profile{}, "", task)
		if err != nil {
			t.Errorf("%s: run: %v", f.Name, err)
			continue
		}
		whole, partial, err := BuildReviewFiles(task, result)
		if err != nil {
			t.Errorf("%s: build review files: %v", f.Name, err)
			continue
		}
		if len(whole) != 1 {
			t.Errorf("%s: expected 1 whole file, got %d", f.Name, len(whole))
			continue
		}
		for path, content := range whole {
			if path != result.Changes[0].Path {
				t.Errorf("%s: whole file path mismatch: got %q", f.Name, path)
			}
			if content != f.ExpectedContent {
				t.Errorf("%s: whole file content mismatch", f.Name)
			}
		}
		if len(partial) != 0 {
			t.Errorf("%s: expected empty partial map, got %d", f.Name, len(partial))
		}
	}
}

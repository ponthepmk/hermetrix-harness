package taskcoord

import (
	"testing"

	"hermetrix-harness/internal/worker"
)

func TestHistoricalBoundaryEmpty(t *testing.T) {
	input := []worker.Change{{Path: "a.go", Content: ""}}
	writable := []string{"a.go"}

	records, omitted, truncated := historicalSourceExcerpts(input, writable)

	if len(records) != 1 {
		t.Fatalf("records=%d want 1", len(records))
	}
	if omitted != 0 {
		t.Fatalf("omitted=%d want 0", omitted)
	}
	if truncated {
		t.Fatalf("truncated=%v want false", truncated)
	}

	rec := records[0]
	if rec["path"].(string) != "a.go" {
		t.Fatalf("path=%v want a.go", rec["path"])
	}
	if rec["full_sha256"].(string) != worker.Hash("") {
		t.Fatalf("full_sha256=%v want worker.Hash(\"\")", rec["full_sha256"])
	}
	if rec["full_bytes"].(int) != 0 {
		t.Fatalf("full_bytes=%d want 0", rec["full_bytes"])
	}
	if rec["excerpt_bytes"].(int) != 0 {
		t.Fatalf("excerpt_bytes=%d want 0", rec["excerpt_bytes"])
	}
	if rec["excerpt"].(string) != "" {
		t.Fatalf("excerpt=%q want empty", rec["excerpt"])
	}
	if rec["truncated"].(bool) {
		t.Fatalf("truncated=%v want false", rec["truncated"])
	}

	if input[0].Path != "a.go" {
		t.Fatalf("input path mutated")
	}
	if input[0].Content != "" {
		t.Fatalf("input content mutated")
	}
}

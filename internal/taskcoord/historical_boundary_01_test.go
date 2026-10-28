package taskcoord

import (
	"testing"
	"unicode/utf8"

	"hermetrix-harness/internal/worker"
)

func TestHistoricalBoundaryThaiSmall(t *testing.T) {
	changes := []worker.Change{{Path: "a.go", Content: "กabc"}}
	writable := []string{"a.go"}

	records, omitted, truncated := historicalSourceExcerpts(changes, writable)

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if omitted != 0 {
		t.Fatalf("expected omitted 0, got %d", omitted)
	}
	if truncated {
		t.Fatal("expected truncated false")
	}

	r := records[0]
	if r["path"].(string) != "a.go" {
		t.Fatalf("expected path a.go, got %v", r["path"])
	}
	if r["full_bytes"].(int) != 6 {
		t.Fatalf("expected full_bytes 6, got %v", r["full_bytes"])
	}
	if r["excerpt_bytes"].(int) != 6 {
		t.Fatalf("expected excerpt_bytes 6, got %v", r["excerpt_bytes"])
	}
	if r["excerpt"].(string) != "กabc" {
		t.Fatalf("expected excerpt กabc, got %q", r["excerpt"])
	}
	if r["truncated"].(bool) {
		t.Fatal("expected record truncated false")
	}
	if r["full_sha256"].(string) != worker.Hash("กabc") {
		t.Fatalf("expected full_sha256 %s, got %v", worker.Hash("กabc"), r["full_sha256"])
	}
	if !utf8.ValidString(r["excerpt"].(string)) {
		t.Fatal("expected valid UTF-8 excerpt")
	}
	if len([]byte(r["excerpt"].(string))) != 6 {
		t.Fatalf("expected excerpt byte length 6, got %d", len([]byte(r["excerpt"].(string))))
	}
	if changes[0].Content != "กabc" || writable[0] != "a.go" {
		t.Fatal("inputs changed")
	}
}

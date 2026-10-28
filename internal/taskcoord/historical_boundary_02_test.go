package taskcoord

import (
	"strings"
	"testing"
	"unicode/utf8"

	"hermetrix-harness/internal/worker"
)

func TestHistoricalBoundaryThaiClip(t *testing.T) {
	full := strings.Repeat("ก", 4000)
	changes := []worker.Change{{Path: "a.go", Content: full}}
	writable := []string{"a.go"}

	records, omitted, truncated := historicalSourceExcerpts(changes, writable)

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if omitted != 0 {
		t.Fatalf("expected omitted 0, got %d", omitted)
	}
	if !truncated {
		t.Fatal("expected overall truncated true")
	}

	rec := records[0]
	if rec["path"] != "a.go" {
		t.Fatalf("expected path a.go, got %v", rec["path"])
	}
	if rec["full_bytes"] != 12000 {
		t.Fatalf("expected full_bytes 12000, got %v", rec["full_bytes"])
	}
	if rec["excerpt_bytes"] != 8190 {
		t.Fatalf("expected excerpt_bytes 8190, got %v", rec["excerpt_bytes"])
	}
	if rec["truncated"] != true {
		t.Fatalf("expected record truncated true, got %v", rec["truncated"])
	}

	excerpt := rec["excerpt"].(string)
	if len(excerpt) != 8190 {
		t.Fatalf("expected excerpt byte length 8190, got %d", len(excerpt))
	}
	if !utf8.ValidString(excerpt) {
		t.Fatal("expected excerpt to be valid UTF-8")
	}
	if !strings.HasPrefix(full, excerpt) {
		t.Fatal("expected full to start with excerpt")
	}

	fullSHA := worker.Hash(full)
	if rec["full_sha256"] != fullSHA {
		t.Fatalf("expected full_sha256 %s, got %v", fullSHA, rec["full_sha256"])
	}
	if rec["full_sha256"] == worker.Hash(excerpt) {
		t.Fatal("expected full_sha256 to be complete 12000-byte hash, not excerpt hash")
	}

	if changes[0].Content != full {
		t.Fatal("expected input change content unchanged")
	}
	if len(writable) != 1 || writable[0] != "a.go" {
		t.Fatal("expected writable paths unchanged")
	}
}

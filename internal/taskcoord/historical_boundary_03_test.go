package taskcoord

import (
	"strings"
	"testing"
	"unicode/utf8"

	"hermetrix-harness/internal/worker"
)

func TestHistoricalBoundaryRuneRemainder(t *testing.T) {
	a := strings.Repeat("a", 8191)
	b := "ก"
	changes := []worker.Change{
		{Path: "a.go", Content: a},
		{Path: "b.go", Content: b},
	}
	paths := []string{"a.go", "b.go"}
	records, omitted, truncated := historicalSourceExcerpts(changes, paths)
	if len(changes) != 2 || changes[0].Content != a || changes[1].Content != b || paths[0] != "a.go" || paths[1] != "b.go" {
		t.Fatal("inputs changed")
	}
	if len(records) != 1 || omitted != 1 || !truncated {
		t.Fatalf("records=%d omitted=%d truncated=%v", len(records), omitted, truncated)
	}
	r := records[0]
	if r["path"].(string) != "a.go" || r["full_bytes"].(int) != 8191 || r["excerpt_bytes"].(int) != 8191 {
		t.Fatalf("path=%v full_bytes=%v excerpt_bytes=%v", r["path"], r["full_bytes"], r["excerpt_bytes"])
	}
	if r["truncated"].(bool) != false {
		t.Fatal("a.go truncated should be false")
	}
	if r["excerpt"].(string) != a {
		t.Fatal("excerpt mismatch")
	}
	if !utf8.ValidString(r["excerpt"].(string)) {
		t.Fatal("excerpt invalid UTF-8")
	}
	hash := worker.Hash(a)
	if r["full_sha256"].(string) != hash {
		t.Fatal("sha256 mismatch")
	}
	if len(a) != 8191 || len(b) != 3 {
		t.Fatal("unexpected input lengths")
	}
	if len(b) <= 1 {
		t.Fatal("b.go cannot fit remaining 1 byte")
	}
}

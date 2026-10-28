package taskcoord

import (
	"strings"
	"testing"
	"unicode/utf8"

	"hermetrix-harness/internal/worker"
)

func TestHistoricalBoundaryCombinedBudget(t *testing.T) {
	a := strings.Repeat("a", 6000)
	b := strings.Repeat("b", 4000)

	changes := []worker.Change{
		{
			Path:         "a.go",
			Content:      a,
			BeforeSHA256: worker.Hash(a),
			Strategy:     "full",
		},
		{
			Path:         "b.go",
			Content:      b,
			BeforeSHA256: worker.Hash(b),
			Strategy:     "full",
		},
	}
	writablePaths := []string{"a.go", "b.go"}

	records, omitted, truncated := historicalSourceExcerpts(changes, writablePaths)

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if omitted != 0 {
		t.Fatalf("expected omitted 0, got %d", omitted)
	}
	if !truncated {
		t.Fatalf("expected truncated overall true")
	}

	wantFullSHA := worker.Hash(a)
	wantExcerpt := a
	wantExcerptSHA := worker.Hash(wantExcerpt)
	if records[0]["path"] != "a.go" {
		t.Fatalf("expected path a.go")
	}
	if records[0]["full_sha256"] != wantFullSHA {
		t.Fatalf("expected full_sha256 %s", wantFullSHA)
	}
	if records[0]["full_bytes"] != 6000 {
		t.Fatalf("expected full_bytes 6000")
	}
	if records[0]["excerpt_bytes"] != 6000 {
		t.Fatalf("expected excerpt_bytes 6000")
	}
	if records[0]["excerpt"] != wantExcerpt {
		t.Fatalf("expected full a excerpt")
	}
	if !utf8.ValidString(records[0]["excerpt"].(string)) {
		t.Fatalf("excerpt not valid UTF-8")
	}
	if !strings.HasPrefix(a, wantExcerpt) {
		t.Fatalf("full content does not have expected excerpt prefix")
	}
	if len(a) != 6000 {
		t.Fatalf("input a length changed")
	}
	if len(b) != 4000 {
		t.Fatalf("input b length changed")
	}

	wantBExcerpt := b[:2192]
	wantBExcerptSHA := worker.Hash(wantBExcerpt)
	if records[1]["path"] != "b.go" {
		t.Fatalf("expected path b.go")
	}
	if records[1]["full_sha256"] != worker.Hash(b) {
		t.Fatalf("expected b full_sha256")
	}
	if records[1]["full_bytes"] != 4000 {
		t.Fatalf("expected b full_bytes 4000")
	}
	if records[1]["excerpt_bytes"] != 2192 {
		t.Fatalf("expected b excerpt_bytes 2192")
	}
	if records[1]["excerpt"] != wantBExcerpt {
		t.Fatalf("expected b excerpt of 2192 b bytes")
	}
	if !utf8.ValidString(wantBExcerpt) {
		t.Fatalf("b excerpt not valid UTF-8")
	}
	if strings.HasPrefix(b, wantBExcerpt) == false {
		t.Fatalf("b full does not have expected b excerpt prefix")
	}

	if records[0]["truncated"].(bool) != false {
		t.Fatalf("expected record a truncated false")
	}
	if records[1]["truncated"].(bool) != true {
		t.Fatalf("expected record b truncated true")
	}
	if records[1]["full_sha256"].(string) == wantBExcerptSHA {
		t.Fatalf("b full hash equals b excerpt hash")
	}
	if wantBExcerptSHA == wantFullSHA {
		t.Fatalf("b excerpt hash equals a full hash")
	}
	if wantBExcerptSHA == wantExcerptSHA {
		t.Fatalf("b excerpt hash equals a excerpt hash")
	}
	if worker.Hash(b) == wantExcerptSHA {
		t.Fatalf("b full hash equals a excerpt hash")
	}
	if records[0]["excerpt_bytes"].(int)+records[1]["excerpt_bytes"].(int) != 8192 {
		t.Fatalf("expected total excerpt_bytes 8192, got %d", records[0]["excerpt_bytes"].(int)+records[1]["excerpt_bytes"].(int))
	}
}

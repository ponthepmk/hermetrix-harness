package taskcoord

import (
	"testing"
	"unicode/utf8"

	"hermetrix-harness/internal/worker"
)

import "strings"

func TestHistoricalBoundaryTwoRecordCap(t *testing.T) {
	changes := []worker.Change{
		{Path: "a.go", Content: "x"},
		{Path: "b.go", Content: "y"},
		{Path: "c.go", Content: "z"},
		{Path: "private.go", Content: "p"},
	}
	writablePaths := []string{"a.go", "b.go", "c.go"}
	records, omitted, truncated := historicalSourceExcerpts(changes, writablePaths)

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if omitted != 1 {
		t.Fatalf("expected 1 omitted, got %d", omitted)
	}
	if !truncated {
		t.Fatal("expected truncated true")
	}

	wantA := worker.Hash("x")
	wantB := worker.Hash("y")

	for i, rec := range records {
		path, ok := rec["path"].(string)
		if !ok {
			t.Fatalf("record %d path not string", i)
		}
		fullSHA, ok := rec["full_sha256"].(string)
		if !ok {
			t.Fatalf("record %d full_sha256 not string", i)
		}
		fullBytes, ok := rec["full_bytes"].(int)
		if !ok {
			t.Fatalf("record %d full_bytes not int", i)
		}
		excerptBytes, ok := rec["excerpt_bytes"].(int)
		if !ok {
			t.Fatalf("record %d excerpt_bytes not int", i)
		}
		excerpt, ok := rec["excerpt"].(string)
		if !ok {
			t.Fatalf("record %d excerpt not string", i)
		}
		rcTruncated, ok := rec["truncated"].(bool)
		if !ok {
			t.Fatalf("record %d truncated not bool", i)
		}
		if !utf8.ValidString(excerpt) {
			t.Fatalf("record %d excerpt not valid UTF-8", i)
		}
		if fullBytes != len(changes[i].Content) {
			t.Fatalf("record %d full_bytes mismatch", i)
		}
		if fullSHA != worker.Hash(changes[i].Content) {
			t.Fatalf("record %d full_sha256 mismatch", i)
		}
		if !strings.HasPrefix(changes[i].Content, excerpt) {
			t.Fatalf("record %d excerpt not prefix of full", i)
		}
		if fullBytes != 0 && excerptBytes != len(excerpt) {
			t.Fatalf("record %d excerpt_bytes mismatch", i)
		}
		if fullBytes != 0 && rcTruncated != (len(excerpt) < fullBytes) {
			t.Fatalf("record %d truncated mismatch", i)
		}
		if i == 0 {
			if path != "a.go" || fullSHA != wantA || fullBytes != 1 || excerpt != "x" || excerptBytes != 1 || rcTruncated {
				t.Fatalf("record 0 mismatch: %v", rec)
			}
		} else if i == 1 {
			if path != "b.go" || fullSHA != wantB || fullBytes != 1 || excerpt != "y" || excerptBytes != 1 || rcTruncated {
				t.Fatalf("record 1 mismatch: %v", rec)
			}
		} else {
			t.Fatalf("unexpected record %d", i)
		}
	}

	if !utf8.ValidString(changes[0].Content) || !utf8.ValidString(changes[1].Content) || !utf8.ValidString(changes[2].Content) || !utf8.ValidString(changes[3].Content) {
		t.Fatal("input content changed UTF-8 validity")
	}
	if changes[0].Path != "a.go" || changes[0].Content != "x" || changes[1].Path != "b.go" || changes[1].Content != "y" || changes[2].Path != "c.go" || changes[2].Content != "z" || changes[3].Path != "private.go" || changes[3].Content != "p" {
		t.Fatal("inputs changed")
	}
}

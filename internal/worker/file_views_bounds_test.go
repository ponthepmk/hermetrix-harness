package worker

import (
	"testing"
)

func TestBoundedFileViewBoundaryRules(t *testing.T) {
	source := string([]byte{97, 13, 10, 195, 169, 122})
	path := "bounds.txt"
	fullSHA := Hash(string(source))

	invalid := []struct {
		name       string
		start, end int
	}{
		{"negative start", -1, 0},
		{"negative end", 0, -1},
		{"reversed", 5, 2},
		{"start over EOF", 7, 7},
		{"end over EOF", 0, 7},
		{"start inside UTF8", 4, 6},
		{"end inside UTF8", 0, 4},
		{"start splits CRLF", 2, 6},
		{"end splits CRLF", 0, 2},
		{"empty splits CRLF", 2, 2},
	}
	for _, c := range invalid {
		_, err := NewBoundedFileView(path, source, fullSHA, c.start, c.end)
		if err == nil {
			t.Fatalf("expected error for %s", c.name)
		}
	}

	valid := []struct {
		name       string
		start, end int
	}{
		{"empty start", 0, 0},
		{"empty EOF", 6, 6},
		{"full", 0, 6},
		{"whole multibyte rune", 3, 5},
	}
	for _, c := range valid {
		view, err := NewBoundedFileView(path, source, fullSHA, c.start, c.end)
		if err != nil {
			t.Fatalf("expected success for %s: %v", c.name, err)
		}
		if view.Content != source[c.start:c.end] {
			t.Fatalf("content mismatch for %s", c.name)
		}
		if err := ValidateBoundedFileView(path, source, fullSHA, view); err != nil {
			t.Fatalf("validation failed for %s: %v", c.name, err)
		}
	}
}

package worker

import (
	"fmt"
	"strings"
	"testing"
)

func TestMaterializeBoundedChangeEditLimit(t *testing.T) {
	for _, n := range []int{64, 65} {
		fullSource := buildEditLimitSource(n)
		fullSHA := Hash(fullSource)

		views, err := NewBoundedFileView("test", fullSource, fullSHA, 0, len(fullSource))
		if err != nil {
			t.Fatalf("NewBoundedFileView: %v", err)
		}
		view := views

		edits := make([]TextEdit, n)
		for i := 0; i < n; i++ {
			edits[i] = TextEdit{Old: fmt.Sprintf("X%03d", i), New: fmt.Sprintf("Y%03d", i)}
		}

		full, strategy, err := MaterializeBoundedChange("test", fullSource, fullSHA, []BoundedFileView{view}, nil, edits)
		if n == 64 {
			if err != nil {
				t.Fatalf("MaterializeBoundedChange 64 edits: %v", err)
			}
			if full != buildEditLimitSourceY(n) {
				t.Fatalf("64 edits full mismatch: got %q", full)
			}
			if strategy != "replace_text" {
				t.Fatalf("64 edits strategy mismatch: got %q", strategy)
			}
		} else {
			if err == nil {
				t.Fatalf("65 edits should fail")
			}
			if full != "" || strategy != "" {
				t.Fatalf("65 edits non-empty output: full=%q strategy=%q", full, strategy)
			}
		}
	}
}

func TestMaterializeBoundedChangeViewLimit(t *testing.T) {
	source := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefg"
	fullSHA := Hash(source)
	edit := TextEdit{Old: "A", New: "!"}

	for _, n := range []int{32, 33, 0} {
		views := make([]BoundedFileView, 0, n)
		for i := 0; i < n; i++ {
			view, err := NewBoundedFileView("test", source, fullSHA, i, i+1)
			if err != nil {
				t.Fatalf("NewBoundedFileView %d: %v", i, err)
			}
			views = append(views, view)
		}

		full, strategy, err := MaterializeBoundedChange("test", source, fullSHA, views, nil, []TextEdit{edit})
		if n == 32 {
			if err != nil {
				t.Fatalf("32 views MaterializeBoundedChange: %v", err)
			}
			if full != "!BCDEFGHIJKLMNOPQRSTUVWXYZabcdefg" {
				t.Fatalf("32 views full mismatch: got %q", full)
			}
			if strategy != "replace_text" {
				t.Fatalf("32 views strategy mismatch: got %q", strategy)
			}
		} else {
			if err == nil {
				t.Fatalf("%d views should fail", n)
			}
			if full != "" || strategy != "" {
				t.Fatalf("%d views non-empty output: full=%q strategy=%q", n, full, strategy)
			}
		}
	}
}

func buildEditLimitSource(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(fmt.Sprintf("X%03d|", i))
	}
	return b.String()
}

func buildEditLimitSourceY(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(fmt.Sprintf("Y%03d|", i))
	}
	return b.String()
}

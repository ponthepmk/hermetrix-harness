package worker

import (
	"testing"
)

func TestMaterializeBoundedChangeContentEdits(t *testing.T) {
	source := "abcDEF"
	fullSHA := Hash(source)
	view, err := NewBoundedFileView("p", source, fullSHA, 0, 6)
	if err != nil {
		t.Fatalf("NewBoundedFileView: %v", err)
	}
	views := []BoundedFileView{view}

	empty := ""
	full, strategy, err := MaterializeBoundedChange("p", source, fullSHA, views, &empty, []TextEdit{{Old: "abc", New: "A"}})
	if err == nil {
		t.Fatal("expected error for empty content with edits")
	}
	if full != "" || strategy != "" {
		t.Fatalf("expected empty full and strategy, got %q %q", full, strategy)
	}

	replacement := "replacement"
	full, strategy, err = MaterializeBoundedChange("p", source, fullSHA, views, &replacement, nil)
	if err == nil {
		t.Fatal("expected error for content with no edits")
	}
	if full != "" || strategy != "" {
		t.Fatalf("expected empty full and strategy, got %q %q", full, strategy)
	}

	full, strategy, err = MaterializeBoundedChange("p", source, fullSHA, views, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil content with no edits")
	}
	if full != "" || strategy != "" {
		t.Fatalf("expected empty full and strategy, got %q %q", full, strategy)
	}

	full, strategy, err = MaterializeBoundedChange("p", source, fullSHA, views, nil, []TextEdit{{Old: "abc", New: "A"}})
	if err != nil {
		t.Fatalf("expected no error for valid control, got %v", err)
	}
	if full != "ADEF" {
		t.Fatalf("expected full ADEF, got %q", full)
	}
	if strategy != "replace_text" {
		t.Fatalf("expected strategy replace_text, got %q", strategy)
	}
}

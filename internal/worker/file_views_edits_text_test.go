package worker

import (
	"testing"
)

func TestMaterializeBoundedChangeTextEdits(t *testing.T) {
	source := "abcDEF"
	fullSHA := Hash(source)
	view, err := NewBoundedFileView("p", source, fullSHA, 0, 6)
	if err != nil {
		t.Fatalf("NewBoundedFileView: %v", err)
	}
	full, strategy, err := MaterializeBoundedChange("p", source, fullSHA, []BoundedFileView{view}, nil, []TextEdit{{Old: "abc", New: ""}})
	if err != nil {
		t.Fatalf("MaterializeBoundedChange delete: %v", err)
	}
	if full != "DEF" {
		t.Fatalf("delete full mismatch: got %q want %q", full, "DEF")
	}
	if strategy != "replace_text" {
		t.Fatalf("delete strategy mismatch: got %q want %q", strategy, "replace_text")
	}

	full, strategy, err = MaterializeBoundedChange("p", source, fullSHA, []BoundedFileView{view}, nil, []TextEdit{{Old: "", New: "A"}})
	if err == nil {
		t.Fatalf("empty Old: expected error")
	}
	if full != "" || strategy != "" {
		t.Fatalf("empty Old: got full=%q strategy=%q", full, strategy)
	}

	full, strategy, err = MaterializeBoundedChange("p", source, fullSHA, []BoundedFileView{view}, nil, []TextEdit{{Old: "missing", New: "A"}})
	if err == nil {
		t.Fatalf("missing Old: expected error")
	}
	if full != "" || strategy != "" {
		t.Fatalf("missing Old: got full=%q strategy=%q", full, strategy)
	}

	full, strategy, err = MaterializeBoundedChange("p", source, fullSHA, []BoundedFileView{view}, nil, []TextEdit{{Old: "abc", New: string([]byte{255})}})
	if err == nil {
		t.Fatalf("invalid UTF-8 New: expected error")
	}
	if full != "" || strategy != "" {
		t.Fatalf("invalid UTF-8 New: got full=%q strategy=%q", full, strategy)
	}
}

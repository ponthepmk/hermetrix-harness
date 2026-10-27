package worker

import (
	"reflect"
	"testing"
)

func TestMaterializeBoundedChangeViewOverlap(t *testing.T) {
	source := "abcDEF"
	fullSHA := Hash(source)
	viewPath := "p"
	edit := TextEdit{Old: "abc", New: "A"}

	// Negative case 1: the same valid view 0..6 appears twice; edit abc->A.
	// Must fail with two empty strings.
	{
		v, err := NewBoundedFileView(viewPath, source, fullSHA, 0, 6)
		if err != nil {
			t.Fatalf("NewBoundedFileView: %v", err)
		}
		views := []BoundedFileView{v, v}
		full, strategy, err := MaterializeBoundedChange(viewPath, source, fullSHA, views, nil, []TextEdit{edit})
		if err == nil {
			t.Fatalf("expected error for duplicate view, got full=%q strategy=%q", full, strategy)
		}
		if full != "" || strategy != "" {
			t.Fatalf("expected empty full and strategy, got full=%q strategy=%q", full, strategy)
		}
	}

	// Negative case 2: valid distinct views 0..4 and 2..6 overlap; edit c->C is
	// unique and contained. Must fail with two empty strings.
	{
		v1, err := NewBoundedFileView(viewPath, source, fullSHA, 0, 4)
		if err != nil {
			t.Fatalf("NewBoundedFileView: %v", err)
		}
		v2, err := NewBoundedFileView(viewPath, source, fullSHA, 2, 6)
		if err != nil {
			t.Fatalf("NewBoundedFileView: %v", err)
		}
		views := []BoundedFileView{v1, v2}
		full, strategy, err := MaterializeBoundedChange(viewPath, source, fullSHA, views, nil, []TextEdit{{Old: "c", New: "C"}})
		if err == nil {
			t.Fatalf("expected error for overlapping views, got full=%q strategy=%q", full, strategy)
		}
		if full != "" || strategy != "" {
			t.Fatalf("expected empty full and strategy, got full=%q strategy=%q", full, strategy)
		}
	}

	// Negative case 3: first view 0..4 authorizes abc->A; a second view 4..6 is
	// unused, but its Content is tampered after construction. Must still fail
	// with two empty strings, proving all views are validated.
	{
		v1, err := NewBoundedFileView(viewPath, source, fullSHA, 0, 4)
		if err != nil {
			t.Fatalf("NewBoundedFileView: %v", err)
		}
		v2, err := NewBoundedFileView(viewPath, source, fullSHA, 4, 6)
		if err != nil {
			t.Fatalf("NewBoundedFileView: %v", err)
		}
		v2.Content = "DEF"
		views := []BoundedFileView{v1, v2}
		full, strategy, err := MaterializeBoundedChange(viewPath, source, fullSHA, views, nil, []TextEdit{edit})
		if err == nil {
			t.Fatalf("expected error for tampered unused view, got full=%q strategy=%q", full, strategy)
		}
		if full != "" || strategy != "" {
			t.Fatalf("expected empty full and strategy, got full=%q strategy=%q", full, strategy)
		}
	}

	// Positive control: whole view 0..6 plus an unused genuine empty view 3..3;
	// edit abc->A must return ADEF, replace_text, nil. Empty views do not
	// overlap positive ranges and do not grant nonempty edit permission.
	{
		v1, err := NewBoundedFileView(viewPath, source, fullSHA, 0, 6)
		if err != nil {
			t.Fatalf("NewBoundedFileView: %v", err)
		}
		v2, err := NewBoundedFileView(viewPath, source, fullSHA, 3, 3)
		if err != nil {
			t.Fatalf("NewBoundedFileView: %v", err)
		}
		views := []BoundedFileView{v1, v2}
		full, strategy, err := MaterializeBoundedChange(viewPath, source, fullSHA, views, nil, []TextEdit{edit})
		if err != nil {
			t.Fatalf("MaterializeBoundedChange: %v", err)
		}
		if full != "ADEF" {
			t.Fatalf("full mismatch: got %q want ADEF", full)
		}
		if strategy != "replace_text" {
			t.Fatalf("strategy mismatch: got %q want replace_text", strategy)
		}
		if !reflect.DeepEqual(views, []BoundedFileView{v1, v2}) {
			t.Fatalf("views mutated")
		}
	}
}

package worker

import (
	"reflect"
	"testing"
)

func TestMaterializeBoundedChangeViewTamper(t *testing.T) {
	baseSource := "abcDEF"
	basePath := "p"
	fullSHA := Hash(baseSource)
	view, err := NewBoundedFileView(basePath, baseSource, fullSHA, 0, 6)
	if err != nil {
		t.Fatalf("NewBoundedFileView: %v", err)
	}
	viewCopy := view
	edits := []TextEdit{{Old: "abc", New: "A"}}
	editsCopy := edits

	full, strategy, err := MaterializeBoundedChange(basePath, baseSource, fullSHA, []BoundedFileView{view}, nil, edits)
	if err != nil {
		t.Fatalf("MaterializeBoundedChange: %v", err)
	}
	if full != "ADEF" {
		t.Fatalf("full mismatch: got %q want %q", full, "ADEF")
	}
	if strategy != "replace_text" {
		t.Fatalf("strategy mismatch: got %q want %q", strategy, "replace_text")
	}
	if !reflect.DeepEqual(view, viewCopy) {
		t.Fatalf("view mutated")
	}
	if !reflect.DeepEqual(edits, editsCopy) {
		t.Fatalf("edits mutated")
	}

	negatives := []struct {
		id           string
		fn           func(v BoundedFileView) BoundedFileView
		expectedPath string
		expectedSHA  string
	}{{
		id: "view_path_tampered",
		fn: func(v BoundedFileView) BoundedFileView {
			v.Path = "q"
			return v
		},
		expectedPath: basePath,
		expectedSHA:  fullSHA,
	}, {
		id: "view_full_sha_tampered",
		fn: func(v BoundedFileView) BoundedFileView {
			v.FullSHA256 = Hash("other")
			return v
		},
		expectedPath: basePath,
		expectedSHA:  fullSHA,
	}, {
		id: "view_start_tampered",
		fn: func(v BoundedFileView) BoundedFileView {
			v.Start = 1
			return v
		},
		expectedPath: basePath,
		expectedSHA:  fullSHA,
	}, {
		id: "view_end_tampered",
		fn: func(v BoundedFileView) BoundedFileView {
			v.End = 5
			return v
		},
		expectedPath: basePath,
		expectedSHA:  fullSHA,
	}, {
		id: "view_content_tampered",
		fn: func(v BoundedFileView) BoundedFileView {
			v.Content = "tampered"
			return v
		},
		expectedPath: basePath,
		expectedSHA:  fullSHA,
	}, {
		id: "view_region_id_tampered",
		fn: func(v BoundedFileView) BoundedFileView {
			v.RegionID = "tampered"
			return v
		},
		expectedPath: basePath,
		expectedSHA:  fullSHA,
	}, {
		id: "expected_path_tampered",
		fn: func(v BoundedFileView) BoundedFileView {
			return v
		},
		expectedPath: "q",
		expectedSHA:  fullSHA,
	}, {
		id: "expected_sha_tampered",
		fn: func(v BoundedFileView) BoundedFileView {
			return v
		},
		expectedPath: basePath,
		expectedSHA:  Hash("other"),
	}, {
		id: "expected_sha_invalid_source_hash",
		fn: func(v BoundedFileView) BoundedFileView {
			return v
		},
		expectedPath: basePath,
		expectedSHA:  Hash(string([]byte{255})),
	},
	}

	for _, tt := range negatives {
		v := view
		v = tt.fn(v)
		full, strategy, err := MaterializeBoundedChange(tt.expectedPath, baseSource, tt.expectedSHA, []BoundedFileView{v}, nil, edits)
		if err == nil {
			t.Fatalf("%s: expected error", tt.id)
		}
		if full != "" {
			t.Fatalf("%s: expected empty full, got %q", tt.id, full)
		}
		if strategy != "" {
			t.Fatalf("%s: expected empty strategy, got %q", tt.id, strategy)
		}
	}

	invalidSource := string([]byte{97, 255})
	invalidSHA := Hash(invalidSource)
	invalidView, err := NewBoundedFileView(basePath, invalidSource, invalidSHA, 0, 1)
	if err == nil {
		t.Fatalf("NewBoundedFileView invalid UTF-8: expected error")
	}
	invalidView = BoundedFileView{
		Path:       basePath,
		FullSHA256: invalidSHA,
		Start:      0,
		End:        1,
		Content:    "a",
		RegionID:   hashRegionID(basePath, invalidSHA, 0, 1),
	}
	full, strategy, err = MaterializeBoundedChange(basePath, invalidSource, invalidSHA, []BoundedFileView{invalidView}, nil, []TextEdit{{Old: "a", New: "A"}})
	if err == nil {
		t.Fatalf("MaterializeBoundedChange invalid UTF-8: expected error")
	}
	if full != "" {
		t.Fatalf("MaterializeBoundedChange invalid UTF-8: expected empty full, got %q", full)
	}
	if strategy != "" {
		t.Fatalf("MaterializeBoundedChange invalid UTF-8: expected empty strategy, got %q", strategy)
	}
}

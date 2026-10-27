package worker

import (
	"reflect"
	"testing"
)

func TestMaterializeBoundedChangePositiveFixtures(t *testing.T) {
	tests := []struct {
		id       string
		source   string
		expected string
		ranges   []struct {
			start int
			end   int
		}
		edits []TextEdit
	}{{
		id:       "different_length_original_offsets",
		source:   "HIDDEN-A\none=1\ntwo=2\nHIDDEN-Z\n",
		expected: "HIDDEN-A\nONE=100\nT2\nHIDDEN-Z\n",
		ranges: []struct {
			start int
			end   int
		}{{9, 14}, {15, 20}},
		edits: []TextEdit{{Old: "one=1", New: "ONE=100"}, {Old: "two=2", New: "T2"}},
	}, {
		id:       "same_edit_reverse_input_order",
		source:   "HIDDEN-A\none=1\ntwo=2\nHIDDEN-Z\n",
		expected: "HIDDEN-A\nONE=100\nT2\nHIDDEN-Z\n",
		ranges: []struct {
			start int
			end   int
		}{{9, 14}, {15, 20}},
		edits: []TextEdit{{Old: "two=2", New: "T2"}, {Old: "one=1", New: "ONE=100"}},
	}, {
		id:       "adjacent_disjoint",
		source:   "abcDEF",
		expected: "ADDDD",
		ranges: []struct {
			start int
			end   int
		}{{0, 3}, {3, 6}},
		edits: []TextEdit{{Old: "abc", New: "A"}, {Old: "DEF", New: "DDDD"}},
	}, {
		id:       "noop",
		source:   "hidden|ok|end",
		expected: "hidden|ok|end",
		ranges: []struct {
			start int
			end   int
		}{{7, 9}},
		edits: []TextEdit{{Old: "ok", New: "ok"}},
	}, {
		id:       "unicode_whole_crlf",
		source:   "secret|ก\r\ntail",
		expected: "secret|ข\r\ntail",
		ranges: []struct {
			start int
			end   int
		}{{7, 12}},
		edits: []TextEdit{{Old: "ก\r\n", New: "ข\r\n"}},
	}}

	for _, tt := range tests {
		fullSHA := Hash(tt.source)
		views := make([]BoundedFileView, 0, len(tt.ranges))
		for _, r := range tt.ranges {
			view, err := NewBoundedFileView("test", tt.source, fullSHA, r.start, r.end)
			if err != nil {
				t.Fatalf("NewBoundedFileView: %v", err)
			}
			views = append(views, view)
		}
		viewsCopy := make([]BoundedFileView, len(views))
		for i, v := range views {
			viewsCopy[i] = v
		}
		editsCopy := make([]TextEdit, len(tt.edits))
		for i, e := range tt.edits {
			editsCopy[i] = e
		}
		full, strategy, err := MaterializeBoundedChange("test", tt.source, fullSHA, views, nil, tt.edits)
		if err != nil {
			t.Fatalf("MaterializeBoundedChange: %v", err)
		}
		if full != tt.expected {
			t.Fatalf("full mismatch: got %q want %q", full, tt.expected)
		}
		if strategy != "replace_text" {
			t.Fatalf("strategy mismatch: got %q want %q", strategy, "replace_text")
		}
		if !reflect.DeepEqual(views, viewsCopy) {
			t.Fatalf("views mutated")
		}
		if !reflect.DeepEqual(tt.edits, editsCopy) {
			t.Fatalf("edits mutated")
		}
	}
}

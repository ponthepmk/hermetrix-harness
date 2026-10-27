package worker

import (
	"testing"
)

func TestMaterializeBoundedChangeFrozenNegatives(t *testing.T) {
	type caseDef struct {
		name   string
		source string
		start  int
		end    int
		edits  []TextEdit
	}
	cases := []caseDef{
		{
			name:   "overlapping_original_edits",
			source: "abcde",
			start:  0,
			end:    5,
			edits: []TextEdit{
				{Old: "abc", New: "X"},
				{Old: "cde", New: "Y"},
			},
		},
		{
			name:   "chain_referencing_new_only_text",
			source: "abc",
			start:  0,
			end:    3,
			edits: []TextEdit{
				{Old: "a", New: "X"},
				{Old: "X", New: "Y"},
			},
		},
		{
			name:   "missing_old",
			source: "abc",
			start:  0,
			end:    3,
			edits: []TextEdit{
				{Old: "z", New: "X"},
			},
		},
		{
			name:   "empty_view_permission",
			source: "abc",
			start:  1,
			end:    1,
			edits: []TextEdit{
				{Old: "a", New: "X"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fullSHA := Hash(c.source)
			view, err := NewBoundedFileView("f.txt", c.source, fullSHA, c.start, c.end)
			if err != nil {
				t.Fatalf("NewBoundedFileView: %v", err)
			}
			full, strategy, err := MaterializeBoundedChange("f.txt", c.source, fullSHA, []BoundedFileView{view}, nil, c.edits)
			if err == nil {
				t.Fatalf("MaterializeBoundedChange: expected error, got full=%q strategy=%q", full, strategy)
			}
			if full != "" {
				t.Fatalf("MaterializeBoundedChange: expected empty full, got %q", full)
			}
			if strategy != "" {
				t.Fatalf("MaterializeBoundedChange: expected empty strategy, got %q", strategy)
			}
		})
	}
}

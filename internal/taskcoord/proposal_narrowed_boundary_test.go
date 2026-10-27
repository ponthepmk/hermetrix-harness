package taskcoord

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"hermetrix-harness/internal/worker"
)

func TestProposalNarrowedViewFrozenCases(t *testing.T) {
	data, err := os.ReadFile("proposal_view_cases.json")
	if err != nil {
		t.Fatalf("read proposal_view_cases.json: %v", err)
	}
	var rows []struct {
		Name      string
		Path      string
		Source    string
		Regions   []proposalSourceRegion
		Limit     int
		WantError bool
		Expected  [][2]int
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("decode proposal_view_cases.json: %v", err)
	}
	if len(rows) != 12 {
		t.Fatalf("expected exactly 12 rows, got %d", len(rows))
	}
	for i, row := range rows {
		before, err := json.Marshal(row.Regions)
		if err != nil {
			t.Fatalf("marshal regions before row %d: %v", i, err)
		}
		views, err := proposalViewsFromRegions(row.Path, row.Source, row.Regions, nil, row.Limit)
		after, marshalErr := json.Marshal(row.Regions)
		if marshalErr != nil {
			t.Fatalf("marshal regions after row %d: %v", i, marshalErr)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("regions mutated for row %d", i)
		}
		if row.WantError {
			if err == nil {
				t.Fatalf("expected error for row %d", i)
			}
			if views != nil {
				t.Fatalf("expected nil views for row %d", i)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for row %d: %v", i, err)
		}
		if len(views) != len(row.Expected) || len(row.Expected) == 0 {
			t.Fatalf("expected %d views, got %d", len(row.Expected), len(views))
		}
		for j, want := range row.Expected {
			if views[j].Start != want[0] || views[j].End != want[1] {
				t.Fatalf("view %d mismatch: got (%d,%d), want (%d,%d)", j, views[j].Start, views[j].End, want[0], want[1])
			}
			if err := worker.ValidateBoundedFileView(row.Path, row.Source, worker.Hash(row.Source), views[j]); err != nil {
				t.Fatalf("validate view %d: %v", j, err)
			}
		}
	}
}

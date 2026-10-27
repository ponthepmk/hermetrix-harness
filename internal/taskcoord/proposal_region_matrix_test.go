package taskcoord

import (
	"encoding/json"
	"os"
	"testing"
)

func TestProposalGoFrozenRanges(t *testing.T) {
	data, err := os.ReadFile("proposal_region_cases_v2.json")
	if err != nil {
		t.Fatalf("read proposal_region_cases_v2.json: %v", err)
	}
	var rows []struct {
		Name      string
		Source    string
		Terms     []string
		WantError bool
		Expected  []struct {
			Start int
			End   int
		}
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("decode proposal_region_cases_v2.json: %v", err)
	}
	if len(rows) != 7 {
		t.Fatalf("expected exactly 7 rows, got %d", len(rows))
	}
	for i, row := range rows {
		regions, err := proposalGoSourceRegions(row.Name+".go", row.Source, row.Terms)
		if row.WantError {
			if err == nil {
				t.Fatalf("expected error for row %d", i)
			}
			if regions != nil {
				t.Fatalf("expected nil regions for row %d", i)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for row %d: %v", i, err)
		}
		if len(regions) != len(row.Expected) || len(row.Expected) == 0 {
			t.Fatalf("expected %d regions, got %d", len(row.Expected), len(regions))
		}
		for j, want := range row.Expected {
			if regions[j].Start != want.Start || regions[j].End != want.End {
				t.Fatalf("region %d mismatch: got (%d,%d), want (%d,%d)", j, regions[j].Start, regions[j].End, want.Start, want.End)
			}
			if regions[j].Start < 0 || regions[j].End > len(row.Source) || regions[j].Start >= regions[j].End {
				t.Fatalf("region %d out of bounds: (%d,%d) source len %d", j, regions[j].Start, regions[j].End, len(row.Source))
			}
		}
	}
}

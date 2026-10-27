package taskcoord

import (
	"encoding/json"
	"os"
	"testing"
)

func TestProposalLineFrozenCases(t *testing.T) {
	data, err := os.ReadFile("proposal_line_cases.json")
	if err != nil {
		t.Fatalf("read proposal_line_cases.json: %v", err)
	}
	var rows []struct {
		Name      string
		Source    string
		Terms     []string
		Radius    int
		WantError bool
		Expected  [][2]int
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("decode proposal_line_cases.json: %v", err)
	}
	if len(rows) != 10 {
		t.Fatalf("expected exactly 10 rows, got %d", len(rows))
	}
	for i, row := range rows {
		got, err := proposalLineSourceRegions(row.Source, row.Terms, row.Radius)
		if row.WantError {
			if err == nil {
				t.Fatalf("expected error for row %d", i)
			}
			if got != nil {
				t.Fatalf("expected nil result for row %d", i)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for row %d: %v", i, err)
		}
		if len(got) != len(row.Expected) {
			t.Fatalf("expected %d regions, got %d", len(row.Expected), len(got))
		}
		for j, want := range row.Expected {
			if got[j].Start != want[0] || got[j].End != want[1] {
				t.Fatalf("region %d mismatch: got (%d,%d), want (%d,%d)", j, got[j].Start, got[j].End, want[0], want[1])
			}
		}
	}
}

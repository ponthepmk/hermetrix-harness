package taskcoord

import (
	"encoding/json"
	"os"
	"testing"
)

type fixture struct {
	Terms  []termsRow
	Scores []scoreRow
}

type termsRow struct {
	Name      string
	Text      string
	Expected  []string
	WantError bool
}

type scoreRow struct {
	Name     string
	Text     string
	Terms    []string
	Expected int
}

func TestProposalAnchorFrozenCases(t *testing.T) {
	data, err := os.ReadFile("proposal_anchor_cases_v2.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture fixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(fixture.Terms) != 6 {
		t.Fatalf("expected 6 terms rows, got %d", len(fixture.Terms))
	}
	if len(fixture.Scores) != 3 {
		t.Fatalf("expected 3 scores rows, got %d", len(fixture.Scores))
	}
	for _, row := range fixture.Terms {
		terms, err := proposalAnchorTerms(row.Text)
		if row.WantError {
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if terms != nil {
				t.Fatalf("expected nil terms, got %v", terms)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(terms) != len(row.Expected) {
			t.Fatalf("expected length %d, got %d", len(row.Expected), len(terms))
		}
		for i, want := range row.Expected {
			if terms[i] != want {
				t.Fatalf("term %d: expected %q, got %q", i, want, terms[i])
			}
		}
	}
	for _, row := range fixture.Scores {
		if got := proposalRegionScore(row.Text, row.Terms); got != row.Expected {
			t.Fatalf("expected %d, got %d", row.Expected, got)
		}
	}
}

package skills

import (
	"testing"
)

func TestParseBehavioralPairedNegative(t *testing.T) {
	tests := []struct {
		name          string
		raw           string
		expectedTasks int
	}{
		{"root null", `null`, 1},
		{"root array", `[{}]`, 1},
		{"missing cases", `{}`, 1},
		{"empty cases", `{"cases":[]}`, 1},
		{"cases wrong type", `{"cases":"not-array"}`, 1},
		{"null case entry", `{"cases":[null]}`, 1},
		{"whitespace-only case_id", `{"cases":[{"case_id":"   ","baseline_passed":false,"candidate_passed":false}]}`, 1},
		{"wrong-type case_id", `{"cases":[{"case_id":1,"baseline_passed":false,"candidate_passed":false}]}`, 1},
		{"missing baseline_passed", `{"cases":[{"case_id":"case-1","candidate_passed":false}]}`, 1},
		{"null candidate_passed", `{"cases":[{"case_id":"case-1","baseline_passed":false,"candidate_passed":null}]}`, 1},
		{"wrong-type candidate_passed", `{"cases":[{"case_id":"case-1","baseline_passed":false,"candidate_passed":"no"}]}`, 1},
		{"duplicate ID with surrounding whitespace", `{"cases":[{"case_id":"case-1","baseline_passed":false,"candidate_passed":false},{"case_id":"  case-1  ","baseline_passed":false,"candidate_passed":false}]}`, 2},
		{"case count mismatch", `{"cases":[{"case_id":"case-1","baseline_passed":false,"candidate_passed":false}]}`, 2},
		{"trailing JSON", `{"cases":[{"case_id":"case-1","baseline_passed":false,"candidate_passed":false}]} extra`, 1},
	}

	for _, tt := range tests {
		total, baseline, candidate, regressions, err := parseBehavioralPaired([]byte(tt.raw), tt.expectedTasks)
		if err == nil {
			t.Errorf("%s: parseBehavioralPaired expected error, got none (total=%d baseline=%d candidate=%d regressions=%d)",
				tt.name, total, baseline, candidate, regressions)
			continue
		}
	}
}

func TestParseBehavioralPairedPositive(t *testing.T) {
	tests := []struct {
		name            string
		raw             string
		expectedTasks   int
		wantTotal       int
		wantBaseline    int
		wantCandidate   int
		wantRegressions int
	}{
		{"false false accepted", `{"cases":[{"case_id":"case-1","baseline_passed":false,"candidate_passed":false}]}`, 1, 1, 0, 0, 0},
		{"true false regression", `{"cases":[{"case_id":"case-1","baseline_passed":true,"candidate_passed":false}]}`, 1, 1, 1, 0, 1},
		{"false true improvement", `{"cases":[{"case_id":"case-1","baseline_passed":false,"candidate_passed":true}]}`, 1, 1, 0, 1, 0},
	}

	for _, tt := range tests {
		total, baseline, candidate, regressions, err := parseBehavioralPaired([]byte(tt.raw), tt.expectedTasks)
		if err != nil {
			t.Errorf("%s: parseBehavioralPaired unexpected error: %v", tt.name, err)
			continue
		}
		if total != tt.wantTotal || baseline != tt.wantBaseline || candidate != tt.wantCandidate || regressions != tt.wantRegressions {
			t.Errorf("%s: got total=%d baseline=%d candidate=%d regressions=%d, want total=%d baseline=%d candidate=%d regressions=%d",
				tt.name, total, baseline, candidate, regressions,
				tt.wantTotal, tt.wantBaseline, tt.wantCandidate, tt.wantRegressions)
		}
	}
}

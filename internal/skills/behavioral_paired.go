package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// parseBehavioralPaired parses the paired behavioral evaluation payload:
// a root object with a "cases" array of objects, each carrying case_id,
// baseline_passed and candidate_passed. It returns the derived counts and
// an error for any schema violation.
func parseBehavioralPaired(raw json.RawMessage, expectedTasks int) (int, int, int, int, error) {
	var root struct {
		Cases []json.RawMessage `json:"cases"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("behavioral paired payload: %w", err)
	}
	if len(root.Cases) == 0 {
		return 0, 0, 0, 0, errors.New("behavioral paired payload: cases is absent, empty or null")
	}
	if len(root.Cases) != expectedTasks {
		return 0, 0, 0, 0, fmt.Errorf("behavioral paired payload: expected %d cases, got %d", expectedTasks, len(root.Cases))
	}
	seen := make(map[string]struct{}, len(root.Cases))
	baselinePassed := 0
	candidatePassed := 0
	regressions := 0
	for _, entry := range root.Cases {
		var item struct {
			CaseID          string `json:"case_id"`
			BaselinePassed  *bool  `json:"baseline_passed"`
			CandidatePassed *bool  `json:"candidate_passed"`
		}
		if err := json.Unmarshal(entry, &item); err != nil {
			return 0, 0, 0, 0, fmt.Errorf("behavioral paired payload case: %w", err)
		}
		if item.BaselinePassed == nil || item.CandidatePassed == nil {
			return 0, 0, 0, 0, fmt.Errorf("behavioral paired payload case %q: baseline_passed and candidate_passed are required", item.CaseID)
		}
		id := strings.TrimSpace(item.CaseID)
		if id == "" {
			return 0, 0, 0, 0, fmt.Errorf("behavioral paired payload: case id must be a nonempty non-whitespace string")
		}
		if _, dup := seen[id]; dup {
			return 0, 0, 0, 0, fmt.Errorf("behavioral paired payload: duplicate case id %q", id)
		}
		seen[id] = struct{}{}
		if *item.BaselinePassed {
			baselinePassed++
		}
		if *item.CandidatePassed {
			candidatePassed++
		}
		if *item.BaselinePassed && !*item.CandidatePassed {
			regressions++
		}
	}
	return len(root.Cases), baselinePassed, candidatePassed, regressions, nil
}

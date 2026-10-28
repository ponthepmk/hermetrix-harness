package taskcoord

import (
	"encoding/json"
	"os"
	"testing"
)

type descriptor struct {
	Path       string `json:"path"`
	Unit       string `json:"unit"`
	Repeat     int    `json:"repeat"`
	FullBytes  int    `json:"full_bytes"`
	FullSHA256 string `json:"full_sha256"`
}

type expected struct {
	FileCount int  `json:"file_count"`
	ByteCount int  `json:"byte_count"`
	Omitted   int  `json:"omitted"`
	Truncated bool `json:"truncated"`
}

type call struct {
	Changes       []descriptor `json:"changes"`
	WritablePaths []string     `json:"writable_paths"`
	Want          expected     `json:"want"`
}

type historicalSourceBudgetCase struct {
	Name  string `json:"name"`
	Calls []call `json:"calls"`
}

func loadHistoricalBudgetCases(t *testing.T) []historicalSourceBudgetCase {
	data, err := os.ReadFile("historical_source_budget_cases.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var cases []historicalSourceBudgetCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(cases) != 7 {
		t.Fatalf("expected 7 cases, got %d", len(cases))
	}
	return cases
}

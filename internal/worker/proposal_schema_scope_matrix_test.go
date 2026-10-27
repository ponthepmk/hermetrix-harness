package worker

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"hermetrix-harness/internal/providers"
)

type caseRow struct {
	Name       string   `json:"name"`
	Task       Task     `json:"task"`
	Allowed    []string `json:"allowed"`
	MaxChanges int      `json:"max_changes"`
}

type rootCases struct {
	Cases []caseRow `json:"cases"`
}

func TestProposalSchemaFrozenMatrix(t *testing.T) {
	data, err := os.ReadFile("proposal_schema_scope_cases.json")
	if err != nil {
		t.Fatalf("read data: %v", err)
	}
	var root rootCases
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("decode root: %v", err)
	}
	if len(root.Cases) != 6 {
		t.Fatalf("expected 6 cases, got %d", len(root.Cases))
	}
	for _, row := range root.Cases {
		t.Run(row.Name, func(t *testing.T) {
			beforeBytes, err := json.Marshal(row.Task)
			if err != nil {
				t.Fatalf("marshal task before: %v", err)
			}
			frozen, _, wire, err := FreezeTask(row.Task)
			if err != nil {
				t.Fatalf("freeze: %v", err)
			}
			request := proposalRequest(frozen, wire)
			changes := request.Tools[0].Function.Parameters["properties"].(map[string]any)["changes"].(map[string]any)
			minItems := changes["minItems"].(int)
			if minItems != 1 {
				t.Fatalf("minItems: %d", minItems)
			}
			maxItems, ok := changes["maxItems"].(int)
			if !ok {
				t.Fatal("maxItems missing")
			}
			if maxItems != row.MaxChanges {
				t.Fatalf("maxItems: %d", maxItems)
			}
			enum := changes["items"].(map[string]any)["properties"].(map[string]any)["path"].(map[string]any)["enum"].([]string)
			allowedBytes, err := json.Marshal(row.Allowed)
			if err != nil {
				t.Fatalf("marshal allowed: %v", err)
			}
			enumBytes, err := json.Marshal(enum)
			if err != nil {
				t.Fatalf("marshal enum: %v", err)
			}
			if !bytes.Equal(allowedBytes, enumBytes) {
				t.Fatalf("enum mismatch: %s vs %s", enumBytes, allowedBytes)
			}
			afterBytes, err := json.Marshal(row.Task)
			if err != nil {
				t.Fatalf("marshal task after: %v", err)
			}
			if !bytes.Equal(beforeBytes, afterBytes) {
				t.Fatalf("task mutated")
			}
			estimate, err := EstimateProposalInput(row.Task)
			if err != nil {
				t.Fatalf("estimate: %v", err)
			}
			if estimate.WireBytes != len(wire) {
				t.Fatalf("wire bytes: %d", estimate.WireBytes)
			}
			if estimate.Tokens != providers.EstimateRequestTokens(request) {
				t.Fatalf("tokens mismatch")
			}
		})
	}
}

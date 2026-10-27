package worker

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"hermetrix-harness/internal/providers"
)

type proposalRequestFixtureRow struct {
	Name    string                `json:"name"`
	Task    Task                  `json:"task"`
	Request providers.ChatRequest `json:"request"`
}

func TestProposalRequestMatchesFrozenTransportFixture(t *testing.T) {
	data, err := os.ReadFile("proposal_request_fixture.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var rows []proposalRequestFixtureRow
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 fixture rows, got %d", len(rows))
	}
	for _, row := range rows {
		frozen, _, wire, err := FreezeTask(row.Task)
		if err != nil {
			t.Fatalf("%s: FreezeTask: %v", row.Name, err)
		}
		decorateScopeGolden(t, row.Name, &row.Request)
		got := proposalRequest(frozen, wire)
		gotBytes, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("%s: marshal got request: %v", row.Name, err)
		}
		rowBytes, err := json.Marshal(row.Request)
		if err != nil {
			t.Fatalf("%s: marshal fixture request: %v", row.Name, err)
		}
		if !bytes.Equal(gotBytes, rowBytes) {
			t.Fatalf("%s: request JSON mismatch\ngot: %s\nwant: %s", row.Name, gotBytes, rowBytes)
		}
		stats, err := EstimateProposalInput(row.Task)
		if err != nil {
			t.Fatalf("%s: EstimateProposalInput: %v", row.Name, err)
		}
		if stats.WireBytes != len(wire) {
			t.Fatalf("%s: WireBytes mismatch: got %d want %d", row.Name, stats.WireBytes, len(wire))
		}
		if stats.Tokens != providers.EstimateRequestTokens(row.Request) {
			t.Fatalf("%s: Tokens mismatch: got %d want %d", row.Name, stats.Tokens, providers.EstimateRequestTokens(row.Request))
		}
	}
}

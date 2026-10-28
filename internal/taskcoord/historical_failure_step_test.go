package taskcoord

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

type historicalFailureStepCase struct {
	Name       string                 `json:"name"`
	Packet     taskengine.StepPacket  `json:"packet"`
	Evidence   worker.FailureEvidence `json:"evidence"`
	WantStepID string                 `json:"want_step_id"`
}

func TestHistoricalFailureStepFrozenCases(t *testing.T) {
	data, err := os.ReadFile("historical_failure_step_cases.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var cases []historicalFailureStepCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(cases) != 19 {
		t.Fatalf("expected 19 cases, got %d", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			before, err := json.Marshal(c)
			if err != nil {
				t.Fatalf("marshal before: %v", err)
			}
			got := historicalFailureStepID(c.Packet, c.Evidence)
			if got != c.WantStepID {
				t.Fatalf("historicalFailureStepID = %q, want %q", got, c.WantStepID)
			}
			after, err := json.Marshal(c)
			if err != nil {
				t.Fatalf("marshal after: %v", err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("row mutated by historicalFailureStepID")
			}
		})
	}
}

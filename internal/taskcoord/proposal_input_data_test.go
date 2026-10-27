package taskcoord

import (
	"encoding/json"
	"os"
	"testing"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

// proposalInputFixture holds the fixture inputs for proposal input tests.
type proposalInputFixture struct {
	Coordinator *Service
	Proposal    taskengine.CodeProposal
	Outer       product.Artifact
	Bundle      proposalBundle
	Input       product.Artifact
	Canonical   []byte
	Task        worker.Task
}

// proposalInputGolden holds the expected golden values for proposal input tests.
type proposalInputGolden struct {
	TaskJSON   string        `json:"task_json"`
	Result     worker.Result `json:"result"`
	TaskID     string        `json:"task_id"`
	StepID     string        `json:"step_id"`
	AttemptID  string        `json:"attempt_id"`
	PacketHash string        `json:"packet_hash"`
}

// readProposalInputGolden reads the proposal input golden fixture and returns the parsed values.
func readProposalInputGolden(t *testing.T) (proposalInputGolden, worker.Task) {
	t.Helper()
	body, err := os.ReadFile("proposal_input_fixture.json")
	if err != nil {
		t.Fatalf("read proposal_input_fixture.json: %v", err)
	}
	var golden proposalInputGolden
	if err := json.Unmarshal(body, &golden); err != nil {
		t.Fatalf("unmarshal proposal_input_fixture.json: %v", err)
	}
	var task worker.Task
	if err := json.Unmarshal([]byte(golden.TaskJSON), &task); err != nil {
		t.Fatalf("unmarshal task_json: %v", err)
	}
	return golden, task
}

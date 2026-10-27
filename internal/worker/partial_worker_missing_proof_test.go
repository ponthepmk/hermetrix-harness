package worker

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestMissingProofRejectsRecomputedID(t *testing.T) {
	task, _, _ := partialWorkerFixture(t)
	result, err := Run(context.Background(), &fake{arguments: partialWorkerArguments}, providers.Profile{}, "", task)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("changes: %d", len(result.Changes))
	}
	if err := ValidateResult(task, result); err != nil {
		t.Fatalf("ValidateResult: %v", err)
	}

	result.Changes[0].Edits = nil
	result.ProposalID = proposalID(result.InputSHA256, result)

	if err := ValidateResult(task, result); err == nil {
		t.Fatalf("expected rejection after edits removed")
	}
}

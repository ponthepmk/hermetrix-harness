package worker

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestPartialWorkerStrategyProof(t *testing.T) {
	task, originalFullSource, _ := partialWorkerFixture(t)
	fake := &fake{arguments: partialWorkerArguments}
	result, err := Run(context.Background(), fake, providers.Profile{}, "", task)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("changes: %d", len(result.Changes))
	}
	if err := ValidateResult(task, result); err != nil {
		t.Fatalf("ValidateResult: %v", err)
	}

	result.Changes[0].Strategy = "replace_file"
	result.ProposalID = proposalID(result.InputSHA256, result)
	if err := ValidateResult(task, result); err == nil {
		t.Fatalf("ValidateResult after attack: expected error")
	}
	_ = originalFullSource
}

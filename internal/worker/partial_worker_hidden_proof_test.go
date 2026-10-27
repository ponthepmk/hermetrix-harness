package worker

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestPartialWorkerHiddenProof(t *testing.T) {
	task, originalFullSource, _ := partialWorkerFixture(t)
	result, err := Run(context.Background(), &fake{arguments: partialWorkerArguments}, providers.Profile{}, "", task)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := ValidateResult(task, result); err != nil {
		t.Fatalf("ValidateResult before attack: %v", err)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("expected one change, got %d", len(result.Changes))
	}
	c := result.Changes[0].Content
	c = "X" + c[1:]
	result.Changes[0].Content = c
	result.ProposalID = proposalID(result.InputSHA256, result)
	if err := ValidateResult(task, result); err == nil {
		t.Fatal("ValidateResult after attack: expected error")
	}
	_ = originalFullSource
}

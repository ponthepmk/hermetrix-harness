package worker

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestPartialWorkerProofRejectsForgedEdit(t *testing.T) {
	task, original, expected := partialWorkerFixture(t)
	fake := &fake{arguments: partialWorkerArguments}
	result, err := Run(context.Background(), fake, providers.Profile{}, "", task)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if err := ValidateResult(task, result); err != nil {
		t.Fatalf("ValidateResult before attack: %v", err)
	}

	result.Changes[0].Edits[0].New = "return a*b"
	result.ProposalID = proposalID(result.InputSHA256, result)
	if err := ValidateResult(task, result); err == nil {
		t.Fatalf("forged edit accepted; expected error")
	}
	_ = original
	_ = expected
}

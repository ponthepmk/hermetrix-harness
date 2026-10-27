package worker

import (
	"context"
	"hermetrix-harness/internal/providers"
	"strings"
	"testing"
)

const (
	privateBoundaryMarker = "PRIVATE_BOUNDARY_MARKER"
	changed               = "CHANGED"
)

func TestPartialWorkerRejectsOutsideViewEdit(t *testing.T) {
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
	result.Changes[0].Edits = []TextEdit{{Old: privateBoundaryMarker, New: changed}}
	result.Changes[0].Content = strings.Replace(originalFullSource, privateBoundaryMarker, changed, 1)
	result.ProposalID = proposalID(result.InputSHA256, result)
	if err := ValidateResult(task, result); err == nil {
		t.Fatal("ValidateResult after outside-view attack: expected error")
	}
}

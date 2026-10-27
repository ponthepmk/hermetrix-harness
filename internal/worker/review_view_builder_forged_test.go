package worker

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestReviewViewBuilderForged(t *testing.T) {
	task, _, _ := partialWorkerFixture(t)
	f := &fake{arguments: partialWorkerArguments}
	result, err := Run(context.Background(), f, providers.Profile{}, "", task)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != "proposed_unverified" {
		t.Fatalf("status: %q", result.Status)
	}
	if len(result.Changes) == 0 {
		t.Fatal("no changes")
	}
	before, after, err := BuildReviewFiles(task, result)
	if err != nil {
		t.Fatalf("BuildReviewFiles: %v", err)
	}
	if before == nil || after == nil {
		t.Fatal("maps nil")
	}
	mutated := result
	mutated.Changes[0].Content = "X" + mutated.Changes[0].Content
	mutated.ProposalID = proposalID(mutated.InputSHA256, mutated)
	before2, after2, err2 := BuildReviewFiles(task, mutated)
	if err2 == nil {
		t.Fatal("expected error")
	}
	if before2 != nil || after2 != nil {
		t.Fatalf("maps not nil: %v %v", before2, after2)
	}
}

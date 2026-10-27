package taskcoord

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"hermetrix-harness/internal/product"
)

func TestLoadProposalBundleRoundtrip(t *testing.T) {
	f := newProposalInputFixture(t)
	got, err := f.Coordinator.loadProposalBundle(context.Background(), f.Proposal)
	if err != nil {
		t.Fatalf("loadProposalBundle: %v", err)
	}
	if got.InputTask == nil {
		t.Fatal("got nil InputTask")
	}
	if !reflect.DeepEqual(*got.InputTask, f.Task) {
		t.Fatalf("InputTask mismatch: got %+v want %+v", got.InputTask, f.Task)
	}
	if got.Artifact.ID != f.Outer.ID {
		t.Fatalf("Artifact.ID mismatch: got %q want %q", got.Artifact.ID, f.Outer.ID)
	}
	if got.Bundle.InputArtifactID != f.Input.ID {
		t.Fatalf("Bundle.InputArtifactID mismatch: got %q want %q", got.Bundle.InputArtifactID, f.Input.ID)
	}
}

func TestLoadProposalBundleMissingInputReturnsZero(t *testing.T) {
	f := newProposalInputFixture(t)
	bundle := f.Bundle
	bundle.InputArtifactID = "nonexistent-input"
	body, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	newArtifact, err := f.Coordinator.products.CreateArtifact(context.Background(), product.ArtifactInput{
		ProjectID: f.Outer.ProjectID,
		Name:      "missing-input",
		Kind:      "code_proposal",
		MIMEType:  "application/json",
		Content:   string(body),
		Metadata:  f.Outer.Metadata,
	})
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	proposal := f.Proposal
	proposal.ArtifactID = newArtifact.ID
	proposal.ResultHash = newArtifact.Checksum
	loaded, err := f.Coordinator.loadProposalBundle(context.Background(), proposal)
	if err == nil {
		t.Fatal("expected error for missing input")
	}
	if !reflect.DeepEqual(loaded, loadedProposal{}) {
		t.Fatalf("loadedProposal not zero: %+v", loaded)
	}
}

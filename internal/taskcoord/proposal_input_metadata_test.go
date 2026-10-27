package taskcoord

import (
	"context"
	"reflect"
	"testing"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/worker"
)

func TestLoadProposalInputMetadata(t *testing.T) {
	f := newProposalInputFixture(t)
	ctx := context.Background()
	got, err := f.Coordinator.loadProposalInput(ctx, f.Proposal, f.Outer, f.Bundle)
	if err != nil {
		t.Fatalf("positive control: %v", err)
	}
	if !reflect.DeepEqual(got, f.Task) {
		t.Fatalf("positive control: task mismatch")
	}
	for _, key := range []string{"task_id", "step_id", "attempt_id", "packet_hash", "proposal_id", "project_root"} {
		for _, mode := range []string{"missing", "wrongstring", "wrongtype"} {
			meta := make(map[string]any, len(f.Input.Metadata))
			for k, v := range f.Input.Metadata {
				meta[k] = v
			}
			switch mode {
			case "missing":
				delete(meta, key)
			case "wrongstring":
				meta[key] = "wrong"
			case "wrongtype":
				meta[key] = 7
			}
			in := f.Input
			in.Metadata = meta
			newIn, err := f.Coordinator.products.CreateArtifact(ctx, product.ArtifactInput{
				ProjectID: f.Proposal.ProjectID,
				Name:      "proposal-input-" + key + "-" + mode,
				Kind:      "code_proposal_input",
				MIMEType:  "application/json",
				Content:   string(f.Canonical),
				Metadata:  meta,
			})
			if err != nil {
				t.Fatalf("CreateArtifact %s/%s: %v", key, mode, err)
			}
			b := f.Bundle
			b.InputArtifactID = newIn.ID
			got, err = f.Coordinator.loadProposalInput(ctx, f.Proposal, f.Outer, b)
			if err == nil {
				t.Fatalf("%s/%s: expected error", key, mode)
			}
			if !reflect.DeepEqual(got, worker.Task{}) {
				t.Fatalf("%s/%s: expected zero Task", key, mode)
			}
		}
	}
}

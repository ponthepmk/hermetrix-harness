package taskcoord

import (
	"context"
	"encoding/json"
	"testing"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
)

func newProposalInputFixture(t *testing.T) proposalInputFixture {
	c, _, ownedTask, _, _, _ := newGoalFixtureWithStore(t, &goalAdapter{}, taskengine.GoalContract{WritablePaths: []string{"sum.go"}})
	ctx := context.Background()
	root, err := c.products.ProjectRootIdentity(ctx, ownedTask.ProjectID)
	if err != nil {
		t.Fatalf("project root identity: %v", err)
	}
	golden, task := readProposalInputGolden(t)
	proposal := taskengine.CodeProposal{
		ID:         golden.Result.ProposalID,
		TaskID:     golden.TaskID,
		StepID:     golden.StepID,
		AttemptID:  golden.AttemptID,
		PacketHash: golden.PacketHash,
		ProjectID:  ownedTask.ProjectID,
	}
	input, err := c.products.CreateArtifact(ctx, product.ArtifactInput{
		ProjectID: ownedTask.ProjectID,
		Name:      "fixture-input",
		Kind:      "code_proposal_input",
		MIMEType:  "application/json",
		Content:   golden.TaskJSON,
		Metadata: map[string]any{
			"task_id":      golden.TaskID,
			"step_id":      golden.StepID,
			"attempt_id":   golden.AttemptID,
			"packet_hash":  golden.PacketHash,
			"proposal_id":  golden.Result.ProposalID,
			"project_root": root,
		},
	})
	if err != nil {
		t.Fatalf("create input artifact: %v", err)
	}
	bundle := proposalBundle{
		Version:         2,
		InputArtifactID: input.ID,
		Result:          golden.Result,
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	outer, err := c.products.CreateArtifact(ctx, product.ArtifactInput{
		ProjectID: ownedTask.ProjectID,
		Name:      "fixture-proposal",
		Kind:      "code_proposal",
		MIMEType:  "application/json",
		Content:   string(encoded),
		Metadata: map[string]any{
			"project_root": root,
		},
	})
	if err != nil {
		t.Fatalf("create outer artifact: %v", err)
	}
	proposal.ArtifactID = outer.ID
	proposal.ResultHash = outer.Checksum
	return proposalInputFixture{
		Coordinator: c,
		Proposal:    proposal,
		Outer:       outer,
		Bundle:      bundle,
		Input:       input,
		Canonical:   []byte(golden.TaskJSON),
		Task:        task,
	}
}

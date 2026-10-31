package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
	"strings"
)

func (s *Service) Apply(ctx context.Context, authority taskengine.RunAuthority, proposalID, actor string) (ApplyOutput, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ApplyOutput{}, fmt.Errorf("apply actor is required")
	}
	proposal, err := s.tasks.GetCodeProposal(ctx, proposalID)
	if err != nil {
		return ApplyOutput{}, err
	}
	if proposal.State != taskengine.ProposalApproved {
		return ApplyOutput{}, fmt.Errorf("proposal cannot apply from state %s", proposal.State)
	}
	loaded, err := s.loadProposalBundle(ctx, proposal)
	if err != nil {
		return ApplyOutput{}, err
	}
	artifact := loaded.Artifact
	result := loaded.Bundle.Result

	frozenRoot, _ := artifact.Metadata["project_root"].(string)
	if frozenRoot != "" {
		if currentRoot, rootErr := s.products.ProjectRootIdentity(ctx, proposal.ProjectID); rootErr != nil || currentRoot != frozenRoot {
			return ApplyOutput{}, fmt.Errorf("proposal project root changed since source snapshot")
		}
	}
	task, err := s.tasks.Get(ctx, proposal.TaskID)
	if err != nil {
		return ApplyOutput{}, err
	}
	for _, change := range result.Changes {
		allowed := len(task.Requirement.Contract.WritablePaths) == 0
		if change.BeforeSHA256 == "absent" && len(task.Requirement.Contract.WritablePaths) == 0 {
			return ApplyOutput{}, fmt.Errorf("new files require explicit frozen writable paths")
		}
		if change.BeforeSHA256 == "absent" && frozenRoot == "" {
			return ApplyOutput{}, fmt.Errorf("creation proposal requires a frozen project root")
		}
		for _, path := range task.Requirement.Contract.WritablePaths {
			allowed = allowed || path == change.Path
		}
		if !allowed {
			return ApplyOutput{}, fmt.Errorf("proposal path %q is outside the frozen writable paths", change.Path)
		}
	}
	preimages := make(map[string]product.FileDocument, len(result.Changes))
	for _, change := range result.Changes {
		document, readErr := s.products.SnapshotProjectFileAtRoot(ctx, proposal.ProjectID, frozenRoot, change.Path)
		if readErr != nil {
			return ApplyOutput{}, readErr
		}
		if document.SHA256 != change.BeforeSHA256 {
			return ApplyOutput{}, fmt.Errorf("proposal preimage for %s is stale", change.Path)
		}
		preimages[change.Path] = document
	}
	rollbackBody, err := json.Marshal(preimages)
	if err != nil {
		return ApplyOutput{}, err
	}
	rollbackArtifact, err := s.products.CreateArtifact(ctx, product.ArtifactInput{
		ProjectID: proposal.ProjectID, Name: proposal.ID + ".rollback.json", Kind: "code_proposal_rollback",
		MIMEType: "application/vnd.hermetrix.rollback+json", Content: string(rollbackBody),
		Metadata: map[string]any{"proposal_id": proposal.ID, "packet_hash": proposal.PacketHash, "project_root": frozenRoot},
	})
	if err != nil {
		return ApplyOutput{}, err
	}
	effect, err := s.tasks.PlanEffect(ctx, authority, proposal.AttemptID, "workspace.apply", "proposal:"+proposal.ID, "review:"+proposal.ID)
	if err != nil {
		return ApplyOutput{}, err
	}
	proposal, err = s.tasks.BeginCodeProposalApply(ctx, proposal.ID, rollbackArtifact.ID)
	if err != nil {
		_, _ = s.tasks.AbandonEffect(ctx, effect.OperationID, "proposal state changed before apply")
		return ApplyOutput{Effect: effect, RollbackArtifact: rollbackArtifact}, err
	}
	effect, err = s.tasks.DispatchEffect(ctx, authority, effect.OperationID)
	if err != nil {
		_, _ = s.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalApplying, taskengine.ProposalApplyFailed)
		return ApplyOutput{Proposal: proposal, Effect: effect, RollbackArtifact: rollbackArtifact}, err
	}
	writeInputs := make([]product.WriteFileInput, 0, len(result.Changes))
	for _, change := range result.Changes {
		writeInputs = append(writeInputs, product.WriteFileInput{
			Path: change.Path, Content: change.Content, ExpectedSHA256: change.BeforeSHA256, Actor: actor,
		})
	}
	batch, writeErr := s.products.WriteProjectFilesAtRoot(ctx, proposal.ProjectID, frozenRoot, writeInputs)
	receipts := batch.Receipts
	if writeErr != nil {
		status := "apply_failed_before_write"
		if len(receipts) > 0 {
			status = "rollback_complete"
			if len(batch.Rollback) != len(receipts) {
				status = "rollback_incomplete"
			} else {
				for _, rollback := range batch.Rollback {
					if rollback.State != "restored" {
						status = "rollback_incomplete"
						break
					}
				}
			}
		}
		effect, _ = s.tasks.ObserveEffect(context.WithoutCancel(ctx), effect.OperationID,
			map[string]any{"status": status, "error": writeErr.Error(), "rollback": batch.Rollback})
		proposal, _ = s.tasks.TransitionCodeProposal(context.WithoutCancel(ctx), proposal.ID, taskengine.ProposalApplying, taskengine.ProposalApplyFailed)
		return ApplyOutput{Proposal: proposal, Effect: effect, Receipts: receipts, RollbackArtifact: rollbackArtifact}, writeErr
	}
	effect, err = s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "applied_unverified", "files": len(receipts), "rollback_artifact_id": rollbackArtifact.ID})
	if err != nil {
		return ApplyOutput{Proposal: proposal, Receipts: receipts, RollbackArtifact: rollbackArtifact}, err
	}
	proposal, err = s.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalApplying, taskengine.ProposalApplied)
	return ApplyOutput{Proposal: proposal, Effect: effect, Receipts: receipts, RollbackArtifact: rollbackArtifact}, err
}

package taskcoord

import (
	"context"
	"errors"
	"fmt"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

// loadedProposal is the private loaded shape of a proposal.
type loadedProposal struct {
	Artifact  product.Artifact
	Bundle    proposalBundle
	InputTask *worker.Task
}

func (s *Service) loadProposalBundle(ctx context.Context, proposal taskengine.CodeProposal) (loadedProposal, error) {
	var out loadedProposal
	if s == nil || s.products == nil {
		return out, errors.New("service and products must be nonnil")
	}
	artifact, body, err := s.products.GetArtifact(ctx, proposal.ArtifactID)
	if err != nil {
		return out, fmt.Errorf("get proposal artifact: %w", err)
	}
	if artifact.Kind != "code_proposal" || artifact.ProjectID != proposal.ProjectID {
		return out, fmt.Errorf("proposal artifact kind/project mismatch: kind=%q project=%q", artifact.Kind, artifact.ProjectID)
	}
	if len(body) == 0 || len(body) > maxProposalBodyBytes {
		return out, fmt.Errorf("proposal body must be nonempty and at most 16MiB, got %d", len(body))
	}
	if computed := worker.Hash(string(body)); computed != artifact.Checksum || computed != proposal.ResultHash {
		return out, fmt.Errorf("proposal checksum mismatch: artifact=%q proposal=%q computed=%q", artifact.Checksum, proposal.ResultHash, computed)
	}
	bundle, err := decodeProposalBundle(body)
	if err != nil {
		return out, fmt.Errorf("decode proposal bundle: %w", err)
	}
	if bundle.Result.ProposalID != proposal.ID {
		return out, fmt.Errorf("decoded proposal id mismatch: decoded=%q proposal=%q", bundle.Result.ProposalID, proposal.ID)
	}
	if bundle.Version == 1 {
		out.Artifact = artifact
		out.Bundle = bundle
		return out, nil
	}
	inputTask, err := s.loadProposalInput(ctx, proposal, artifact, bundle)
	if err != nil {
		return out, fmt.Errorf("load proposal input: %w", err)
	}
	out.Artifact = artifact
	out.Bundle = bundle
	out.InputTask = &inputTask
	return out, nil
}

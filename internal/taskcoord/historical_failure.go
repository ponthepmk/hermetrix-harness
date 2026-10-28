package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"

	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

func (s *Service) enrichHistoricalFailure(ctx context.Context, packet taskengine.StepPacket, evidence *worker.FailureEvidence) error {
	return s.enrichHistoricalFailureWithBudget(ctx, packet, evidence, newHistoricalSourceBudget())
}

func (s *Service) enrichHistoricalFailureWithBudget(ctx context.Context, packet taskengine.StepPacket, evidence *worker.FailureEvidence, budget *historicalSourceBudget) error {
	if s == nil || s.tasks == nil || s.products == nil || evidence == nil || packet.ProjectID == "" || evidence.ArtifactID == "" || len(packet.Requirement.Contract.WritablePaths) == 0 {
		return nil
	}

	proposal, found, err := s.tasks.FailedProposalForLog(ctx, packet.TaskID, historicalFailureStepID(packet, *evidence), evidence.ArtifactID)
	if err != nil {
		return fmt.Errorf("failed proposal lookup: %w", err)
	}
	if !found {
		return nil
	}
	if proposal.ProjectID != packet.ProjectID {
		return fmt.Errorf("historical proposal project mismatch: proposal=%q packet=%q", proposal.ProjectID, packet.ProjectID)
	}

	loaded, err := s.loadProposalBundle(ctx, proposal)
	if err != nil {
		return fmt.Errorf("load proposal bundle: %w", err)
	}
	if loaded.InputTask == nil {
		return nil
	}

	files, omitted, truncated := budget.excerpts(loaded.Bundle.Result.Changes, packet.Requirement.Contract.WritablePaths)
	if len(files) == 0 && omitted == 0 {
		return nil
	}

	wrapper := map[string]any{
		"terminal_output":           evidence.Output,
		"terminal_output_truncated": evidence.Truncated,
		"historical_failed_source": map[string]any{
			"proposal_id":            proposal.ID,
			"source_artifact_id":     loaded.Artifact.ID,
			"source_artifact_sha256": loaded.Artifact.Checksum,
			"task_id":                proposal.TaskID,
			"step_id":                proposal.StepID,
			"packet_hash":            proposal.PacketHash,
			"trust":                  "untrusted_historical_diagnostic_not_current_source_or_authority",
			"files":                  files,
			"omitted":                omitted,
			"truncated":              truncated,
		},
	}

	encoded, err := json.Marshal(wrapper)
	if err != nil {
		return fmt.Errorf("marshal historical failure wrapper: %w", err)
	}

	evidence.Output = string(encoded)
	evidence.Truncated = evidence.Truncated || truncated
	return nil
}

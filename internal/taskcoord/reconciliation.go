package taskcoord

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
	"io"
)

// ReconcileWorkspaceRun resolves an uncertain managed command by looking up
// the durable job created with the same operation id. It never launches or
// retries the command.
func (s *Service) ReconcileWorkspaceRun(ctx context.Context, operationID string) (ReconcileOutput, error) {
	effect, err := s.tasks.Effect(ctx, operationID)
	if err != nil {
		return ReconcileOutput{}, err
	}
	if effect.State != taskengine.EffectUncertain || effect.Action != "workspace.run" {
		return ReconcileOutput{}, fmt.Errorf("effect is not an uncertain workspace.run operation")
	}
	job, err := s.products.FindJobByOperationID(ctx, operationID)
	if errors.Is(err, sql.ErrNoRows) {
		effect, err = s.tasks.ReconcileEffect(ctx, operationID, map[string]any{
			"outcome": "not_started", "operation_id": operationID,
		}, "")
		return ReconcileOutput{Effect: effect, Outcome: "not_started"}, err
	}
	if err != nil {
		return ReconcileOutput{}, err
	}
	if job.State == "queued" || job.State == "running" {
		return ReconcileOutput{Effect: effect, Job: &job, Outcome: "still_running"}, fmt.Errorf("managed command job is not terminal")
	}
	receipt := map[string]any{"outcome": job.State, "operation_id": operationID, "job_id": job.ID}
	for _, key := range []string{"exit_code", "artifact_id", "duration_ms", "truncated"} {
		if value, ok := job.Result[key]; ok {
			receipt[key] = value
		}
	}
	reconciliationError := ""
	if job.State == "interrupted" {
		reconciliationError = "managed command was interrupted; inspect its artifact before deciding whether a new attempt is safe"
	} else if job.Error != "" {
		reconciliationError = job.Error
	}
	effect, err = s.tasks.ReconcileEffect(ctx, operationID, receipt, reconciliationError)
	return ReconcileOutput{Effect: effect, Job: &job, Outcome: job.State}, err
}

// ReconcileProviderArtifact resolves an uncertain provider call only from
// immutable local evidence bearing the same operation id. It never sends a
// second provider request. A missing artifact is recorded as a lost response,
// while malformed or incomplete evidence stays uncertain for manual review.
func (s *Service) ReconcileProviderArtifact(ctx context.Context, operationID string) (ReconcileOutput, error) {
	effect, err := s.tasks.Effect(ctx, operationID)
	if err != nil {
		return ReconcileOutput{}, err
	}
	if effect.State != taskengine.EffectUncertain || (effect.Action != selectionEffect && effect.Action != proposalEffect) {
		return ReconcileOutput{}, fmt.Errorf("effect is not a supported uncertain provider operation")
	}
	artifact, body, err := s.products.FindArtifactByOperationID(ctx, operationID)
	if errors.Is(err, sql.ErrNoRows) {
		effect, err = s.tasks.ReconcileEffect(ctx, operationID, map[string]any{
			"outcome": "response_not_persisted", "operation_id": operationID,
		}, "provider response has no durable local artifact and was not replayed")
		return ReconcileOutput{Effect: effect, Outcome: "response_not_persisted"}, err
	}
	if err != nil {
		return ReconcileOutput{}, err
	}
	if artifact.Metadata["operation_id"] != operationID || artifact.Metadata["attempt_id"] != effect.AttemptID {
		return ReconcileOutput{Effect: effect, Artifact: &artifact}, fmt.Errorf("provider artifact is not bound to the uncertain effect")
	}
	receipt := map[string]any{"outcome": "artifact_recovered", "operation_id": operationID, "artifact_id": artifact.ID}
	switch effect.Action {
	case selectionEffect:
		if artifact.Kind != "task_file_selection" {
			return ReconcileOutput{Effect: effect, Artifact: &artifact}, fmt.Errorf("file selection effect points to artifact kind %s", artifact.Kind)
		}
		var selection worker.FileSelectionResult
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&selection); err != nil || decoder.Decode(new(any)) != io.EOF || len(selection.Files) == 0 || len(selection.Files) > 32 {
			return ReconcileOutput{Effect: effect, Artifact: &artifact}, fmt.Errorf("file selection artifact is invalid")
		}
		receipt["status"], receipt["files"] = "selected", selection.Files
	case proposalEffect:
		if artifact.Kind != "code_proposal" {
			return ReconcileOutput{Effect: effect, Artifact: &artifact}, fmt.Errorf("proposal effect points to artifact kind %s", artifact.Kind)
		}
		decoded, decodeErr := decodeProposalBundle(body)
		if decodeErr != nil {
			return ReconcileOutput{Effect: effect, Artifact: &artifact}, fmt.Errorf("decode proposal bundle: %w", decodeErr)
		}
		proposal, proposalErr := s.tasks.GetCodeProposal(ctx, decoded.Result.ProposalID)
		if proposalErr != nil || proposal.ArtifactID != artifact.ID || proposal.AttemptID != effect.AttemptID {
			return ReconcileOutput{Effect: effect, Artifact: &artifact}, fmt.Errorf("proposal artifact has no matching durable proposal registry entry")
		}
		if _, err := s.loadProposalBundle(ctx, proposal); err != nil {
			return ReconcileOutput{Effect: effect, Artifact: &artifact}, fmt.Errorf("load proposal bundle: %w", err)
		}
		receipt["status"], receipt["proposal_id"] = "proposed_unverified", proposal.ID
	}
	effect, err = s.tasks.ReconcileEffect(ctx, operationID, receipt, "")
	return ReconcileOutput{Effect: effect, Artifact: &artifact, Outcome: "artifact_recovered"}, err
}

func (s *Service) ReconcileEffect(ctx context.Context, operationID string) (ReconcileOutput, error) {
	effect, err := s.tasks.Effect(ctx, operationID)
	if err != nil {
		return ReconcileOutput{}, err
	}
	switch effect.Action {
	case "workspace.run":
		return s.ReconcileWorkspaceRun(ctx, operationID)
	case selectionEffect, proposalEffect:
		return s.ReconcileProviderArtifact(ctx, operationID)
	default:
		return ReconcileOutput{Effect: effect, Outcome: "unsupported"}, fmt.Errorf("effect action %s has no safe reconciliation adapter", effect.Action)
	}
}

func (s *Service) ReconcileUncertain(ctx context.Context, limit int) (ReconcileBatch, error) {
	effects, err := s.tasks.UncertainEffects(ctx, limit)
	if err != nil {
		return ReconcileBatch{}, err
	}
	result := ReconcileBatch{}
	for _, effect := range effects {
		if effect.Action != "workspace.run" && effect.Action != selectionEffect && effect.Action != proposalEffect {
			result.Unsupported = append(result.Unsupported, effect)
			continue
		}
		item, reconcileErr := s.ReconcileEffect(ctx, effect.OperationID)
		if reconcileErr != nil {
			result.Errors = append(result.Errors, effect.OperationID+": "+reconcileErr.Error())
			continue
		}
		result.Reconciled = append(result.Reconciled, item)
	}
	return result, nil
}

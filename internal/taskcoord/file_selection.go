package taskcoord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
	"io"
	"strings"
)

// SelectFiles creates a durable, non-replayed provider effect from a local,
// secret-filtered manifest. Only paths and sizes cross this boundary; file
// contents remain local until the separately recorded proposal effect.
func (s *Service) SelectFiles(ctx context.Context, input SelectFilesInput) (SelectFilesOutput, error) {
	if s == nil || s.tasks == nil || s.products == nil || s.providers == nil {
		return SelectFilesOutput{}, fmt.Errorf("task coordinator is unavailable")
	}
	input.AttemptID, input.ProviderID = strings.TrimSpace(input.AttemptID), strings.TrimSpace(input.ProviderID)
	if input.AttemptID == "" || input.ProviderID == "" {
		return SelectFilesOutput{}, fmt.Errorf("attempt and provider are required")
	}
	attempt, err := s.tasks.Attempt(ctx, input.AttemptID)
	if err != nil {
		return SelectFilesOutput{}, err
	}
	if attempt.State != taskengine.AttemptRunning || attempt.Packet == nil {
		return SelectFilesOutput{}, fmt.Errorf("file selection requires a running attempt with a durable step packet")
	}
	packet := *attempt.Packet
	if err = taskengine.ValidateStepPacket(packet); err != nil {
		return SelectFilesOutput{}, err
	}
	if packet.ProjectID == "" || packet.CanonicalPacketHash != attempt.InputHash {
		return SelectFilesOutput{}, fmt.Errorf("attempt packet is not bound to a project or its input hash")
	}
	effects, err := s.tasks.AttemptEffects(ctx, attempt.ID)
	if err != nil {
		return SelectFilesOutput{}, err
	}
	for index := len(effects) - 1; index >= 0; index-- {
		effect := effects[index]
		if effect.Action != selectionEffect {
			continue
		}
		if (effect.State == taskengine.EffectObserved || effect.State == taskengine.EffectReconciled) && effect.Receipt["status"] == "selected" {
			artifactID, _ := effect.Receipt["artifact_id"].(string)
			artifact, body, loadErr := s.products.GetArtifact(ctx, artifactID)
			if loadErr != nil {
				return SelectFilesOutput{Effect: effect}, fmt.Errorf("load persisted file selection: %w", loadErr)
			}
			var result worker.FileSelectionResult
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.DisallowUnknownFields()
			if loadErr = decoder.Decode(&result); loadErr != nil || decoder.Decode(new(any)) != io.EOF || len(result.Files) == 0 {
				return SelectFilesOutput{Artifact: artifact, Effect: effect}, fmt.Errorf("persisted file selection is invalid")
			}
			return SelectFilesOutput{Result: result, Artifact: artifact, Effect: effect}, nil
		}
		return SelectFilesOutput{Effect: effect}, fmt.Errorf("file selection already has durable state %s; inspect or reconcile it rather than replaying the provider call", effect.State)
	}
	profile, err := s.providers.Get(ctx, input.ProviderID)
	if err != nil {
		return SelectFilesOutput{}, err
	}
	task, err := s.tasks.Get(ctx, packet.TaskID)
	if err != nil {
		return SelectFilesOutput{}, err
	}
	if err := validateTaskEgress(task, profile); err != nil {
		return SelectFilesOutput{}, err
	}
	preset, err := s.codeProposalPreset(ctx, profile)
	if err != nil {
		return SelectFilesOutput{}, err
	}
	if err := s.tasks.BindAttemptInference(ctx, attempt.ID, "worker", preset.ID, preset.Revision, profile.RuntimeFingerprintID); err != nil {
		return SelectFilesOutput{}, err
	}
	selectionInput := worker.FileSelectionTask{TaskID: packet.TaskID, Objective: packet.Objective, StepTitle: packet.Step.Title, StepInstructions: packet.Step.Instructions, AcceptanceCriteria: stepCriteria(packet), Constraints: packet.Requirement.Constraints, MaxOutputTokens: minPositive(profile.MaxOutputTokens, 4096)}
	repositoryContext, err := s.selectionRepositoryContext(ctx, packet.ProjectID, packet.Requirement.Contract.WritablePaths, packet.Step.Checks, &selectionInput, preset.ContextMax)
	if err != nil {
		return SelectFilesOutput{}, err
	}
	effect, err := s.tasks.PlanEffect(ctx, input.Authority, attempt.ID, selectionEffect, profile.ID+":"+profile.Model, "task-packet:"+packet.CanonicalPacketHash)
	if err != nil {
		return SelectFilesOutput{}, err
	}
	dispatched := false
	dispatchCtx := inference.WithOwner(ctx, inference.Owner{Kind: "task", ID: packet.TaskID, Source: "file_selector", Priority: inference.PriorityTask,
		PresetID: preset.ID, PresetRevision: preset.Revision, PresetRole: preset.Role, RuntimeFingerprintID: profile.RuntimeFingerprintID})
	result, selectErr := worker.SelectFilesWithProviderService(dispatchCtx, s.providers, profile, selectionInput, worker.Options{BeforeProviderRequest: func() error {
		if bindingErr := s.validateProviderBinding(ctx, profile); bindingErr != nil {
			return bindingErr
		}
		updated, dispatchErr := s.tasks.DispatchEffect(ctx, input.Authority, effect.OperationID)
		if dispatchErr == nil {
			effect, dispatched = updated, true
		}
		return dispatchErr
	}})
	if selectErr != nil {
		if dispatched {
			effect, _ = s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "selection_failed", "error": selectErr.Error()})
		} else {
			effect, _ = s.tasks.AbandonEffect(ctx, effect.OperationID, "selection rejected before provider dispatch")
		}
		return SelectFilesOutput{Effect: effect}, selectErr
	}
	body, _ := json.Marshal(result)
	artifact, err := s.products.CreateArtifact(ctx, product.ArtifactInput{ProjectID: packet.ProjectID,
		Name: attempt.ID + ".file-selection.json", Kind: "task_file_selection",
		MIMEType: "application/vnd.hermetrix.file-selection+json", Content: string(body),
		Metadata: map[string]any{"repository_context": repositoryContext, "task_id": packet.TaskID, "step_id": packet.Step.ID, "attempt_id": attempt.ID,
			"packet_hash": packet.CanonicalPacketHash, "provider_id": profile.ID, "provider_revision": profile.Revision,
			"operation_id": effect.OperationID},
	})
	if err != nil {
		effect, _ = s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "artifact_persist_failed"})
		return SelectFilesOutput{Result: result, Effect: effect}, err
	}
	effect, err = s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "selected", "files": result.Files, "artifact_id": artifact.ID})
	if err != nil {
		return SelectFilesOutput{Result: result, Artifact: artifact}, err
	}
	return SelectFilesOutput{Result: result, Artifact: artifact, Effect: effect}, nil
}

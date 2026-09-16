// Package taskcoord connects the durable task state machine to bounded,
// proposal-only model workers. It may read explicitly selected project files
// and persist a proposal artifact, but it never writes source files or claims
// that a proposal passed review or tests.
package taskcoord

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

const (
	selectionEffect = "provider.select_files"
	proposalEffect  = "provider.propose"
)

type Service struct {
	tasks     *taskengine.Service
	products  *product.Service
	providers *providers.Service
}

type ProposalInput struct {
	AttemptID  string                `json:"attempt_id"`
	ProviderID string                `json:"provider_id"`
	Packet     taskengine.StepPacket `json:"packet"`
	Files      []string              `json:"files"`
}

type ProposalOutput struct {
	Result   worker.Result           `json:"result"`
	Artifact product.Artifact        `json:"artifact"`
	Effect   taskengine.EffectIntent `json:"effect"`
	Proposal taskengine.CodeProposal `json:"proposal"`
}

type SelectFilesInput struct {
	AttemptID  string `json:"attempt_id"`
	ProviderID string `json:"provider_id"`
}

type SelectFilesOutput struct {
	Result   worker.FileSelectionResult `json:"result"`
	Artifact product.Artifact           `json:"artifact"`
	Effect   taskengine.EffectIntent    `json:"effect"`
}

type ApplyOutput struct {
	Proposal         taskengine.CodeProposal   `json:"proposal"`
	Effect           taskengine.EffectIntent   `json:"effect"`
	Receipts         []product.WriteFileResult `json:"receipts"`
	RollbackArtifact product.Artifact          `json:"rollback_artifact"`
}

type CommandCheck struct {
	ID             string   `json:"id"`
	RequirementIDs []string `json:"requirement_ids,omitempty"`
	Executable     string   `json:"executable"`
	Arguments      []string `json:"arguments"`
	WorkingDir     string   `json:"working_dir"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type VerifyOutput struct {
	Proposal    taskengine.CodeProposal   `json:"proposal"`
	Jobs        []product.Job             `json:"jobs"`
	Validations []taskengine.Validation   `json:"validations"`
	Effects     []taskengine.EffectIntent `json:"effects"`
	Evidence    product.Artifact          `json:"evidence"`
	RolledBack  bool                      `json:"rolled_back"`
}

type verificationBundle struct {
	ProposalID string                  `json:"proposal_id"`
	Checks     []CommandCheck          `json:"checks"`
	Jobs       []product.Job           `json:"jobs"`
	Evidence   []taskengine.Validation `json:"evidence"`
}

type ReviewOutput struct {
	Review     worker.ReviewResult     `json:"review"`
	Artifact   product.Artifact        `json:"artifact"`
	Proposal   taskengine.CodeProposal `json:"proposal"`
	Task       taskengine.Task         `json:"task"`
	Effect     taskengine.EffectIntent `json:"effect"`
	RolledBack bool                    `json:"rolled_back"`
}

type ReconcileOutput struct {
	Effect   taskengine.EffectIntent `json:"effect"`
	Job      *product.Job            `json:"job,omitempty"`
	Artifact *product.Artifact       `json:"artifact,omitempty"`
	Outcome  string                  `json:"outcome"`
}

type ReconcileBatch struct {
	Reconciled  []ReconcileOutput         `json:"reconciled"`
	Unsupported []taskengine.EffectIntent `json:"unsupported"`
	Errors      []string                  `json:"errors"`
}

type AutoPlanInput struct {
	TaskID               string `json:"task_id"`
	ExpectedTaskRevision int    `json:"expected_task_revision"`
	ProviderID           string `json:"provider_id"`
	Actor                string `json:"actor"`
}

type AutoPlanOutput struct {
	Result   worker.PlanResult     `json:"result"`
	Run      taskengine.PlannerRun `json:"run"`
	Artifact product.Artifact      `json:"artifact"`
	Task     taskengine.Task       `json:"task"`
}

func New(tasks *taskengine.Service, products *product.Service, providerService *providers.Service) *Service {
	return &Service{tasks: tasks, products: products, providers: providerService}
}

func (s *Service) AutoPlan(ctx context.Context, input AutoPlanInput) (AutoPlanOutput, error) {
	input.Actor = strings.TrimSpace(input.Actor)
	if input.Actor == "" || strings.TrimSpace(input.ProviderID) == "" {
		return AutoPlanOutput{}, fmt.Errorf("planner actor and provider are required")
	}
	task, err := s.tasks.Get(ctx, input.TaskID)
	if err != nil {
		return AutoPlanOutput{}, err
	}
	if task.Revision != input.ExpectedTaskRevision {
		return AutoPlanOutput{}, taskengine.ErrStaleRevision
	}
	profile, err := s.providers.Get(ctx, input.ProviderID)
	if err != nil {
		return AutoPlanOutput{}, err
	}
	criteria := make([]string, 0, len(task.Requirement.Criteria))
	for _, criterion := range task.Requirement.Criteria {
		criteria = append(criteria, criterion.ID+": "+criterion.Description)
	}
	planTask := worker.PlanTask{TaskID: task.ID, Objective: task.Objective, OriginalRequest: task.OriginalRequest,
		Constraints: task.Requirement.Constraints, Unknowns: task.Requirement.Unknowns, Criteria: criteria,
		MaxOutputTokens: minPositive(profile.MaxOutputTokens, 8192)}
	encodedInput, _ := json.Marshal(planTask)
	run, err := s.tasks.BeginPlannerRun(ctx, task.ID, task.Revision, profile.ID, profile.Revision, worker.Hash(string(encodedInput)))
	if err != nil {
		return AutoPlanOutput{}, err
	}
	dispatched := false
	result, planErr := worker.PlanWithProviderService(ctx, s.providers, profile, planTask, worker.Options{BeforeProviderRequest: func() error {
		updated, dispatchErr := s.tasks.DispatchPlannerRun(ctx, run.ID)
		if dispatchErr == nil {
			run, dispatched = updated, true
		}
		return dispatchErr
	}})
	if planErr != nil {
		from := taskengine.PlannerPlanned
		if dispatched {
			from = taskengine.PlannerDispatched
		}
		run, _ = s.tasks.FailPlannerRun(ctx, run.ID, from, planErr.Error())
		return AutoPlanOutput{Run: run}, planErr
	}
	planBody, _ := json.Marshal(result)
	artifact, err := s.products.CreateArtifact(ctx, product.ArtifactInput{ProjectID: task.ProjectID,
		Name: task.ID + "-r" + fmt.Sprint(task.ActiveRequirementRevision) + ".plan.json", Kind: "task_plan_proposal",
		MIMEType: "application/vnd.hermetrix.task-plan+json", Content: string(planBody),
		Metadata: map[string]any{"task_id": task.ID, "requirement_revision": task.ActiveRequirementRevision,
			"task_revision": task.Revision, "provider_id": profile.ID, "provider_revision": profile.Revision},
	})
	if err != nil {
		run, _ = s.tasks.FailPlannerRun(ctx, run.ID, taskengine.PlannerDispatched, err.Error())
		return AutoPlanOutput{Result: result, Run: run}, err
	}
	run, err = s.tasks.ObservePlannerRun(ctx, run.ID, artifact.ID)
	if err != nil {
		return AutoPlanOutput{Result: result, Artifact: artifact}, err
	}
	steps := make([]taskengine.StepSpec, 0, len(result.Steps))
	for _, step := range result.Steps {
		steps = append(steps, taskengine.StepSpec{Key: step.Key, Title: step.Title, Instructions: step.Instructions,
			RequirementIDs: step.RequirementIDs, Dependencies: step.Dependencies, Checks: step.Checks, EffectScope: step.EffectScope})
	}
	task, err = s.tasks.CreatePlan(ctx, taskengine.CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Reason: result.Reason, Actor: input.Actor, Steps: steps})
	if err != nil {
		run, _ = s.tasks.RejectPlannerRun(ctx, run.ID, err.Error())
	}
	return AutoPlanOutput{Result: result, Run: run, Artifact: artifact, Task: task}, err
}

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
	manifest, err := s.products.ProjectFileManifest(ctx, packet.ProjectID, 2000)
	if err != nil {
		return SelectFilesOutput{}, err
	}
	if len(manifest) == 0 {
		return SelectFilesOutput{}, fmt.Errorf("project has no eligible source files")
	}
	candidates := make([]worker.FileCandidate, 0, len(manifest))
	for _, item := range manifest {
		candidates = append(candidates, worker.FileCandidate{Path: item.Path, Bytes: item.Bytes})
	}
	criteria := make([]string, 0, len(packet.Requirement.Criteria))
	for _, criterion := range packet.Requirement.Criteria {
		criteria = append(criteria, criterion.ID+": "+criterion.Description)
	}
	effect, err := s.tasks.PlanEffect(ctx, attempt.ID, selectionEffect, profile.ID+":"+profile.Model, "task-packet:"+packet.CanonicalPacketHash)
	if err != nil {
		return SelectFilesOutput{}, err
	}
	dispatched := false
	result, selectErr := worker.SelectFilesWithProviderService(ctx, s.providers, profile, worker.FileSelectionTask{
		TaskID: packet.TaskID, Objective: packet.Objective, StepTitle: packet.Step.Title,
		StepInstructions: packet.Step.Instructions, AcceptanceCriteria: criteria,
		Constraints: packet.Requirement.Constraints, Candidates: candidates,
		MaxOutputTokens: minPositive(profile.MaxOutputTokens, 4096),
	}, worker.Options{BeforeProviderRequest: func() error {
		updated, dispatchErr := s.tasks.DispatchEffect(ctx, effect.OperationID)
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
		Metadata: map[string]any{"task_id": packet.TaskID, "step_id": packet.Step.ID, "attempt_id": attempt.ID,
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

func (s *Service) Propose(ctx context.Context, input ProposalInput) (ProposalOutput, error) {
	if s == nil || s.tasks == nil || s.products == nil || s.providers == nil {
		return ProposalOutput{}, fmt.Errorf("task coordinator is unavailable")
	}
	if strings.TrimSpace(input.AttemptID) == "" || strings.TrimSpace(input.ProviderID) == "" {
		return ProposalOutput{}, fmt.Errorf("attempt and provider are required")
	}
	if len(input.Files) == 0 || len(input.Files) > 32 {
		return ProposalOutput{}, fmt.Errorf("select 1-32 files for the bounded proposal")
	}
	attempt, err := s.tasks.Attempt(ctx, input.AttemptID)
	if err != nil {
		return ProposalOutput{}, err
	}
	if input.Packet.CanonicalPacketHash == "" && attempt.Packet != nil {
		input.Packet = *attempt.Packet
	}
	if err := taskengine.ValidateStepPacket(input.Packet); err != nil {
		return ProposalOutput{}, err
	}
	if attempt.TaskID != input.Packet.TaskID || attempt.StepID != input.Packet.Step.ID || attempt.InputHash != input.Packet.CanonicalPacketHash {
		return ProposalOutput{}, fmt.Errorf("attempt is not bound to the supplied step packet")
	}
	if attempt.State != taskengine.AttemptRunning {
		return ProposalOutput{}, fmt.Errorf("attempt is not running")
	}
	if input.Packet.ProjectID == "" {
		return ProposalOutput{}, fmt.Errorf("code proposal requires a task-bound project")
	}
	effects, err := s.tasks.AttemptEffects(ctx, attempt.ID)
	if err != nil {
		return ProposalOutput{}, err
	}
	for index := len(effects) - 1; index >= 0; index-- {
		effect := effects[index]
		if effect.Action != proposalEffect {
			continue
		}
		if (effect.State == taskengine.EffectObserved || effect.State == taskengine.EffectReconciled) && effect.Receipt["status"] == "proposed_unverified" {
			artifactID, _ := effect.Receipt["artifact_id"].(string)
			proposalID, _ := effect.Receipt["proposal_id"].(string)
			artifact, body, loadErr := s.products.GetArtifact(ctx, artifactID)
			if loadErr != nil {
				return ProposalOutput{Effect: effect}, fmt.Errorf("load persisted proposal artifact: %w", loadErr)
			}
			var result worker.Result
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.DisallowUnknownFields()
			if loadErr = decoder.Decode(&result); loadErr != nil || decoder.Decode(new(any)) != io.EOF || result.ProposalID != proposalID {
				return ProposalOutput{Artifact: artifact, Effect: effect}, fmt.Errorf("persisted proposal artifact is invalid")
			}
			proposal, loadErr := s.tasks.GetCodeProposal(ctx, proposalID)
			if loadErr != nil || proposal.ArtifactID != artifact.ID || proposal.AttemptID != attempt.ID {
				return ProposalOutput{Result: result, Artifact: artifact, Effect: effect}, fmt.Errorf("persisted proposal registry binding is invalid")
			}
			return ProposalOutput{Result: result, Artifact: artifact, Effect: effect, Proposal: proposal}, nil
		}
		return ProposalOutput{Effect: effect}, fmt.Errorf("proposal already has durable state %s; inspect or reconcile it rather than replaying the provider call", effect.State)
	}
	profile, err := s.providers.Get(ctx, input.ProviderID)
	if err != nil {
		return ProposalOutput{}, err
	}
	files := make(map[string]string, len(input.Files))
	seen := map[string]bool{}
	for _, requested := range input.Files {
		clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(requested)))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || seen[clean] {
			return ProposalOutput{}, fmt.Errorf("proposal file list contains an invalid or duplicate path")
		}
		document, readErr := s.products.ReadProjectFile(ctx, input.Packet.ProjectID, clean)
		if readErr != nil {
			return ProposalOutput{}, readErr
		}
		files[document.Path] = document.Content
		seen[clean] = true
	}
	criteria := make([]string, 0, len(input.Packet.Requirement.Criteria)+len(input.Packet.Step.Checks))
	for _, criterion := range input.Packet.Requirement.Criteria {
		criteria = append(criteria, criterion.ID+": "+criterion.Description)
	}
	criteria = append(criteria, input.Packet.Step.Checks...)
	workerTask := worker.Task{
		ID:                 input.Packet.TaskID + ":" + input.Packet.Step.Key + ":" + input.Packet.CanonicalPacketHash,
		Kind:               "durable_step_proposal",
		Brief:              input.Packet.Objective + "\n\nCurrent step: " + input.Packet.Step.Title + "\n" + input.Packet.Step.Instructions,
		AcceptanceCriteria: criteria,
		Constraints:        input.Packet.Requirement.Constraints,
		RecommendedChecks:  input.Packet.Step.Checks,
		Files:              files,
		MaxOutputTokens:    minPositive(profile.MaxOutputTokens, 32768),
	}
	effect, err := s.tasks.PlanEffect(ctx, input.AttemptID, proposalEffect, profile.ID+":"+profile.Model,
		"task-packet:"+input.Packet.CanonicalPacketHash)
	if err != nil {
		return ProposalOutput{}, err
	}
	dispatched := false
	result, runErr := worker.RunWithProviderService(ctx, s.providers, profile, workerTask, worker.Options{
		BeforeProviderRequest: func() error {
			updated, dispatchErr := s.tasks.DispatchEffect(ctx, effect.OperationID)
			if dispatchErr == nil {
				effect, dispatched = updated, true
			}
			return dispatchErr
		},
	})
	if runErr != nil {
		if dispatched {
			if observed, observeErr := s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "failed", "error": runErr.Error()}); observeErr == nil {
				effect = observed
			}
		} else if abandoned, abandonErr := s.tasks.AbandonEffect(ctx, effect.OperationID, "proposal rejected before provider dispatch"); abandonErr == nil {
			effect = abandoned
		}
		return ProposalOutput{Effect: effect}, runErr
	}
	if err = worker.ValidateResult(workerTask, result); err != nil {
		if observed, observeErr := s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "invalid_proposal"}); observeErr == nil {
			effect = observed
		}
		return ProposalOutput{Effect: effect}, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return ProposalOutput{Effect: effect}, err
	}
	artifact, err := s.products.CreateArtifact(ctx, product.ArtifactInput{
		ProjectID: input.Packet.ProjectID,
		Name:      input.Packet.Step.Key + "-" + result.ProposalID[:12] + ".proposal.json",
		Kind:      "code_proposal",
		MIMEType:  "application/vnd.hermetrix.code-proposal+json",
		Content:   string(encoded),
		Metadata: map[string]any{
			"task_id": input.Packet.TaskID, "step_id": input.Packet.Step.ID, "attempt_id": input.AttemptID,
			"packet_hash": input.Packet.CanonicalPacketHash, "provider_id": profile.ID,
			"provider_revision": profile.Revision, "status": result.Status, "operation_id": effect.OperationID,
		},
	})
	if err != nil {
		if observed, observeErr := s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "artifact_persist_failed"}); observeErr == nil {
			effect = observed
		}
		return ProposalOutput{Effect: effect}, err
	}
	proposal, err := s.tasks.RecordCodeProposal(ctx, taskengine.RecordCodeProposalInput{
		ID: result.ProposalID, TaskID: input.Packet.TaskID, StepID: input.Packet.Step.ID, AttemptID: input.AttemptID,
		ProjectID: input.Packet.ProjectID, PacketHash: input.Packet.CanonicalPacketHash, ProviderID: profile.ID,
		ProviderRevision: profile.Revision, ArtifactID: artifact.ID, ResultHash: artifact.Checksum,
	})
	if err != nil {
		if observed, observeErr := s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "proposal_registry_failed", "artifact_id": artifact.ID}); observeErr == nil {
			effect = observed
		}
		return ProposalOutput{Result: result, Artifact: artifact, Effect: effect}, err
	}
	effect, err = s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{
		"status": "proposed_unverified", "proposal_id": result.ProposalID, "artifact_id": artifact.ID,
	})
	if err != nil {
		return ProposalOutput{Result: result, Artifact: artifact, Proposal: proposal}, err
	}
	return ProposalOutput{Result: result, Artifact: artifact, Effect: effect, Proposal: proposal}, nil
}

func (s *Service) Apply(ctx context.Context, proposalID, actor string) (ApplyOutput, error) {
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
	artifact, body, err := s.products.GetArtifact(ctx, proposal.ArtifactID)
	if err != nil {
		return ApplyOutput{}, err
	}
	if artifact.Checksum != proposal.ResultHash {
		return ApplyOutput{}, fmt.Errorf("proposal artifact hash no longer matches its review binding")
	}
	var result worker.Result
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil || decoder.Decode(new(any)) != io.EOF || result.ProposalID != proposal.ID {
		return ApplyOutput{}, fmt.Errorf("proposal artifact is invalid")
	}
	preimages := make(map[string]product.FileDocument, len(result.Changes))
	for _, change := range result.Changes {
		document, readErr := s.products.ReadProjectFile(ctx, proposal.ProjectID, change.Path)
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
		Metadata: map[string]any{"proposal_id": proposal.ID, "packet_hash": proposal.PacketHash},
	})
	if err != nil {
		return ApplyOutput{}, err
	}
	effect, err := s.tasks.PlanEffect(ctx, proposal.AttemptID, "workspace.apply", "proposal:"+proposal.ID, "review:"+proposal.ID)
	if err != nil {
		return ApplyOutput{}, err
	}
	proposal, err = s.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalApproved, taskengine.ProposalApplying)
	if err != nil {
		_, _ = s.tasks.AbandonEffect(ctx, effect.OperationID, "proposal state changed before apply")
		return ApplyOutput{Effect: effect, RollbackArtifact: rollbackArtifact}, err
	}
	effect, err = s.tasks.DispatchEffect(ctx, effect.OperationID)
	if err != nil {
		_, _ = s.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalApplying, taskengine.ProposalApplyFailed)
		return ApplyOutput{Proposal: proposal, Effect: effect, RollbackArtifact: rollbackArtifact}, err
	}
	receipts := make([]product.WriteFileResult, 0, len(result.Changes))
	for _, change := range result.Changes {
		receipt, writeErr := s.products.WriteProjectFile(ctx, proposal.ProjectID, product.WriteFileInput{
			Path: change.Path, Content: change.Content, ExpectedSHA256: change.BeforeSHA256, Actor: actor,
		})
		if writeErr != nil {
			rollbackOK := true
			for index := len(receipts) - 1; index >= 0; index-- {
				applied := receipts[index]
				original := preimages[applied.Document.Path]
				if _, rollbackErr := s.products.WriteProjectFile(ctx, proposal.ProjectID, product.WriteFileInput{
					Path: original.Path, Content: original.Content, ExpectedSHA256: applied.Document.SHA256, Actor: actor + ":rollback",
				}); rollbackErr != nil {
					rollbackOK = false
				}
			}
			status := "rollback_complete"
			if !rollbackOK {
				status = "rollback_incomplete"
			}
			effect, _ = s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": status, "error": writeErr.Error()})
			proposal, _ = s.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalApplying, taskengine.ProposalApplyFailed)
			return ApplyOutput{Proposal: proposal, Effect: effect, Receipts: receipts, RollbackArtifact: rollbackArtifact}, writeErr
		}
		receipts = append(receipts, receipt)
	}
	effect, err = s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "applied_unverified", "files": len(receipts), "rollback_artifact_id": rollbackArtifact.ID})
	if err != nil {
		return ApplyOutput{Proposal: proposal, Receipts: receipts, RollbackArtifact: rollbackArtifact}, err
	}
	proposal, err = s.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalApplying, taskengine.ProposalApplied)
	return ApplyOutput{Proposal: proposal, Effect: effect, Receipts: receipts, RollbackArtifact: rollbackArtifact}, err
}

func (s *Service) Verify(ctx context.Context, proposalID, actor string, checks []CommandCheck) (VerifyOutput, error) {
	actor = strings.TrimSpace(actor)
	proposal, err := s.tasks.GetCodeProposal(ctx, proposalID)
	if err != nil {
		return VerifyOutput{}, err
	}
	if actor == "" || proposal.State != taskengine.ProposalApplied {
		return VerifyOutput{}, fmt.Errorf("verified apply requires an actor and an applied proposal")
	}
	task, err := s.tasks.Get(ctx, proposal.TaskID)
	if err != nil {
		return VerifyOutput{}, err
	}
	var step taskengine.Step
	for _, candidate := range task.Plan.Steps {
		if candidate.ID == proposal.StepID {
			step = candidate
			break
		}
	}
	if step.ID == "" || step.State != taskengine.StepRunning {
		return VerifyOutput{}, fmt.Errorf("proposal step is not running")
	}
	want, got := append([]string(nil), step.Checks...), make([]string, 0, len(checks))
	seen := map[string]bool{}
	requirementByID := make(map[string]taskengine.Criterion, len(task.Requirement.Criteria))
	coveredRequirements := map[string]bool{}
	for _, criterion := range task.Requirement.Criteria {
		requirementByID[criterion.ID] = criterion
	}
	for _, check := range checks {
		if strings.TrimSpace(check.ID) == "" || seen[check.ID] {
			return VerifyOutput{}, fmt.Errorf("verification checks require unique ids")
		}
		checkRequirements := map[string]bool{}
		for _, requirementID := range check.RequirementIDs {
			requirementID = strings.TrimSpace(requirementID)
			if _, ok := requirementByID[requirementID]; !ok {
				return VerifyOutput{}, fmt.Errorf("verification check %s references unknown acceptance criterion %q", check.ID, requirementID)
			}
			if requirementID == "" || checkRequirements[requirementID] {
				return VerifyOutput{}, fmt.Errorf("verification check %s has duplicate or empty acceptance criterion ids", check.ID)
			}
			checkRequirements[requirementID], coveredRequirements[requirementID] = true, true
		}
		seen[check.ID], got = true, append(got, check.ID)
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(want, "\x00") != strings.Join(got, "\x00") || len(want) == 0 {
		return VerifyOutput{}, fmt.Errorf("verification commands must cover every frozen step check exactly once")
	}
	finalStep := true
	for _, candidate := range task.Plan.Steps {
		if candidate.ID != step.ID && candidate.State != taskengine.StepCompleted && candidate.State != taskengine.StepSkipped {
			finalStep = false
			break
		}
	}
	if finalStep {
		for _, criterion := range task.Requirement.Criteria {
			if !coveredRequirements[criterion.ID] {
				return VerifyOutput{}, fmt.Errorf("final-step verification does not cover acceptance criterion %s", criterion.ID)
			}
		}
	}
	output := VerifyOutput{}
	for _, check := range checks {
		effect, planErr := s.tasks.PlanEffect(ctx, proposal.AttemptID, "workspace.run", check.Executable+" "+strings.Join(check.Arguments, " "), "review:"+proposal.ID)
		if planErr != nil {
			return output, planErr
		}
		effect, err = s.tasks.DispatchEffect(ctx, effect.OperationID)
		if err != nil {
			return output, err
		}
		job, startErr := s.products.StartCommand(ctx, product.CommandInput{ProjectID: proposal.ProjectID, Actor: actor,
			OperationID: effect.OperationID, Executable: check.Executable, Arguments: check.Arguments,
			WorkingDir: check.WorkingDir, TimeoutSeconds: check.TimeoutSeconds})
		if startErr != nil {
			effect, _ = s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "launch_failed", "error": startErr.Error()})
			output.Effects = append(output.Effects, effect)
			return s.failVerification(ctx, proposal, output, startErr)
		}
		job, err = s.waitJob(ctx, job.ID)
		if err != nil {
			return output, err
		}
		status := taskengine.ValidationFail
		if job.State == "completed" {
			status = taskengine.ValidationPass
		}
		refs := []string{"job:" + job.ID}
		if artifactID, ok := job.Result["artifact_id"].(string); ok && artifactID != "" {
			refs = append(refs, "artifact:"+artifactID)
		}
		effect, _ = s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": job.State, "job_id": job.ID, "evidence_refs": refs})
		validation, validationErr := s.tasks.RecordValidation(ctx, taskengine.Validation{TaskID: task.ID, StepID: step.ID,
			CheckID: check.ID, SubjectRevision: taskengine.StepSubjectRevision(step), Status: status,
			Expected: "exit code 0", Actual: fmt.Sprintf("state=%s exit_code=%v", job.State, job.Result["exit_code"]), EvidenceRefs: refs})
		if validationErr != nil {
			return output, validationErr
		}
		output.Jobs, output.Effects, output.Validations = append(output.Jobs, job), append(output.Effects, effect), append(output.Validations, validation)
		if status != taskengine.ValidationPass {
			return s.failVerification(ctx, proposal, output, fmt.Errorf("mandatory check %s failed", check.ID))
		}
	}
	bundleBody, err := json.Marshal(verificationBundle{ProposalID: proposal.ID, Checks: checks, Jobs: output.Jobs, Evidence: output.Validations})
	if err != nil {
		return output, err
	}
	output.Evidence, err = s.products.CreateArtifact(ctx, product.ArtifactInput{ProjectID: proposal.ProjectID,
		Name: proposal.ID + ".verification.json", Kind: "code_verification_bundle",
		MIMEType: "application/vnd.hermetrix.code-verification+json", Content: string(bundleBody),
		Metadata: map[string]any{"proposal_id": proposal.ID, "packet_hash": proposal.PacketHash},
	})
	if err != nil {
		return output, err
	}
	proposal, err = s.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalApplied, taskengine.ProposalAwaitingReview)
	output.Proposal = proposal
	return output, err
}

// VerifyFrozen executes only the commands frozen into the active step plan.
// It never invokes a shell: each command is parsed into one executable plus
// argv, and shell control syntax is rejected. Requirement mappings come from
// the immutable step revision rather than being invented by a client.
func (s *Service) VerifyFrozen(ctx context.Context, proposalID, actor string) (VerifyOutput, error) {
	proposal, err := s.tasks.GetCodeProposal(ctx, proposalID)
	if err != nil {
		return VerifyOutput{}, err
	}
	task, err := s.tasks.Get(ctx, proposal.TaskID)
	if err != nil {
		return VerifyOutput{}, err
	}
	var step taskengine.Step
	for _, candidate := range task.Plan.Steps {
		if candidate.ID == proposal.StepID {
			step = candidate
			break
		}
	}
	if step.ID == "" || len(step.Checks) == 0 {
		return VerifyOutput{}, fmt.Errorf("proposal step has no frozen verification commands")
	}
	if len(step.RequirementIDs) == 0 {
		return VerifyOutput{}, fmt.Errorf("proposal step has no frozen acceptance-criterion mapping")
	}
	checks := make([]CommandCheck, 0, len(step.Checks))
	for _, command := range step.Checks {
		argv, parseErr := parseFrozenCommand(command)
		if parseErr != nil {
			return VerifyOutput{}, fmt.Errorf("frozen check %q is not directly executable: %w", command, parseErr)
		}
		checks = append(checks, CommandCheck{ID: command, RequirementIDs: append([]string(nil), step.RequirementIDs...),
			Executable: argv[0], Arguments: argv[1:], WorkingDir: ".", TimeoutSeconds: 120})
	}
	return s.Verify(ctx, proposalID, actor, checks)
}

func parseFrozenCommand(command string) ([]string, error) {
	command = strings.TrimSpace(command)
	if command == "" || strings.ContainsAny(command, "\x00\r\n") {
		return nil, fmt.Errorf("command is empty or multiline")
	}
	args := []string{}
	var current strings.Builder
	quote, escaped, token := rune(0), false, false
	flush := func() {
		if token {
			args = append(args, current.String())
			current.Reset()
			token = false
		}
	}
	for _, char := range command {
		if escaped {
			current.WriteRune(char)
			escaped, token = false, true
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped, token = true, true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				current.WriteRune(char)
			}
			token = true
			continue
		}
		switch char {
		case '\'', '"':
			quote, token = char, true
		case ' ', '\t':
			flush()
		case '|', '&', ';', '<', '>', '`':
			return nil, fmt.Errorf("shell control syntax is forbidden")
		default:
			current.WriteRune(char)
			token = true
		}
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("unterminated quote or escape")
	}
	flush()
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return nil, fmt.Errorf("executable is required")
	}
	return args, nil
}

func (s *Service) Review(ctx context.Context, proposalID, reviewerProviderID string) (ReviewOutput, error) {
	proposal, err := s.tasks.GetCodeProposal(ctx, proposalID)
	if err != nil {
		return ReviewOutput{}, err
	}
	if proposal.State != taskengine.ProposalAwaitingReview {
		return ReviewOutput{}, fmt.Errorf("proposal is not awaiting post-test review")
	}
	if strings.TrimSpace(reviewerProviderID) == "" || reviewerProviderID == proposal.ProviderID {
		return ReviewOutput{}, fmt.Errorf("post-test review requires a different provider profile from the implementer")
	}
	profile, err := s.providers.Get(ctx, reviewerProviderID)
	if err != nil {
		return ReviewOutput{}, err
	}
	implementerProfile, err := s.providers.Get(ctx, proposal.ProviderID)
	if err != nil {
		return ReviewOutput{}, err
	}
	if strings.EqualFold(strings.TrimSpace(profile.BaseURL), strings.TrimSpace(implementerProfile.BaseURL)) &&
		strings.EqualFold(strings.TrimSpace(profile.Model), strings.TrimSpace(implementerProfile.Model)) {
		return ReviewOutput{}, fmt.Errorf("post-test reviewer must use a different model or provider endpoint from the implementer")
	}
	task, err := s.tasks.Get(ctx, proposal.TaskID)
	if err != nil {
		return ReviewOutput{}, err
	}
	var step taskengine.Step
	for _, candidate := range task.Plan.Steps {
		if candidate.ID == proposal.StepID {
			step = candidate
			break
		}
	}
	if step.ID == "" || step.State != taskengine.StepRunning {
		return ReviewOutput{}, fmt.Errorf("proposal step is not running")
	}
	artifacts, err := s.products.ListArtifacts(ctx, proposal.ProjectID)
	if err != nil {
		return ReviewOutput{}, err
	}
	var bundleArtifact product.Artifact
	for _, artifact := range artifacts {
		if artifact.Kind == "code_verification_bundle" && artifact.Metadata["proposal_id"] == proposal.ID {
			bundleArtifact = artifact
			break
		}
	}
	if bundleArtifact.ID == "" {
		return ReviewOutput{}, fmt.Errorf("proposal has no immutable verification bundle")
	}
	_, bundleBody, err := s.products.GetArtifact(ctx, bundleArtifact.ID)
	if err != nil {
		return ReviewOutput{}, err
	}
	var bundle verificationBundle
	if err = json.Unmarshal(bundleBody, &bundle); err != nil || bundle.ProposalID != proposal.ID {
		return ReviewOutput{}, fmt.Errorf("verification bundle is invalid")
	}
	_, proposalBody, err := s.products.GetArtifact(ctx, proposal.ArtifactID)
	if err != nil {
		return ReviewOutput{}, err
	}
	var changeSet worker.Result
	if err = json.Unmarshal(proposalBody, &changeSet); err != nil {
		return ReviewOutput{}, fmt.Errorf("proposal artifact is invalid")
	}
	files := make(map[string]string, len(changeSet.Changes))
	for _, change := range changeSet.Changes {
		document, readErr := s.products.ReadProjectFile(ctx, proposal.ProjectID, change.Path)
		if readErr != nil || document.SHA256 != worker.Hash(change.Content) {
			return ReviewOutput{}, fmt.Errorf("review source for %s drifted after verification", change.Path)
		}
		files[change.Path] = document.Content
	}
	criteria := make([]string, 0, len(task.Requirement.Criteria))
	for _, criterion := range task.Requirement.Criteria {
		criteria = append(criteria, criterion.ID+": "+criterion.Description)
	}
	testEvidence := make([]string, 0, len(bundle.Evidence))
	for _, evidence := range bundle.Evidence {
		testEvidence = append(testEvidence, evidence.CheckID+": "+evidence.Actual+" ["+strings.Join(evidence.EvidenceRefs, ", ")+"]")
	}
	effect, err := s.tasks.PlanEffect(ctx, proposal.AttemptID, "provider.review", profile.ID+":"+profile.Model, "verification:"+bundleArtifact.ID)
	if err != nil {
		return ReviewOutput{}, err
	}
	dispatched := false
	review, reviewErr := worker.ReviewWithProviderService(ctx, s.providers, profile, worker.ReviewTask{TaskID: task.ID,
		ProposalID: proposal.ID, Objective: task.Objective, AcceptanceCriteria: criteria, Constraints: task.Requirement.Constraints,
		Files: files, TestEvidence: testEvidence, MaxOutputTokens: minPositive(profile.MaxOutputTokens, 8192)}, worker.Options{
		BeforeProviderRequest: func() error {
			updated, dispatchErr := s.tasks.DispatchEffect(ctx, effect.OperationID)
			if dispatchErr == nil {
				effect, dispatched = updated, true
			}
			return dispatchErr
		},
	})
	if reviewErr != nil {
		if dispatched {
			effect, _ = s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "review_failed", "error": reviewErr.Error()})
		} else {
			effect, _ = s.tasks.AbandonEffect(ctx, effect.OperationID, "review rejected before provider dispatch")
		}
		return ReviewOutput{Effect: effect}, reviewErr
	}
	reviewBody, _ := json.Marshal(review)
	reviewArtifact, err := s.products.CreateArtifact(ctx, product.ArtifactInput{ProjectID: proposal.ProjectID,
		Name: proposal.ID + ".post-review.json", Kind: "code_post_review", MIMEType: "application/vnd.hermetrix.code-review+json",
		Content: string(reviewBody), Metadata: map[string]any{"proposal_id": proposal.ID, "verification_artifact_id": bundleArtifact.ID,
			"provider_id": profile.ID, "provider_revision": profile.Revision, "verdict": review.Verdict,
			"operation_id": effect.OperationID, "attempt_id": proposal.AttemptID},
	})
	if err != nil {
		return ReviewOutput{Review: review, Effect: effect}, err
	}
	verdict := taskengine.ProposalApproved
	if review.Verdict == "reject" {
		verdict = taskengine.ProposalRejected
	}
	if _, err = s.tasks.RecordPostCodeReview(ctx, proposal.ID, "provider:"+profile.ID, verdict, review.Rationale, review.Findings); err != nil {
		return ReviewOutput{Review: review, Artifact: reviewArtifact, Effect: effect}, err
	}
	effect, err = s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "reviewed", "verdict": review.Verdict,
		"artifact_id": reviewArtifact.ID, "verification_artifact_id": bundleArtifact.ID})
	if err != nil {
		return ReviewOutput{Review: review, Artifact: reviewArtifact}, err
	}
	if review.Verdict == "reject" {
		rolledBack := s.rollbackProposal(ctx, proposal)
		proposal, _ = s.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalAwaitingReview, taskengine.ProposalReviewRejected)
		_, _ = s.tasks.FailAttempt(ctx, proposal.AttemptID, "independent post-test review rejected the change")
		task, _ = s.tasks.Get(ctx, task.ID)
		for _, candidate := range task.Plan.Steps {
			if candidate.ID == proposal.StepID {
				_, _ = s.tasks.FailStep(ctx, task.ID, candidate.Key, task.Revision, candidate.Revision, review.Rationale)
				break
			}
		}
		task, _ = s.tasks.Get(ctx, task.ID)
		return ReviewOutput{Review: review, Artifact: reviewArtifact, Proposal: proposal, Task: task, Effect: effect, RolledBack: rolledBack}, nil
	}
	for _, check := range bundle.Checks {
		var evidence taskengine.Validation
		for _, candidate := range bundle.Evidence {
			if candidate.CheckID == check.ID {
				evidence = candidate
				break
			}
		}
		for _, requirementID := range check.RequirementIDs {
			requirementID = strings.TrimSpace(requirementID)
			criterion := requirementByID(task.Requirement.Criteria, requirementID)
			if _, err = s.tasks.RecordValidation(ctx, taskengine.Validation{TaskID: task.ID, RequirementID: requirementID,
				CheckID: check.ID, SubjectRevision: taskengine.RequirementSubjectRevision(task), Status: taskengine.ValidationPass,
				Expected: criterion.Description, Actual: "deterministic check passed and independent reviewer approved",
				EvidenceRefs: append(append([]string(nil), evidence.EvidenceRefs...), "artifact:"+reviewArtifact.ID)}); err != nil {
				return ReviewOutput{Review: review, Artifact: reviewArtifact, Effect: effect}, err
			}
		}
	}
	if _, err = s.tasks.CompleteAttempt(ctx, proposal.AttemptID, "mandatory checks and independent review passed"); err != nil {
		return ReviewOutput{Review: review, Artifact: reviewArtifact, Effect: effect}, err
	}
	task, err = s.tasks.Get(ctx, task.ID)
	if err != nil {
		return ReviewOutput{}, err
	}
	for _, candidate := range task.Plan.Steps {
		if candidate.ID == proposal.StepID {
			step = candidate
			break
		}
	}
	if task, err = s.tasks.CompleteStep(ctx, task.ID, step.Key, task.Revision, step.Revision); err != nil {
		return ReviewOutput{Review: review, Artifact: reviewArtifact, Effect: effect}, err
	}
	if task.State == taskengine.StateVerifying {
		if task, err = s.tasks.CompleteTask(ctx, task.ID, task.Revision); err != nil {
			return ReviewOutput{Review: review, Artifact: reviewArtifact, Effect: effect}, err
		}
	}
	proposal, err = s.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalAwaitingReview, taskengine.ProposalVerified)
	return ReviewOutput{Review: review, Artifact: reviewArtifact, Proposal: proposal, Task: task, Effect: effect}, err
}

func requirementByID(criteria []taskengine.Criterion, id string) taskengine.Criterion {
	for _, criterion := range criteria {
		if criterion.ID == id {
			return criterion
		}
	}
	return taskengine.Criterion{}
}

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
		var proposalResult worker.Result
		if err = json.Unmarshal(body, &proposalResult); err != nil || proposalResult.ProposalID == "" || proposalResult.Status != "proposed_unverified" {
			return ReconcileOutput{Effect: effect, Artifact: &artifact}, fmt.Errorf("proposal artifact is invalid")
		}
		proposal, proposalErr := s.tasks.GetCodeProposal(ctx, proposalResult.ProposalID)
		if proposalErr != nil || proposal.ArtifactID != artifact.ID || proposal.AttemptID != effect.AttemptID {
			return ReconcileOutput{Effect: effect, Artifact: &artifact}, fmt.Errorf("proposal artifact has no matching durable proposal registry entry")
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

func (s *Service) waitJob(ctx context.Context, id string) (product.Job, error) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		job, err := s.products.GetJob(ctx, id)
		if err != nil {
			return job, err
		}
		if job.State != "queued" && job.State != "running" {
			return job, nil
		}
		select {
		case <-ctx.Done():
			return job, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) failVerification(ctx context.Context, proposal taskengine.CodeProposal, output VerifyOutput, cause error) (VerifyOutput, error) {
	rolledBack := s.rollbackProposal(ctx, proposal)
	proposal, _ = s.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalApplied, taskengine.ProposalVerifyFailed)
	_, _ = s.tasks.FailAttempt(ctx, proposal.AttemptID, cause.Error())
	if task, taskErr := s.tasks.Get(ctx, proposal.TaskID); taskErr == nil {
		for _, step := range task.Plan.Steps {
			if step.ID == proposal.StepID {
				_, _ = s.tasks.FailStep(ctx, task.ID, step.Key, task.Revision, step.Revision, cause.Error())
				break
			}
		}
	}
	output.Proposal, output.RolledBack = proposal, rolledBack
	return output, cause
}

func (s *Service) rollbackProposal(ctx context.Context, proposal taskengine.CodeProposal) bool {
	rolledBack := false
	expectedApplied := map[string]string{}
	if _, proposalBody, proposalErr := s.products.GetArtifact(ctx, proposal.ArtifactID); proposalErr == nil {
		var result worker.Result
		if json.Unmarshal(proposalBody, &result) == nil {
			for _, change := range result.Changes {
				expectedApplied[change.Path] = worker.Hash(change.Content)
			}
		}
	}
	artifacts, _ := s.products.ListArtifacts(ctx, proposal.ProjectID)
	for _, artifact := range artifacts {
		if artifact.Kind != "code_proposal_rollback" || artifact.Metadata["proposal_id"] != proposal.ID {
			continue
		}
		_, body, getErr := s.products.GetArtifact(ctx, artifact.ID)
		if getErr != nil {
			break
		}
		var originals map[string]product.FileDocument
		if json.Unmarshal(body, &originals) != nil {
			break
		}
		rolledBack = true
		for path, original := range originals {
			current, readErr := s.products.ReadProjectFile(ctx, proposal.ProjectID, path)
			if readErr != nil || expectedApplied[path] == "" || current.SHA256 != expectedApplied[path] {
				rolledBack = false
				continue
			}
			if _, writeErr := s.products.WriteProjectFile(ctx, proposal.ProjectID, product.WriteFileInput{
				Path: path, Content: original.Content, ExpectedSHA256: current.SHA256, Actor: "verification-rollback",
			}); writeErr != nil {
				rolledBack = false
			}
		}
		break
	}
	return rolledBack
}

func minPositive(value, ceiling int) int {
	if value <= 0 || value > ceiling {
		return ceiling
	}
	return value
}

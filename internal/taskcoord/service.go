// Package taskcoord connects the durable task state machine to bounded model
// workers, scoped file mutation, managed checks and evidence-bound review.
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

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/learning"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

const (
	selectionEffect = "provider.select_files"
	proposalEffect  = "provider.propose"
)

var ErrReviewerRequired = errors.New("reviewer_required")

type Service struct {
	tasks               *taskengine.Service
	products            *product.Service
	providers           *providers.Service
	learning            *learning.Service
	brain               KnowledgeRetriever
	brainCompiler       *ctxcompiler.Compiler
	brainLocalProjectID string
	brainProject        string
}

// A multi-file proposal needs enough output for a complete structured call.
// The selected immutable preset is bound to the attempt before dispatch.
func (s *Service) codeProposalPreset(ctx context.Context, profile providers.Profile) (inference.Preset, error) {
	if profile.MaxOutputTokens > 8192 {
		return s.providers.ResolveCodeProposalPreset(ctx, profile)
	}
	for _, id := range []string{"worker-max", "worker-large", "worker-medium"} {
		if preset, err := s.providers.ResolveTaskPreset(ctx, profile, id, 1); err == nil {
			return preset, nil
		}
	}
	return s.providers.ResolveTaskPreset(ctx, profile, "worker", 1)
}

type ProposalInput struct {
	AttemptID  string                  `json:"attempt_id"`
	ProviderID string                  `json:"provider_id"`
	Authority  taskengine.RunAuthority `json:"authority"`
	Packet     taskengine.StepPacket   `json:"packet"`
	Files      []string                `json:"files"`
}

type ProposalOutput struct {
	Result   worker.Result           `json:"result"`
	Artifact product.Artifact        `json:"artifact"`
	Effect   taskengine.EffectIntent `json:"effect"`
	Proposal taskengine.CodeProposal `json:"proposal"`
}

type SelectFilesInput struct {
	AttemptID  string                  `json:"attempt_id"`
	ProviderID string                  `json:"provider_id"`
	Authority  taskengine.RunAuthority `json:"authority"`
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
	Proposal         taskengine.CodeProposal   `json:"proposal"`
	Jobs             []product.Job             `json:"jobs"`
	Validations      []taskengine.Validation   `json:"validations"`
	Effects          []taskengine.EffectIntent `json:"effects"`
	Evidence         product.Artifact          `json:"evidence"`
	RollbackEvidence product.Artifact          `json:"rollback_evidence,omitempty"`
	RolledBack       bool                      `json:"rolled_back"`
}

type verificationBundle struct {
	Snapshot   product.ProjectSnapshot `json:"source_snapshot"`
	ProposalID string                  `json:"proposal_id"`
	Checks     []CommandCheck          `json:"checks"`
	Jobs       []product.Job           `json:"jobs"`
	Evidence   []taskengine.Validation `json:"evidence"`
}

type ReviewOutput struct {
	Review           worker.ReviewResult     `json:"review"`
	Artifact         product.Artifact        `json:"artifact"`
	RollbackEvidence product.Artifact        `json:"rollback_evidence,omitempty"`
	Proposal         taskengine.CodeProposal `json:"proposal"`
	Task             taskengine.Task         `json:"task"`
	Effect           taskengine.EffectIntent `json:"effect"`
	RolledBack       bool                    `json:"rolled_back"`
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

func (s *Service) WithLearning(service *learning.Service) *Service {
	s.learning = service
	return s
}

func validateTaskEgress(task taskengine.Task, profile providers.Profile) error {
	policy := task.EgressPolicy
	if policy == "" {
		policy = "local_only"
	}
	if policy == "local_only" && !providers.IsLocalProfile(profile) {
		return fmt.Errorf("task egress policy is local_only; remote provider dispatch is blocked")
	}
	return nil
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
	if err := validateTaskEgress(task, profile); err != nil {
		return AutoPlanOutput{}, err
	}
	preset, err := s.selectPlannerPreset(ctx, profile, task.Plan.ReviewStatus == "rejected")
	if err != nil {
		return AutoPlanOutput{}, err
	}
	criteria := make([]string, 0, len(task.Requirement.Criteria))
	for _, criterion := range task.Requirement.Criteria {
		criteria = append(criteria, criterion.ID+": "+criterion.Description)
	}
	planTask := worker.PlanTask{TaskID: task.ID, Objective: task.Objective, OriginalRequest: task.OriginalRequest,
		Constraints: task.Requirement.Constraints, Unknowns: task.Requirement.Unknowns, Criteria: criteria, WritablePaths: task.Requirement.Contract.WritablePaths,
		ExperiencePreview:  task.Requirement.Contract.ExperiencePreview,
		AllowedExecutables: product.AvailableCommandExecutables(),
		MaxOutputTokens:    minPositive(profile.MaxOutputTokens, 8192)}
	implementationPreset, err := s.codeProposalPreset(ctx, profile)
	if err != nil {
		return AutoPlanOutput{}, err
	}
	planTask.ImplementationOutputTokenLimit = implementationPreset.GenerationCap
	if task.Plan.ReviewStatus == "rejected" {
		feedback, feedbackErr := s.tasks.RejectedPlanFeedback(ctx, task.ID, task.ActivePlanRevision)
		if feedbackErr != nil {
			return AutoPlanOutput{}, feedbackErr
		}
		planTask.ReviewFeedback = feedback
		for _, step := range task.Plan.Steps {
			planTask.PreviousPlanSteps = append(planTask.PreviousPlanSteps, worker.PreviousPlanStep{
				Title: step.Title, Instructions: step.Instructions, Checks: step.Checks})
		}
	}
	planTask.ProjectBrainRefs = s.projectBrainRefs(ctx, task.ProjectID, task.Objective, profile)
	if task.ProjectID != "" {
		planTask.GitRepository, err = s.products.ProjectHasGitRepository(ctx, task.ProjectID)
		if err != nil {
			return AutoPlanOutput{}, fmt.Errorf("check project Git capability: %w", err)
		}
	}

	if escalation, escalationErr := s.tasks.PendingEscalation(ctx, task.ID); escalationErr != nil {
		return AutoPlanOutput{}, escalationErr
	} else if escalation != nil {
		planTask.Escalation = &worker.PlanningEscalation{Ordinal: escalation.Ordinal, StepID: escalation.StepID,
			NormalizedSignature: escalation.NormalizedSignature, FailureIDs: escalation.FailureIDs,
			EvidenceRefs: escalation.EvidenceRefs, PriorChangeRefs: escalation.PriorChangeRefs,
			DiffArtifactID: escalation.DiffArtifactID}
		packet, packetErr := s.tasks.BuildNextStepPacket(ctx, task.ID)
		if packetErr != nil {
			return AutoPlanOutput{}, packetErr
		}
		planTask.FailureEvidence, err = s.proposalFailureEvidence(ctx, packet)
		if err != nil {
			return AutoPlanOutput{}, err
		}
		if err = s.appendReviewFailureEvidence(ctx, task.ID, task.ProjectID, &planTask.FailureEvidence); err != nil {
			return AutoPlanOutput{}, err
		}
	}
	var repositoryContext map[string]any
	if task.ProjectID != "" {
		repositoryContext, err = s.plannerRepositoryContext(ctx, task, &planTask, preset.ContextMax)
		if err != nil {
			return AutoPlanOutput{}, fmt.Errorf("build planner repository context: %w", err)
		}
	}
	encodedInput, _ := json.Marshal(planTask)
	if len(encodedInput) > 60*1024 && len(planTask.ProjectBrainRefs) > 0 {
		// An optional Pi reference must not consume the planner's bounded input
		// at the expense of the task's authoritative constraints or manifest.
		planTask.ProjectBrainRefs = nil
		encodedInput, _ = json.Marshal(planTask)
	}
	run, err := s.tasks.BeginPlannerRun(ctx, task.ID, task.Revision, profile.ID, profile.Revision, worker.Hash(string(encodedInput)))
	if err != nil {
		return AutoPlanOutput{}, err
	}
	dispatched := false
	dispatchCtx := inference.WithOwner(ctx, inference.Owner{Kind: "task", ID: task.ID, Source: "planner", Priority: inference.PriorityTask,
		PresetID: preset.ID, PresetRevision: preset.Revision, PresetRole: preset.Role, RuntimeFingerprintID: profile.RuntimeFingerprintID})
	result, planErr := worker.PlanWithProviderService(dispatchCtx, s.providers, profile, planTask, worker.Options{BeforeProviderRequest: func() error {
		if bindingErr := s.validateProviderBinding(ctx, profile); bindingErr != nil {
			return bindingErr
		}
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
	if err = validatePlannedChecks(result, planTask); err != nil {
		run, _ = s.tasks.FailPlannerRun(ctx, run.ID, taskengine.PlannerDispatched, err.Error())
		return AutoPlanOutput{Result: result, Run: run}, err
	}
	planBody, _ := json.Marshal(result)
	allowedEffects := []string{}
	seenEffects := map[string]bool{}
	for _, step := range result.Steps {
		for _, effect := range step.EffectScope {
			if !seenEffects[effect] {
				seenEffects[effect] = true
				allowedEffects = append(allowedEffects, effect)
			}
		}
	}
	sort.Strings(allowedEffects)
	artifact, err := s.products.CreateArtifact(ctx, product.ArtifactInput{ProjectID: task.ProjectID,
		Name: task.ID + "-r" + fmt.Sprint(task.ActiveRequirementRevision) + ".plan.json", Kind: "task_plan_proposal",
		MIMEType: "application/vnd.hermetrix.task-plan+json", Content: string(planBody),
		Metadata: map[string]any{"task_id": task.ID, "requirement_revision": task.ActiveRequirementRevision,
			"task_revision": task.Revision, "plan_revision": task.ActivePlanRevision + 1, "planning_packet_hash": worker.Hash(string(encodedInput)),
			"project_brain_refs": knowledgeRefIdentities(planTask.ProjectBrainRefs), "repository_context": repositoryContext,
			"allowed_file_scope": []string{}, "allowed_effect_scope": allowedEffects, "provider_id": profile.ID,
			"provider_revision": profile.Revision, "runtime_fingerprint_id": profile.RuntimeFingerprintID,
			"preset_id": preset.ID, "preset_revision": preset.Revision, "generation_budget": preset.GenerationCap,
			"input_token_ceiling": preset.ContextMax},
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
		// The proposal worker receives the structured mutation intent as part
		// of the frozen step packet; it cannot be lost between planning and execution.
		instructions := "Workspace change: " + strings.TrimSpace(step.WorkspaceChange) + "\n\n" + strings.TrimSpace(step.Instructions)
		steps = append(steps, taskengine.StepSpec{Key: step.Key, Title: step.Title, Instructions: instructions,
			RequirementIDs: step.RequirementIDs, Dependencies: step.Dependencies, Checks: step.Checks, EffectScope: step.EffectScope})
	}
	task, err = s.tasks.CreatePlan(ctx, taskengine.CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Reason: result.Reason, Actor: input.Actor, Steps: steps})
	if err != nil {
		run, _ = s.tasks.RejectPlannerRun(ctx, run.ID, err.Error())
	} else {
		_ = s.tasks.ResolvePendingEscalation(ctx, task.ID, run.ID, task.ActivePlanRevision)
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
	task, err := s.tasks.Get(ctx, input.Packet.TaskID)
	if err != nil {
		return ProposalOutput{}, err
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
			proposal, loadErr := s.tasks.GetCodeProposal(ctx, proposalID)
			if loadErr != nil || proposal.ArtifactID != artifactID || proposal.AttemptID != attempt.ID {
				return ProposalOutput{Effect: effect}, fmt.Errorf("load persisted proposal registry: %w", loadErr)
			}
			loaded, loadErr := s.loadProposalBundle(ctx, proposal)
			if loadErr != nil {
				return ProposalOutput{Effect: effect}, fmt.Errorf("load proposal bundle: %w", loadErr)
			}
			return ProposalOutput{Result: loaded.Bundle.Result, Artifact: loaded.Artifact, Effect: effect, Proposal: proposal}, nil
		}
		return ProposalOutput{Effect: effect}, fmt.Errorf("proposal already has durable state %s; inspect or reconcile it rather than replaying the provider call", effect.State)
	}
	profile, err := s.providers.Get(ctx, input.ProviderID)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := validateTaskEgress(task, profile); err != nil {
		return ProposalOutput{}, err
	}
	preset, err := s.codeProposalPreset(ctx, profile)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := s.tasks.BindAttemptInference(ctx, attempt.ID, "worker", preset.ID, preset.Revision, profile.RuntimeFingerprintID); err != nil {
		return ProposalOutput{}, err
	}
	files := make(map[string]string, len(input.Files))
	projectRoot, err := s.products.ProjectRootIdentity(ctx, input.Packet.ProjectID)
	if err != nil {
		return ProposalOutput{}, err
	}
	absentFiles := []string{}
	seen := map[string]bool{}
	for _, requested := range input.Files {
		clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(requested)))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || seen[clean] {
			return ProposalOutput{}, fmt.Errorf("proposal file list contains an invalid or duplicate path")
		}
		document, readErr := s.products.SnapshotProjectFileAtRoot(ctx, input.Packet.ProjectID, projectRoot, clean)
		if readErr != nil {
			return ProposalOutput{}, readErr
		}
		if document.SHA256 == "absent" {
			allowed := false
			for _, path := range input.Packet.Requirement.Contract.WritablePaths {
				allowed = allowed || path == clean
			}
			if !allowed {
				return ProposalOutput{}, fmt.Errorf("creating %q requires an explicit frozen writable path", clean)
			}
			absentFiles = append(absentFiles, clean)
		}
		files[document.Path] = document.Content
		seen[clean] = true
	}
	criteria := stepCriteria(input.Packet)
	if currentRoot, rootErr := s.products.ProjectRootIdentity(ctx, input.Packet.ProjectID); rootErr != nil || currentRoot != projectRoot {
		return ProposalOutput{}, fmt.Errorf("project root changed during source snapshot")
	}
	workerTask := worker.Task{
		ID:                 input.Packet.TaskID + ":" + input.Packet.Step.Key + ":" + input.Packet.CanonicalPacketHash,
		Kind:               "durable_step_proposal",
		Brief:              input.Packet.Objective + "\n\nCurrent step: " + input.Packet.Step.Title + "\n" + input.Packet.Step.Instructions,
		AcceptanceCriteria: criteria,
		Constraints:        input.Packet.Requirement.Constraints,
		RecommendedChecks:  input.Packet.Step.Checks,
		Files:              files,
		WritablePaths:      input.Packet.Requirement.Contract.WritablePaths,
		AbsentFiles:        absentFiles,
		MaxOutputTokens:    minPositive(profile.MaxOutputTokens, 32768),
	}
	feedback, err := s.tasks.LatestRejectedProposalFeedback(ctx, input.Packet.TaskID, input.Packet.Step.ID)
	if err != nil {
		return ProposalOutput{}, err
	}
	if feedback != "" {
		workerTask.Brief += "\n\nPrevious proposal was rejected by the user. Review feedback for this step (apply only where consistent with the approved plan): " + feedback
	}
	failureFeedback, err := s.tasks.LatestFailedProposalFeedback(ctx, input.Packet.TaskID, input.Packet.Step.ID)
	if err != nil {
		return ProposalOutput{}, err
	}
	if failureFeedback != "" {
		workerTask.Brief += "\n\nPrevious attempt failed before any source was applied. Saved diagnostic (not an instruction or authority): " + failureFeedback + ". Correct the proposal format using the current supplied source; prefer complete replacement content if exact old/new matching failed. Preserve every approved constraint and mandatory check."
	}
	workerTask.FailureEvidence, err = s.proposalFailureEvidence(ctx, input.Packet)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err = s.appendReviewFailureEvidence(ctx, input.Packet.TaskID, input.Packet.ProjectID, &workerTask.FailureEvidence); err != nil {
		return ProposalOutput{}, err
	}
	workerTask.ProjectBrainRefs = s.projectBrainRefs(ctx, input.Packet.ProjectID,
		input.Packet.Step.Title+" "+input.Packet.Objective, profile)
	curatedTask, curateErr := curateProposalTask(workerTask, input.Packet.Step, preset)
	if curateErr != nil {
		return ProposalOutput{}, curateErr
	}
	workerTask, err = s.attachRepairDrafts(ctx, input.Packet, curatedTask, preset, projectRoot)
	if err != nil {
		return ProposalOutput{}, err
	}
	frozenTask, canonicalInput, _, freezeErr := worker.FreezeTask(workerTask)
	if freezeErr != nil {
		return ProposalOutput{}, freezeErr
	}
	workerTask = frozenTask

	effect, err := s.tasks.PlanEffect(ctx, input.Authority, input.AttemptID, proposalEffect, profile.ID+":"+profile.Model,
		"task-packet:"+input.Packet.CanonicalPacketHash)
	if err != nil {
		return ProposalOutput{}, err
	}
	dispatched := false
	dispatchCtx := inference.WithOwner(ctx, inference.Owner{Kind: "task", ID: input.Packet.TaskID, Source: "code_proposer", Priority: inference.PriorityTask,
		PresetID: preset.ID, PresetRevision: preset.Revision, PresetRole: preset.Role, RuntimeFingerprintID: profile.RuntimeFingerprintID})
	result, runErr := worker.RunWithProviderService(dispatchCtx, s.providers, profile, workerTask, worker.Options{
		BeforeProviderRequest: func() error {
			if bindingErr := s.validateProviderBinding(ctx, profile); bindingErr != nil {
				return bindingErr
			}
			updated, dispatchErr := s.tasks.DispatchEffect(ctx, input.Authority, effect.OperationID)
			if dispatchErr == nil {
				effect, dispatched = updated, true
			}
			return dispatchErr
		},
	})
	if runErr == nil && repeatsFailedDrafts(workerTask, result) {
		runErr = &worker.Error{Code: worker.ErrorInvalidProposal, Phase: worker.PhaseValidatingProposal,
			Message: "proposal reproduces only unchanged verification-failed drafts; edit the cause of the failing check; retain_draft restores unchanged companions alongside a repair"}
	}
	if runErr != nil {
		if dispatched {
			receipt := map[string]any{"status": "failed", "error": runErr.Error()}
			var workerErr *worker.Error
			if errors.As(runErr, &workerErr) {
				receipt["failure_code"], receipt["failure_phase"] = workerErr.Code, workerErr.Phase
			}
			if observed, observeErr := s.tasks.ObserveEffect(ctx, effect.OperationID, receipt); observeErr == nil {
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
	inputArtifact, err := s.products.CreateArtifact(ctx, product.ArtifactInput{
		ProjectID: input.Packet.ProjectID,
		Name:      result.ProposalID + ".input.json",
		Kind:      "code_proposal_input",
		MIMEType:  "application/json",
		Content:   string(canonicalInput),
		Metadata: map[string]any{
			"task_id":             input.Packet.TaskID,
			"step_id":             input.Packet.Step.ID,
			"attempt_id":          input.AttemptID,
			"packet_hash":         input.Packet.CanonicalPacketHash,
			"proposal_id":         result.ProposalID,
			"project_root":        projectRoot,
			"source_operation_id": effect.OperationID,
		},
	})
	if err != nil {
		observed, observeErr := s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "input_persist_failed"})
		if observeErr == nil {
			effect = observed
		}
		return ProposalOutput{Effect: effect}, err
	}

	encoded, err := json.Marshal(proposalBundle{
		Version:         2,
		InputArtifactID: inputArtifact.ID,
		Result:          result,
	})
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
			"project_root": projectRoot,
			"packet_hash":  input.Packet.CanonicalPacketHash, "provider_id": profile.ID,
			"project_brain_refs": knowledgeRefIdentities(workerTask.ProjectBrainRefs),
			"provider_revision":  profile.Revision, "status": result.Status, "operation_id": effect.OperationID,
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

func (s *Service) proposalFailureEvidence(ctx context.Context, packet taskengine.StepPacket) ([]worker.FailureEvidence, error) {
	allowedChecks := map[string]bool{}
	for _, check := range packet.Step.Checks {
		allowedChecks[check] = true
	}
	seen := map[string]bool{}
	result := []worker.FailureEvidence{}
	budget := newHistoricalSourceBudget()
	for i := len(packet.ValidationEvidence) - 1; i >= 0 && len(result) < 3; i-- {
		validation := packet.ValidationEvidence[i]
		if validation.Status != taskengine.ValidationFail || !allowedChecks[validation.CheckID] || seen[validation.CheckID] {
			continue
		}
		seen[validation.CheckID] = true
		evidence := worker.FailureEvidence{ValidationID: validation.ID, CheckID: validation.CheckID, SubjectRevision: validation.SubjectRevision, Actual: validation.Actual}
		for _, ref := range validation.EvidenceRefs {
			if !strings.HasPrefix(ref, "artifact:") {
				continue
			}
			artifact, body, err := s.products.GetArtifact(ctx, strings.TrimPrefix(ref, "artifact:"))
			if err != nil {
				return nil, err
			}
			if artifact.ProjectID != packet.ProjectID || artifact.Kind != "terminal_log" {
				continue
			}
			output := []rune(string(body))
			if len(output) > 2000 {
				output, evidence.Truncated = output[len(output)-2000:], true
			}
			evidence.ArtifactID, evidence.ArtifactSHA256, evidence.Output = artifact.ID, artifact.Checksum, string(output)
			break
		}
		if err := s.enrichHistoricalFailureWithBudget(ctx, packet, &evidence, budget); err != nil {
			return nil, err
		}
		result = append(result, evidence)
	}
	return result, nil
}

type verificationContext struct {
	snapshot product.ProjectSnapshot
	proposal taskengine.CodeProposal
	task     taskengine.Task
	step     taskengine.Step
	root     string
}

func (s *Service) prepareVerification(ctx context.Context, proposalID, actor string, checks []CommandCheck) (verificationContext, error) {
	actor = strings.TrimSpace(actor)
	proposal, err := s.tasks.GetCodeProposal(ctx, proposalID)
	if err != nil {
		return verificationContext{}, err
	}
	if actor == "" || proposal.State != taskengine.ProposalApplied {
		return verificationContext{}, fmt.Errorf("verified apply requires an actor and an applied proposal")
	}
	loaded, err := s.loadProposalBundle(ctx, proposal)
	if err != nil {
		return verificationContext{}, err
	}
	proposalArtifact := loaded.Artifact
	frozenRoot, _ := proposalArtifact.Metadata["project_root"].(string)
	task, err := s.tasks.Get(ctx, proposal.TaskID)
	if err != nil {
		return verificationContext{}, err
	}
	var step taskengine.Step
	for _, candidate := range task.Plan.Steps {
		if candidate.ID == proposal.StepID {
			step = candidate
			break
		}
	}
	if step.ID == "" || step.State != taskengine.StepRunning {
		return verificationContext{}, fmt.Errorf("proposal step is not running")
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
			return verificationContext{}, fmt.Errorf("verification checks require unique ids")
		}
		checkRequirements := map[string]bool{}
		for _, requirementID := range check.RequirementIDs {
			requirementID = strings.TrimSpace(requirementID)
			if _, ok := requirementByID[requirementID]; !ok {
				return verificationContext{}, fmt.Errorf("verification check %s references unknown acceptance criterion %q", check.ID, requirementID)
			}
			if requirementID == "" || checkRequirements[requirementID] {
				return verificationContext{}, fmt.Errorf("verification check %s has duplicate or empty acceptance criterion ids", check.ID)
			}
			checkRequirements[requirementID], coveredRequirements[requirementID] = true, true
		}
		seen[check.ID], got = true, append(got, check.ID)
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(want, "\x00") != strings.Join(got, "\x00") || len(want) == 0 {
		return verificationContext{}, fmt.Errorf("verification commands must cover every frozen step check exactly once")
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
				return verificationContext{}, fmt.Errorf("final-step verification does not cover acceptance criterion %s", criterion.ID)
			}
		}
	}
	changes := loaded.Bundle.Result
	paths := append([]string(nil), task.Requirement.Contract.WritablePaths...)
	pathSeen := map[string]bool{}
	for _, path := range paths {
		pathSeen[path] = true
	}
	for _, change := range changes.Changes {
		if !pathSeen[change.Path] {
			paths = append(paths, change.Path)
			pathSeen[change.Path] = true
		}
	}
	snapshot, err := s.products.CaptureProjectSnapshot(ctx, proposal.ProjectID, frozenRoot, paths)
	if err != nil {
		return verificationContext{}, err
	}
	for _, change := range changes.Changes {
		if snapshot.Hashes[change.Path] != worker.Hash(change.Content) {
			return verificationContext{}, fmt.Errorf("verification source for %s drifted after apply", change.Path)
		}
	}
	return verificationContext{proposal: proposal, task: task, step: step, root: snapshot.Root, snapshot: snapshot}, nil
}

func (s *Service) Verify(ctx context.Context, authority taskengine.RunAuthority, proposalID, actor string, checks []CommandCheck) (VerifyOutput, error) {
	async, err := s.tasks.HasGoalVerification(ctx, proposalID)
	if err != nil {
		return VerifyOutput{}, err
	}
	if async {
		return VerifyOutput{}, fmt.Errorf("proposal has durable goal verification; inspect its existing jobs instead of starting checks again")
	}
	prepared, err := s.prepareVerification(ctx, proposalID, actor, checks)
	if err != nil {
		return VerifyOutput{}, err
	}
	proposal, task, step, frozenRoot := prepared.proposal, prepared.task, prepared.step, prepared.root
	output := VerifyOutput{}
	for _, check := range checks {
		unlock, scopeErr := s.products.GuardProjectSnapshot(ctx, proposal.ProjectID, prepared.snapshot)
		if scopeErr != nil {
			return output, scopeErr
		}
		unlock()
		effect, planErr := s.tasks.PlanEffect(ctx, authority, proposal.AttemptID, "workspace.run", check.Executable+" "+strings.Join(check.Arguments, " "), "review:"+proposal.ID)
		if planErr != nil {
			return output, planErr
		}
		effect, err = s.tasks.DispatchEffect(ctx, authority, effect.OperationID)
		if err != nil {
			return output, err
		}
		job, startErr := s.products.StartCommand(ctx, product.CommandInput{ProjectID: proposal.ProjectID, ExpectedRoot: frozenRoot, Actor: actor,
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
		actual := fmt.Sprintf("state=%s exit_code=%v", job.State, job.Result["exit_code"])
		if text, _ := job.Result["output"].(string); emptyTestDiscovery(check.Executable, check.Arguments, text) {
			status = taskengine.ValidationFail
			actual += "; no tests discovered: this does not verify acceptance"
		}
		refs := []string{"job:" + job.ID}
		if artifactID, ok := job.Result["artifact_id"].(string); ok && artifactID != "" {
			refs = append(refs, "artifact:"+artifactID)
		}
		effect, _ = s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": job.State, "job_id": job.ID, "evidence_refs": refs})
		unlock, scopeErr = s.products.GuardProjectSnapshot(ctx, proposal.ProjectID, prepared.snapshot)
		if scopeErr != nil {
			return output, fmt.Errorf("verification source snapshot changed: %w", scopeErr)
		}
		unlock()
		validation, validationErr := s.tasks.RecordValidation(ctx, taskengine.Validation{TaskID: task.ID, StepID: step.ID,
			CheckID: check.ID, SubjectRevision: taskengine.StepSubjectRevision(step), Status: status,
			Expected: "exit code 0 and nonempty discovery for test commands", Actual: actual, EvidenceRefs: refs})
		if validationErr != nil {
			return output, validationErr
		}
		output.Jobs, output.Effects, output.Validations = append(output.Jobs, job), append(output.Effects, effect), append(output.Validations, validation)
		if status != taskengine.ValidationPass {
			return s.failVerification(ctx, proposal, output, fmt.Errorf("mandatory check %s failed", check.ID))
		}
	}
	return s.finishVerification(ctx, proposal, checks, output, prepared.snapshot)
}

func (s *Service) finishVerification(ctx context.Context, proposal taskengine.CodeProposal, checks []CommandCheck, output VerifyOutput, snapshot product.ProjectSnapshot) (VerifyOutput, error) {
	unlock, err := s.products.GuardProjectSnapshot(ctx, proposal.ProjectID, snapshot)
	if err != nil {
		return output, err
	}
	defer unlock()
	bundleBody, err := json.Marshal(verificationBundle{ProposalID: proposal.ID, Checks: checks, Jobs: output.Jobs, Evidence: output.Validations, Snapshot: snapshot})
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
	proposal, err = s.tasks.BindCodeProposalVerification(ctx, proposal.ID, output.Evidence.ID)
	output.Proposal = proposal
	return output, err
}

// VerifyFrozen executes only the commands frozen into the active step plan.
// It never invokes a shell: each command is parsed into one executable plus
// argv, and shell control syntax is rejected. Requirement mappings come from
// the immutable step revision rather than being invented by a client.
func (s *Service) VerifyFrozen(ctx context.Context, authority taskengine.RunAuthority, proposalID, actor string) (VerifyOutput, error) {
	checks, err := s.frozenChecks(ctx, proposalID, 120)
	if err != nil {
		return VerifyOutput{}, err
	}
	return s.Verify(ctx, authority, proposalID, actor, checks)
}

func (s *Service) frozenChecks(ctx context.Context, proposalID string, timeout int) ([]CommandCheck, error) {
	proposal, err := s.tasks.GetCodeProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	task, err := s.tasks.Get(ctx, proposal.TaskID)
	if err != nil {
		return nil, err
	}
	var step taskengine.Step
	for _, candidate := range task.Plan.Steps {
		if candidate.ID == proposal.StepID {
			step = candidate
			break
		}
	}
	if step.ID == "" || len(step.Checks) == 0 {
		return nil, fmt.Errorf("proposal step has no frozen verification commands")
	}
	if len(step.RequirementIDs) == 0 {
		return nil, fmt.Errorf("proposal step has no frozen acceptance-criterion mapping")
	}
	checks := make([]CommandCheck, 0, len(step.Checks))
	for _, command := range step.Checks {
		argv, parseErr := parseFrozenCommand(command)
		if parseErr != nil {
			return nil, fmt.Errorf("frozen check %q is not directly executable: %w", command, parseErr)
		}
		if assertionCommand(argv) {
			argv, parseErr = compileFileAssertion(argv)
			if parseErr != nil {
				return nil, parseErr
			}
		}
		checks = append(checks, CommandCheck{ID: command, RequirementIDs: append([]string(nil), step.RequirementIDs...),
			Executable: argv[0], Arguments: argv[1:], WorkingDir: ".", TimeoutSeconds: timeout})
	}
	return checks, nil
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

func (s *Service) Review(ctx context.Context, authority taskengine.RunAuthority, proposalID, reviewerProviderID string) (ReviewOutput, error) {
	proposal, err := s.tasks.GetCodeProposal(ctx, proposalID)
	if err != nil {
		return ReviewOutput{}, err
	}
	if proposal.State != taskengine.ProposalAwaitingReview {
		return ReviewOutput{}, fmt.Errorf("proposal is not awaiting post-test review")
	}
	if strings.TrimSpace(reviewerProviderID) == "" || reviewerProviderID == proposal.ProviderID {
		_ = s.tasks.MarkReviewerRequired(ctx, proposal.ID, "reviewer_required:different_provider_profile")
		return ReviewOutput{}, fmt.Errorf("%w: post-test review requires a different provider profile from the implementer", ErrReviewerRequired)
	}
	profile, err := s.providers.Get(ctx, reviewerProviderID)
	if err != nil {
		return ReviewOutput{}, err
	}
	// A distinct provider profile starts a fresh review call with only the saved
	// change and test evidence. This permits one local model to fill both roles
	// when its profiles have different context budgets. The review is a second
	// pass, not model-diverse evidence; the user's approval remains required.
	task, err := s.tasks.Get(ctx, proposal.TaskID)
	if err != nil {
		return ReviewOutput{}, err
	}
	if err := validateTaskEgress(task, profile); err != nil {
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
	if proposal.VerificationArtifactID == "" {
		return ReviewOutput{}, fmt.Errorf("proposal has no immutable verification bundle")
	}
	bundleArtifact, bundleBody, err := s.products.GetArtifact(ctx, proposal.VerificationArtifactID)
	if err != nil {
		return ReviewOutput{}, err
	}
	if bundleArtifact.Kind != "code_verification_bundle" || bundleArtifact.ProjectID != proposal.ProjectID ||
		bundleArtifact.Metadata["proposal_id"] != proposal.ID {
		return ReviewOutput{}, fmt.Errorf("proposal verification artifact binding is invalid")
	}
	var bundle verificationBundle
	if err = json.Unmarshal(bundleBody, &bundle); err != nil || bundle.ProposalID != proposal.ID {
		return ReviewOutput{}, fmt.Errorf("verification bundle is invalid")
	}
	loaded, err := s.loadProposalBundle(ctx, proposal)
	if err != nil {
		return ReviewOutput{}, err
	}
	changeSet := loaded.Bundle.Result

	files := make(map[string]string, len(changeSet.Changes))
	releaseSource, err := s.products.GuardProjectSnapshot(ctx, proposal.ProjectID, bundle.Snapshot)
	if err != nil {
		return ReviewOutput{}, fmt.Errorf("review source snapshot changed: %w", err)
	}
	defer func() {
		if releaseSource != nil {
			releaseSource()
		}
	}()
	for _, change := range changeSet.Changes {
		document, readErr := s.products.ReadProjectFile(ctx, proposal.ProjectID, change.Path)
		if readErr != nil || document.SHA256 != worker.Hash(change.Content) {
			return ReviewOutput{}, fmt.Errorf("review source for %s drifted after verification", change.Path)
		}
		files[change.Path] = document.Content
	}
	// A process may stop after the reviewer decision and before the step is
	// finalized. The decision is durable; dispatching the reviewer again would
	// create a second, potentially contradictory decision for the same evidence.
	if saved, savedErr := s.tasks.LatestPostCodeReview(ctx, proposal.ID); savedErr == nil {
		if saved.Reviewer != "provider:"+profile.ID || saved.Verdict != taskengine.ProposalApproved {
			return ReviewOutput{}, fmt.Errorf("saved post-review decision cannot be finalized with this reviewer")
		}
		return s.finalizeApprovedReview(ctx, authority, proposal, task, step, bundle,
			worker.ReviewResult{Verdict: "approve", Rationale: saved.Rationale, Findings: saved.Findings}, product.Artifact{})
	} else if !errors.Is(savedErr, sql.ErrNoRows) {
		return ReviewOutput{}, savedErr
	}
	releaseSource()
	releaseSource = nil
	criteria := stepCriteria(taskengine.StepPacket{Step: step, Requirement: task.Requirement})
	testEvidence := make([]string, 0, len(bundle.Evidence))
	for _, evidence := range bundle.Evidence {
		testEvidence = append(testEvidence, evidence.CheckID+": "+evidence.Actual+" ["+strings.Join(evidence.EvidenceRefs, ", ")+"]")
		for _, ref := range evidence.EvidenceRefs {
			if !strings.HasPrefix(ref, "artifact:") {
				continue
			}
			artifact, body, err := s.products.GetArtifact(ctx, strings.TrimPrefix(ref, "artifact:"))
			if err != nil {
				return ReviewOutput{}, err
			}
			if artifact.ProjectID != proposal.ProjectID || artifact.Kind != "terminal_log" {
				continue
			}
			output := []rune(string(body))
			if len(output) > 2000 {
				output = append([]rune("[output tail truncated]\n"), output[len(output)-2000:]...)
			}
			testEvidence = append(testEvidence, fmt.Sprintf("Saved actual output for %s (artifact %s, sha256 %s):\n%s", evidence.CheckID, artifact.ID, artifact.Checksum, string(output)))
			break
		}
	}
	preset, err := s.providers.ResolveCodeReviewPreset(ctx, profile)
	if err != nil {
		return ReviewOutput{}, err
	}
	files, partialFiles, err := proposalReviewFiles(s, profile, loaded.InputTask, changeSet, files)
	if err != nil {
		return ReviewOutput{}, err
	}
	effect, err := s.tasks.PlanEffect(ctx, authority, proposal.AttemptID, "provider.review", profile.ID+":"+profile.Model, "verification:"+bundleArtifact.ID)
	if err != nil {
		return ReviewOutput{}, err
	}
	dispatched := false
	dispatchCtx := inference.WithOwner(ctx, inference.Owner{Kind: "task", ID: task.ID, Source: "code_reviewer", Priority: inference.PriorityTask,
		PresetID: preset.ID, PresetRevision: preset.Revision, PresetRole: preset.Role, RuntimeFingerprintID: profile.RuntimeFingerprintID})
	review, reviewErr := worker.ReviewWithProviderService(dispatchCtx, s.providers, profile, worker.ReviewTask{TaskID: task.ID,
		ProposalID: proposal.ID, Objective: task.Objective, StepTitle: step.Title, StepInstructions: step.Instructions,
		StepChecks: step.Checks, AcceptanceCriteria: criteria, Constraints: task.Requirement.Constraints,
		Files: files, PartialFiles: partialFiles, TestEvidence: testEvidence, MaxOutputTokens: minPositive(profile.MaxOutputTokens, 8192)}, worker.Options{
		BeforeProviderRequest: func() error {
			if bindingErr := s.validateProviderBinding(ctx, profile); bindingErr != nil {
				return bindingErr
			}
			updated, dispatchErr := s.tasks.DispatchEffect(ctx, authority, effect.OperationID)
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
	releaseSource, err = s.products.GuardProjectSnapshot(ctx, proposal.ProjectID, bundle.Snapshot)
	if err != nil {
		effect, observeErr := s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "review_snapshot_stale", "accepted": false, "artifact_id": reviewArtifact.ID, "verification_artifact_id": bundleArtifact.ID})
		if observeErr != nil {
			return ReviewOutput{Review: review, Artifact: reviewArtifact, Effect: effect}, observeErr
		}
		return ReviewOutput{Review: review, Artifact: reviewArtifact, Effect: effect}, fmt.Errorf("review source snapshot changed before acceptance: %w", err)
	}
	if review.Verdict == "reject" {
		releaseSource()
		releaseSource = nil
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
		rollback := s.rollbackProposal(ctx, proposal)
		proposal, _ = s.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalAwaitingReview, taskengine.ProposalReviewRejected)
		_, _ = s.tasks.FailAttempt(ctx, proposal.AttemptID, "independent post-test review rejected the change")
		evidenceRefs := []string{"artifact:" + reviewArtifact.ID}
		if rollback.Artifact.ID != "" {
			evidenceRefs = append(evidenceRefs, "rollback_artifact:"+rollback.Artifact.ID)
		}
		_, _ = s.tasks.RecordConfirmedFailure(ctx, taskengine.ConfirmedFailureInput{AttemptID: proposal.AttemptID,
			FailureKind: "execution", Message: "independent post-test review rejected the change",
			DiffArtifactID: proposal.ArtifactID, EvidenceRefs: evidenceRefs,
			PriorChangeRefs: []string{"artifact:" + proposal.ArtifactID}})
		task, _ = s.tasks.Get(ctx, task.ID)
		for _, candidate := range task.Plan.Steps {
			if candidate.ID == proposal.StepID {
				_, _ = s.tasks.FailStep(ctx, task.ID, candidate.Key, task.Revision, candidate.Revision, review.Rationale)
				break
			}
		}
		task, _ = s.tasks.Get(ctx, task.ID)
		return ReviewOutput{Review: review, Artifact: reviewArtifact, RollbackEvidence: rollback.Artifact,
			Proposal: proposal, Task: task, Effect: effect, RolledBack: rollback.Restored}, nil
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
				Expected: criterion.Description, Actual: "deterministic check passed and separate reviewer pass approved",
				EvidenceRefs: append(append([]string(nil), evidence.EvidenceRefs...), "artifact:"+reviewArtifact.ID)}); err != nil {
				return ReviewOutput{Review: review, Artifact: reviewArtifact, Effect: effect}, err
			}
		}
	}
	return s.finalizeApprovedReview(ctx, authority, proposal, task, step, bundle, review, reviewArtifact)
}

func (s *Service) finalizeApprovedReview(ctx context.Context, authority taskengine.RunAuthority,
	proposal taskengine.CodeProposal, task taskengine.Task, step taskengine.Step, bundle verificationBundle,
	review worker.ReviewResult, reviewArtifact product.Artifact) (ReviewOutput, error) {
	// Pausing and resuming changes the step's lifecycle revision even though the
	// applied proposal and frozen checks have not changed. Preserve the original
	// evidence and add an explicit current-revision binding before completion.
	for _, check := range bundle.Checks {
		var evidence *taskengine.Validation
		for i := range bundle.Evidence {
			if bundle.Evidence[i].CheckID == check.ID && bundle.Evidence[i].Status == taskengine.ValidationPass {
				evidence = &bundle.Evidence[i]
				break
			}
		}
		if evidence == nil {
			return ReviewOutput{Review: review, Artifact: reviewArtifact}, fmt.Errorf("verified check %s has no passing evidence", check.ID)
		}
		if evidence.SubjectRevision != taskengine.StepSubjectRevision(step) {
			refs := append([]string(nil), evidence.EvidenceRefs...)
			refs = append(refs, "artifact:"+proposal.VerificationArtifactID)
			if _, err := s.tasks.RecordValidation(ctx, taskengine.Validation{TaskID: task.ID, StepID: step.ID,
				CheckID: check.ID, SubjectRevision: taskengine.StepSubjectRevision(step), Status: taskengine.ValidationPass,
				Expected: evidence.Expected, Actual: "carried from immutable frozen check after lifecycle-only review recovery; proposal source hashes match",
				EvidenceRefs: refs}); err != nil {
				return ReviewOutput{Review: review, Artifact: reviewArtifact}, err
			}
		}
	}
	attempt, err := s.tasks.Attempt(ctx, proposal.AttemptID)
	if err != nil {
		return ReviewOutput{Review: review, Artifact: reviewArtifact}, err
	}
	if attempt.State == taskengine.AttemptRunning {
		if _, err = s.tasks.CompleteAttempt(ctx, authority, proposal.AttemptID, "mandatory checks and separate review passed"); err != nil {
			return ReviewOutput{Review: review, Artifact: reviewArtifact}, err
		}
	} else if attempt.State != taskengine.AttemptCompleted {
		return ReviewOutput{Review: review, Artifact: reviewArtifact}, fmt.Errorf("reviewed attempt is not ready to complete")
	}
	task, err = s.tasks.Get(ctx, task.ID)
	if err != nil {
		return ReviewOutput{Review: review, Artifact: reviewArtifact}, err
	}
	for _, candidate := range task.Plan.Steps {
		if candidate.ID == proposal.StepID {
			step = candidate
			break
		}
	}
	if task, err = s.tasks.CompleteStep(ctx, task.ID, step.Key, task.Revision, step.Revision); err != nil {
		return ReviewOutput{Review: review, Artifact: reviewArtifact}, err
	}
	proposal, err = s.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalAwaitingReview, taskengine.ProposalVerified)
	if err != nil {
		return ReviewOutput{Review: review, Artifact: reviewArtifact, Task: task}, err
	}
	_ = s.tasks.ClearReviewerRequired(ctx, proposal.ID)
	if task.State == taskengine.StateVerifying {
		report, reportErr := s.tasks.EvaluateCompletion(ctx, task.ID, task.Revision)
		if reportErr != nil {
			return ReviewOutput{Review: review, Artifact: reviewArtifact, Proposal: proposal, Task: task}, reportErr
		}
		// Technical verification succeeded. Waiting for the human's exact-evidence
		// acceptance is an ordinary next action, not a failed reviewer operation.
		if report.AwaitingUserAcceptance {
			return ReviewOutput{Review: review, Artifact: reviewArtifact, Proposal: proposal, Task: task}, nil
		}
		if task, err = s.tasks.CompleteTask(ctx, task.ID, task.Revision); err != nil {
			return ReviewOutput{Review: review, Artifact: reviewArtifact, Proposal: proposal, Task: task}, err
		}
		if reviewArtifact.ID != "" {
			s.enqueueVerifiedTaskLearning(context.WithoutCancel(ctx), task, proposal, reviewArtifact, bundle)
		}
	}
	return ReviewOutput{Review: review, Artifact: reviewArtifact, Proposal: proposal, Task: task}, nil
}

func (s *Service) enqueueVerifiedTaskLearning(ctx context.Context, task taskengine.Task, proposal taskengine.CodeProposal,
	reviewArtifact product.Artifact, bundle verificationBundle) {
	if s.learning == nil {
		return
	}
	decisions, receipts := []string{}, []string{}
	for _, step := range task.Plan.Steps {
		decisions = append(decisions, step.Title+": "+step.Instructions)
	}
	for _, evidence := range bundle.Evidence {
		receipts = append(receipts, evidence.EvidenceRefs...)
	}
	verified := append([]string(nil), receipts...)
	verified = append(verified, "artifact:"+reviewArtifact.ID)
	artifacts := []string{"artifact:" + reviewArtifact.ID}
	if proposal.ArtifactID != "" {
		artifacts = append(artifacts, "artifact:"+proposal.ArtifactID)
	}
	if proposal.VerificationArtifactID != "" {
		artifacts = append(artifacts, "artifact:"+proposal.VerificationArtifactID)
	}
	_, _, _ = s.learning.Enqueue(ctx, learning.EnqueueInput{SessionID: "task:" + task.ID,
		MilestoneID: "task-complete:" + fmt.Sprint(task.Revision), TriggerKind: "successful_milestone",
		Digest: learning.Digest{GoalAndConstraints: task.Objective + "\n" + strings.Join(task.Requirement.Constraints, "\n"),
			Outcome: "completed", Decisions: decisions, ToolReceipts: receipts, VerifiedBy: verified, Artifacts: artifacts}})
}

func (s *Service) validateProviderBinding(ctx context.Context, expected providers.Profile) error {
	current, err := s.providers.Get(ctx, expected.ID)
	if err != nil {
		return fmt.Errorf("reload task provider before dispatch: %w", err)
	}
	if current.ID != expected.ID || current.Revision != expected.Revision || providers.Revision(current) != expected.Revision {
		return fmt.Errorf("task provider revision changed before dispatch; create a new task operation with the current provider contract")
	}
	if !current.Enabled {
		return fmt.Errorf("task provider was disabled before dispatch")
	}
	if !current.CredentialReady {
		return fmt.Errorf("task provider credential became unavailable before dispatch")
	}
	return nil
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
	rollback := s.rollbackProposal(ctx, proposal)
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
	evidenceRefs := []string{}
	for _, validation := range output.Validations {
		evidenceRefs = append(evidenceRefs, validation.EvidenceRefs...)
	}
	if output.Evidence.ID != "" {
		evidenceRefs = append(evidenceRefs, "artifact:"+output.Evidence.ID)
	}
	if rollback.Artifact.ID != "" {
		evidenceRefs = append(evidenceRefs, "rollback_artifact:"+rollback.Artifact.ID)
	}
	_, _ = s.tasks.RecordConfirmedFailure(ctx, taskengine.ConfirmedFailureInput{AttemptID: proposal.AttemptID,
		FailureKind: "test", Message: cause.Error(), DiffArtifactID: proposal.ArtifactID,
		EvidenceRefs: evidenceRefs, PriorChangeRefs: []string{"artifact:" + proposal.ArtifactID}})
	output.Proposal, output.RolledBack, output.RollbackEvidence = proposal, rollback.Restored, rollback.Artifact
	return output, cause
}

type rollbackFileReceipt struct {
	Path                   string `json:"path"`
	ExpectedAppliedSHA256  string `json:"expected_applied_sha256"`
	OriginalSHA256         string `json:"original_sha256"`
	BeforeSHA256           string `json:"before_sha256,omitempty"`
	AfterSHA256            string `json:"after_sha256,omitempty"`
	Status                 string `json:"status"`
	Error                  string `json:"error,omitempty"`
	RestorationArtifactID  string `json:"restoration_artifact_id,omitempty"`
	RestorationOperationID string `json:"restoration_operation_id,omitempty"`
}

type rollbackReceipt struct {
	ProposalID         string                `json:"proposal_id"`
	PreimageArtifactID string                `json:"preimage_artifact_id"`
	Restored           bool                  `json:"restored"`
	Files              []rollbackFileReceipt `json:"files"`
	Error              string                `json:"error,omitempty"`
}

type rollbackOutcome struct {
	Restored bool
	Artifact product.Artifact
}

func (s *Service) rollbackProposal(ctx context.Context, proposal taskengine.CodeProposal) rollbackOutcome {
	receipt := rollbackReceipt{ProposalID: proposal.ID, PreimageArtifactID: proposal.RollbackArtifactID,
		Files: []rollbackFileReceipt{}}
	finish := func() rollbackOutcome {
		body, err := json.Marshal(receipt)
		if err != nil {
			return rollbackOutcome{Restored: receipt.Restored}
		}
		artifact, err := s.products.CreateArtifact(context.WithoutCancel(ctx), product.ArtifactInput{
			ProjectID: proposal.ProjectID, Name: proposal.ID + ".rollback-receipt.json",
			Kind: "code_rollback_receipt", MIMEType: "application/vnd.hermetrix.rollback-receipt+json",
			Content: string(body), Metadata: map[string]any{"proposal_id": proposal.ID, "restored": receipt.Restored},
		})
		if err != nil {
			return rollbackOutcome{Restored: receipt.Restored}
		}
		return rollbackOutcome{Restored: receipt.Restored, Artifact: artifact}
	}
	loaded, loadErr := s.loadProposalBundle(ctx, proposal)
	if loadErr != nil {
		receipt.Error = fmt.Sprintf("failed to load proposal bundle: %v", loadErr)
		return finish()
	}
	expectedApplied := map[string]string{}
	for _, change := range loaded.Bundle.Result.Changes {
		expectedApplied[change.Path] = worker.Hash(change.Content)
	}
	if proposal.RollbackArtifactID == "" {
		receipt.Error = "rollback preimage artifact is missing"
		return finish()
	}
	artifact, body, err := s.products.GetArtifact(ctx, proposal.RollbackArtifactID)
	if err != nil || artifact.Kind != "code_proposal_rollback" || artifact.ProjectID != proposal.ProjectID ||
		artifact.Metadata["proposal_id"] != proposal.ID {
		receipt.Error = "rollback preimage artifact is unavailable or does not match this proposal"
		return finish()
	}
	var originals map[string]product.FileDocument
	if json.Unmarshal(body, &originals) != nil || len(originals) == 0 {
		receipt.Error = "rollback preimage artifact has no valid files"
		return finish()
	}
	frozenRoot, _ := artifact.Metadata["project_root"].(string)
	if frozenRoot != "" {
		if currentRoot, rootErr := s.products.ProjectRootIdentity(ctx, proposal.ProjectID); rootErr != nil || currentRoot != frozenRoot {
			receipt.Error = "project root changed; inspect original scope before rollback"
			return finish()
		}
	}
	paths := make([]string, 0, len(originals))
	for path := range originals {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	receipt.Restored = true
	for _, path := range paths {
		original := originals[path]
		file := rollbackFileReceipt{Path: path, ExpectedAppliedSHA256: expectedApplied[path], OriginalSHA256: original.SHA256}
		current, readErr := s.products.SnapshotProjectFileAtRoot(ctx, proposal.ProjectID, frozenRoot, path)
		if readErr != nil {
			file.Status, file.Error = "not_restored", readErr.Error()
		} else {
			file.BeforeSHA256 = current.SHA256
			switch {
			case expectedApplied[path] == "":
				file.Status, file.Error = "not_restored", "applied file hash is unavailable"
			case current.SHA256 == original.SHA256:
				file.Status, file.AfterSHA256 = "already_original", current.SHA256
			case current.SHA256 != expectedApplied[path]:
				file.Status, file.Error = "changed_since_apply", "file no longer matches the applied proposal"
			case original.SHA256 == "absent":
				removed, removeErr := s.products.RestoreProjectFileAbsenceAtRoot(ctx, proposal.ProjectID, frozenRoot, path, current.SHA256, "verification-rollback")
				file.RestorationArtifactID, file.RestorationOperationID = removed.ArtifactID, removed.OperationID
				if removeErr != nil {
					file.Status, file.Error = "not_restored", removeErr.Error()
				} else if removed.State != "restored" {
					file.Status, file.Error = "not_restored", "absence rollback lacks a verified receipt"
				} else {
					file.Status, file.AfterSHA256 = "restored", "absent"
				}
			default:
				_, writeErr := s.products.WriteProjectFilesAtRoot(ctx, proposal.ProjectID, frozenRoot, []product.WriteFileInput{{
					Path: path, Content: original.Content, ExpectedSHA256: current.SHA256, Actor: "verification-rollback",
				}})
				if writeErr != nil {
					file.Status, file.Error = "not_restored", writeErr.Error()
				} else if after, afterErr := s.products.ReadProjectFile(ctx, proposal.ProjectID, path); afterErr != nil {
					file.Status, file.Error = "not_verified", afterErr.Error()
				} else {
					file.AfterSHA256 = after.SHA256
					if after.SHA256 == original.SHA256 {
						file.Status = "restored"
					} else {
						file.Status, file.Error = "not_verified", "file hash does not match the original"
					}
				}
			}
		}
		if file.Status != "restored" && file.Status != "already_original" {
			receipt.Restored = false
		}
		receipt.Files = append(receipt.Files, file)
	}
	return finish()
}

func minPositive(value, ceiling int) int {
	if value <= 0 || value > ceiling {
		return ceiling
	}
	return value
}

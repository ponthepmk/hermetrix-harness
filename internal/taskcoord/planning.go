package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"
	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
	"sort"
	"strings"
)

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
	selectedSkills, err := s.taskSkillRefs(ctx, task.ProjectID, task.Objective, profile)
	if err != nil {
		return AutoPlanOutput{}, err
	}
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
	for _, selectedSkill := range selectedSkills {
		trial := planTask
		trial.SkillRefs = append([]worker.SkillRef{}, planTask.SkillRefs...)
		trial.SkillRefs = append(trial.SkillRefs, selectedSkill)
		trialBody, trialErr := json.Marshal(trial)
		if trialErr != nil || len(trialBody) > repositoryBodyBudget(preset.ContextMax, 60*1024) {
			continue
		}
		planTask.SkillRefs = append(planTask.SkillRefs, selectedSkill)
	}
	encodedInput, _ := json.Marshal(planTask)
	if len(encodedInput) > 60*1024 && len(planTask.ProjectBrainRefs) > 0 {
		// An optional Pi reference must not consume the planner's bounded input
		// at the expense of the task's authoritative constraints or manifest.
		planTask.ProjectBrainRefs = nil
		planTask.SkillRefs = nil
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
	metadata := map[string]any{"task_id": task.ID, "requirement_revision": task.ActiveRequirementRevision,
		"task_revision": task.Revision, "plan_revision": task.ActivePlanRevision + 1, "planning_packet_hash": worker.Hash(string(encodedInput)),
		"project_brain_refs": knowledgeRefIdentities(planTask.ProjectBrainRefs), "repository_context": repositoryContext,
		"allowed_file_scope": []string{}, "allowed_effect_scope": allowedEffects, "provider_id": profile.ID,
		"provider_revision": profile.Revision, "runtime_fingerprint_id": profile.RuntimeFingerprintID,
		"preset_id": preset.ID, "preset_revision": preset.Revision, "generation_budget": preset.GenerationCap,
		"input_token_ceiling": preset.ContextMax}
	metadata["skill_refs"] = skillRefIdentities(planTask.SkillRefs)
	metadata["skill_context"] = map[string]any{"selected": len(selectedSkills), "included": len(planTask.SkillRefs)}
	artifact, err := s.products.CreateArtifact(ctx, product.ArtifactInput{ProjectID: task.ProjectID,
		Name: task.ID + "-r" + fmt.Sprint(task.ActiveRequirementRevision) + ".plan.json", Kind: "task_plan_proposal",
		MIMEType: "application/vnd.hermetrix.task-plan+json", Content: string(planBody),
		Metadata: metadata,
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

package taskcoord

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

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
	if err = validateStoredVerificationScope(loaded, bundleBody, bundle.Snapshot); err != nil {
		return ReviewOutput{}, err
	}

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
	}
	return ReviewOutput{Review: review, Artifact: reviewArtifact, Proposal: proposal, Task: task}, nil
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

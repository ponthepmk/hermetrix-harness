package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
	"sort"
	"strings"
	"time"
)

type verificationContext struct {
	snapshot     product.ProjectSnapshot
	proposal     taskengine.CodeProposal
	task         taskengine.Task
	step         taskengine.Step
	root         string
	scopeVersion int
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
	if loaded.InputTask != nil {
		for path := range loaded.InputTask.Files {
			if !pathSeen[path] {
				paths = append(paths, path)
				pathSeen[path] = true
			}
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
	version := 0
	if loaded.InputTask != nil {
		version = verificationScopeVersion
	}
	if err = validateVerificationScope(loaded, snapshot, version); err != nil {
		return verificationContext{}, err
	}
	return verificationContext{proposal: proposal, task: task, step: step, root: snapshot.Root, snapshot: snapshot, scopeVersion: version}, nil
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
	return s.finishVerification(ctx, proposal, checks, output, prepared.snapshot, prepared.scopeVersion)
}

func (s *Service) finishVerification(ctx context.Context, proposal taskengine.CodeProposal, checks []CommandCheck, output VerifyOutput, snapshot product.ProjectSnapshot, scopeVersion int) (VerifyOutput, error) {
	loaded, err := s.loadProposalBundle(ctx, proposal)
	if err != nil {
		return output, err
	}
	if err = validateVerificationScope(loaded, snapshot, scopeVersion); err != nil {
		return output, err
	}
	unlock, err := s.products.GuardProjectSnapshot(ctx, proposal.ProjectID, snapshot)
	if err != nil {
		return output, err
	}
	defer unlock()
	bundleBody, err := json.Marshal(scopedVerificationBundle{verificationBundle: verificationBundle{ProposalID: proposal.ID, Checks: checks, Jobs: output.Jobs, Evidence: output.Validations, Snapshot: snapshot}, ScopeVersion: scopeVersion})
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

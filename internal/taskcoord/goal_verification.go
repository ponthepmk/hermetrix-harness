package taskcoord

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
)

// Persist IDs and small validation records. Command output stays in its managed
// job/artifact instead of being copied into the polling checkpoint on every tick.
type goalVerificationProgress struct {
	ScopeVersion int                     `json:"scope_version,omitempty"`
	Snapshot     product.ProjectSnapshot `json:"source_snapshot"`
	RunID        string                  `json:"run_id"`
	AttemptID    string                  `json:"attempt_id"`
	PacketHash   string                  `json:"packet_hash"`
	Root         string                  `json:"root"`
	Actor        string                  `json:"actor"`
	Checks       []CommandCheck          `json:"checks"`
	Index        int                     `json:"index"`
	OperationID  string                  `json:"operation_id,omitempty"`
	JobID        string                  `json:"job_id,omitempty"`
	Jobs         []string                `json:"jobs"`
	Validations  []taskengine.Validation `json:"validations"`
	EvidenceID   string                  `json:"evidence_id,omitempty"`
	RollbackID   string                  `json:"rollback_id,omitempty"`
}

func goalCheckTimeout(g taskengine.GoalRun) int {
	if g.Config.VerificationTimeoutSeconds == 0 {
		return 120
	}
	return g.Config.VerificationTimeoutSeconds
}

func decodeGoalVerification(v taskengine.GoalVerification) (goalVerificationProgress, error) {
	var p goalVerificationProgress
	err := json.Unmarshal(v.Progress, &p)
	if err == nil && (len(p.Checks) == 0 || p.Index < 0 || p.Index > len(p.Checks) || len(p.Jobs) != p.Index || len(p.Validations) != p.Index || p.Index == len(p.Checks) && (p.JobID != "" || p.OperationID != "")) {
		err = fmt.Errorf("verification checkpoint is inconsistent")
	}
	return p, err
}

func (s *Service) validateAsyncVerificationScope(ctx context.Context, g taskengine.GoalRun, x taskengine.ExecutionSnapshot, v taskengine.GoalVerification) error {
	p, err := decodeGoalVerification(v)
	if err != nil {
		return err
	}
	if p.Snapshot.Root != p.Root || p.Snapshot.Hashes == nil {
		return fmt.Errorf("verification source snapshot is missing or bound to another root")
	}
	if x.Run == nil || x.Attempt == nil || x.Proposal == nil || x.Run.Owner != g.ID || x.Run.State != taskengine.RunRunning || x.Attempt.State != taskengine.AttemptRunning || p.RunID != x.Run.ID || p.AttemptID != x.Attempt.ID || p.PacketHash != x.Proposal.PacketHash || p.Root != g.Config.ProjectRoot || v.GoalID != g.ID || v.ProposalID != x.Proposal.ID {
		return fmt.Errorf("verification authority changed")
	}
	checks, err := s.frozenChecks(ctx, x.Proposal.ID, goalCheckTimeout(g))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(checks, p.Checks) {
		return fmt.Errorf("frozen checks changed")
	}
	loaded, err := s.loadProposalBundle(ctx, *x.Proposal)
	if err != nil {
		return err
	}
	if err = validateVerificationScope(loaded, p.Snapshot, p.ScopeVersion); err != nil {
		return err
	}
	matches, err := s.tasks.VerificationEffectsMatch(ctx, x.Task.ID, p.OperationID)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("unrelated or uncertain effects block verification")
	}
	report, err := s.tasks.EvaluateCompletion(ctx, x.Task.ID, x.Task.Revision)
	if err != nil {
		return err
	}
	for _, gate := range report.Gates {
		if (gate.ID == "desktop_effects" || gate.ID == "browser_effects") && gate.Status != taskengine.ValidationPass {
			return fmt.Errorf("unresolved desktop/browser effects block verification")
		}
	}
	return nil
}

func (s *Service) goalVerificationPending(ctx context.Context, g taskengine.GoalRun, x taskengine.ExecutionSnapshot) (bool, error) {
	if x.Proposal == nil || x.Proposal.State != taskengine.ProposalApplied {
		return false, nil
	}
	v, err := s.tasks.GoalVerification(ctx, g.ID, x.Proposal.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if v.State != "active" {
		return false, nil
	}
	if err = s.validateAsyncVerificationScope(ctx, g, x, v); err != nil {
		return false, err
	}
	p, err := decodeGoalVerification(v)
	if err != nil {
		return false, err
	}
	if p.JobID == "" {
		if p.OperationID == "" && p.Index < len(p.Checks) {
			capacity, err := s.products.VerificationCapacity(ctx)
			return !capacity, err
		}
		return false, nil
	}
	job, err := s.products.GetJob(ctx, p.JobID)
	if err != nil {
		return false, err
	}
	if !verificationJobMatches(job, x.Task.ProjectID, p, p.Checks[p.Index]) {
		return false, fmt.Errorf("verification job binding changed")
	}
	return job.State == "queued" || job.State == "running", nil
}

func (s *Service) saveGoalVerification(ctx context.Context, g taskengine.GoalRun, v taskengine.GoalVerification, p goalVerificationProgress, state string) (taskengine.GoalVerification, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return v, err
	}
	return s.tasks.SaveGoalVerification(ctx, g, v.ProposalID, v.Revision, state, body)
}

func (s *Service) beginGoalVerification(ctx context.Context, g taskengine.GoalRun, x taskengine.ExecutionSnapshot, actor string) (map[string]any, error) {
	checks, err := s.frozenChecks(ctx, x.Proposal.ID, goalCheckTimeout(g))
	if err != nil {
		return nil, err
	}
	prepared, err := s.prepareVerification(ctx, x.Proposal.ID, actor, checks)
	if err != nil {
		return nil, err
	}
	if prepared.root == "" || prepared.root != g.Config.ProjectRoot {
		return nil, fmt.Errorf("verification requires the goal's frozen project root")
	}
	p := goalVerificationProgress{RunID: x.Run.ID, AttemptID: x.Attempt.ID, PacketHash: x.Proposal.PacketHash, Root: prepared.root, Actor: actor, Checks: checks, Snapshot: prepared.snapshot, ScopeVersion: prepared.scopeVersion}
	_, err = s.saveGoalVerification(ctx, g, taskengine.GoalVerification{ProposalID: x.Proposal.ID}, p, "active")
	if err != nil {
		return nil, err
	}
	return s.advanceGoalVerification(ctx, g, x)
}

func verificationJobMatches(job product.Job, projectID string, p goalVerificationProgress, check CommandCheck) bool {
	args, _ := json.Marshal(job.Payload["arguments"])
	wantArgs, _ := json.Marshal(check.Arguments)
	return job.Kind == "command" && job.Payload["project_id"] == projectID && job.Payload["operation_id"] == p.OperationID && job.Payload["project_root"] == p.Root && job.Payload["actor"] == p.Actor && job.Payload["executable"] == check.Executable && job.Payload["working_dir"] == check.WorkingDir && string(args) == string(wantArgs) && fmt.Sprint(job.Payload["timeout_seconds"]) == fmt.Sprint(check.TimeoutSeconds)
}

func (s *Service) advanceGoalVerification(ctx context.Context, g taskengine.GoalRun, x taskengine.ExecutionSnapshot) (map[string]any, error) {
	v, err := s.tasks.GoalVerification(ctx, g.ID, x.Proposal.ID)
	if err != nil {
		return nil, err
	}
	if v.State != "active" {
		return nil, fmt.Errorf("verification is not active; inspect retained operation")
	}
	if err = s.validateAsyncVerificationScope(ctx, g, x, v); err != nil {
		return nil, err
	}
	p, err := decodeGoalVerification(v)
	if err != nil {
		return nil, err
	}
	authority := taskengine.RunAuthority{RunID: x.Run.ID, LeaseToken: x.Run.LeaseToken}
	for p.Index < len(p.Checks) {
		check := p.Checks[p.Index]
		if p.OperationID == "" {
			release, err := s.products.GuardProjectSnapshot(ctx, x.Proposal.ProjectID, p.Snapshot)
			if err != nil {
				return nil, err
			}
			release()
			capacity, capacityErr := s.products.VerificationCapacity(ctx)
			if capacityErr != nil {
				return nil, capacityErr
			}
			if !capacity {
				return map[string]any{"proposal_id": x.Proposal.ID, "verification_pending": true, "queued_for_capacity": true}, nil
			}
			effect, planErr := s.tasks.PlanEffect(ctx, authority, p.AttemptID, "workspace.run", check.Executable+" "+strings.Join(check.Arguments, " "), "review:"+x.Proposal.ID)
			if planErr != nil {
				return nil, planErr
			}
			p.OperationID = effect.OperationID
			v, err = s.saveGoalVerification(ctx, g, v, p, "active")
			if err != nil {
				return nil, err
			}
		}
		effects, loadErr := s.tasks.AttemptEffects(ctx, p.AttemptID)
		if loadErr != nil {
			return nil, loadErr
		}
		var effect taskengine.EffectIntent
		for _, e := range effects {
			if e.OperationID == p.OperationID {
				effect = e
				break
			}
		}
		if effect.OperationID == "" || effect.Action != "workspace.run" {
			return nil, fmt.Errorf("verification operation missing")
		}
		if p.JobID == "" {
			if effect.State == taskengine.EffectPlanned {
				effect, err = s.tasks.DispatchEffect(ctx, authority, p.OperationID)
				if err != nil {
					return nil, err
				}
				job, startErr := s.products.StartVerificationCommand(ctx, product.CommandInput{ProjectID: x.Proposal.ProjectID, ExpectedRoot: p.Root, OperationID: p.OperationID, Actor: p.Actor, Executable: check.Executable, Arguments: check.Arguments, WorkingDir: check.WorkingDir, TimeoutSeconds: check.TimeoutSeconds})
				if startErr != nil {
					if errors.Is(startErr, product.ErrVerificationBusy) {
						if _, err = s.tasks.ObserveEffect(ctx, p.OperationID, map[string]any{"status": "capacity_wait", "command_started": false}); err != nil {
							return nil, err
						}
						p.OperationID = ""
						if _, err = s.saveGoalVerification(ctx, g, v, p, "active"); err != nil {
							return nil, err
						}
						return map[string]any{"proposal_id": x.Proposal.ID, "verification_pending": true, "queued_for_capacity": true}, nil
					}
					// A lost insert response can still have committed a unique job. Adopt
					// only its exact binding, never launch this operation a second time.
					job, err = s.products.FindJobByOperationID(ctx, p.OperationID)
					if err != nil {
						if !errors.Is(err, sql.ErrNoRows) {
							return nil, err
						}
						if _, err = s.tasks.ObserveEffect(ctx, p.OperationID, map[string]any{"status": "launch_failed"}); err != nil {
							return nil, err
						}
						return s.failGoalVerification(ctx, g, x, v, p, startErr)
					}
				}
				p.JobID = job.ID
			} else if effect.State == taskengine.EffectDispatched || effect.State == taskengine.EffectObserved {
				job, findErr := s.products.FindJobByOperationID(ctx, p.OperationID)
				if findErr != nil {
					return nil, fmt.Errorf("dispatched verification has no confirmed job receipt; inspect before retry: %w", findErr)
				}
				p.JobID = job.ID
			} else {
				return nil, fmt.Errorf("verification effect is uncertain or abandoned")
			}
			v, err = s.saveGoalVerification(ctx, g, v, p, "active")
			if err != nil {
				return nil, err
			}
		}
		job, jobErr := s.products.GetJob(ctx, p.JobID)
		if jobErr != nil {
			return nil, jobErr
		}
		if !verificationJobMatches(job, x.Proposal.ProjectID, p, check) {
			return nil, fmt.Errorf("verification job differs from frozen command")
		}
		if job.State == "queued" || job.State == "running" {
			return map[string]any{"proposal_id": x.Proposal.ID, "job_id": p.JobID, "operation_id": p.OperationID, "verification_pending": true}, nil
		}
		if job.State != "completed" && job.State != "failed" {
			return nil, fmt.Errorf("verification process interrupted; inspect retained job and effect")
		}
		status := taskengine.ValidationFail
		if job.State == "completed" {
			status = taskengine.ValidationPass
		}
		actual := fmt.Sprintf("state=%s exit_code=%v", job.State, job.Result["exit_code"])
		if output, _ := job.Result["output"].(string); emptyTestDiscovery(check.Executable, check.Arguments, output) {
			status = taskengine.ValidationFail
			actual += "; no tests discovered: this does not verify acceptance"
		}
		refs := []string{"job:" + job.ID}
		if id, _ := job.Result["artifact_id"].(string); id != "" {
			refs = append(refs, "artifact:"+id)
		}
		if effect.State == taskengine.EffectDispatched {
			if _, err = s.tasks.ObserveEffect(ctx, p.OperationID, map[string]any{"status": job.State, "job_id": job.ID, "evidence_refs": refs}); err != nil {
				return nil, err
			}
		} else if effect.State != taskengine.EffectObserved {
			return nil, fmt.Errorf("verification effect cannot be collected")
		}
		release, scopeErr := s.products.GuardProjectSnapshot(ctx, x.Proposal.ProjectID, p.Snapshot)
		if scopeErr != nil {
			return nil, fmt.Errorf("verification source snapshot changed: %w", scopeErr)
		}
		release()
		var step taskengine.Step
		for _, candidate := range x.Task.Plan.Steps {
			if candidate.ID == x.Proposal.StepID {
				step = candidate
				break
			}
		}
		validation, validationErr := s.tasks.RecordValidation(ctx, taskengine.Validation{TaskID: x.Task.ID, StepID: step.ID, CheckID: check.ID, SubjectRevision: taskengine.StepSubjectRevision(step), Status: status, Expected: "exit code 0 and nonempty discovery for test commands", Actual: actual, EvidenceRefs: refs})
		if validationErr != nil {
			return nil, validationErr
		}
		p.Jobs = append(p.Jobs, job.ID)
		p.Validations = append(p.Validations, validation)
		p.Index++
		p.JobID = ""
		p.OperationID = ""
		v, err = s.saveGoalVerification(ctx, g, v, p, "active")
		if err != nil {
			return nil, err
		}
		if status != taskengine.ValidationPass {
			return s.failGoalVerification(ctx, g, x, v, p, fmt.Errorf("mandatory check %s failed", check.ID))
		}
	}
	output, err := s.loadGoalVerificationOutput(ctx, p)
	if err != nil {
		return nil, err
	}
	output, err = s.finishVerification(ctx, *x.Proposal, p.Checks, output, p.Snapshot, p.ScopeVersion)
	if err != nil {
		return nil, err
	}
	p.EvidenceID = output.Evidence.ID
	if _, err = s.saveGoalVerification(ctx, g, v, p, "completed"); err != nil {
		return nil, err
	}
	return map[string]any{"proposal_id": output.Proposal.ID, "evidence_artifact_id": output.Evidence.ID, "verification_pending": false}, nil
}

func (s *Service) loadGoalVerificationOutput(ctx context.Context, p goalVerificationProgress) (VerifyOutput, error) {
	out := VerifyOutput{Validations: p.Validations}
	for _, id := range p.Jobs {
		job, err := s.products.GetJob(ctx, id)
		if err != nil {
			return out, err
		}
		out.Jobs = append(out.Jobs, job)
	}
	return out, nil
}

func (s *Service) failGoalVerification(ctx context.Context, g taskengine.GoalRun, x taskengine.ExecutionSnapshot, v taskengine.GoalVerification, p goalVerificationProgress, cause error) (map[string]any, error) {
	output, err := s.loadGoalVerificationOutput(ctx, p)
	if err != nil {
		return nil, err
	}
	output, verifyErr := s.failVerification(ctx, *x.Proposal, output, cause)
	p.RollbackID = output.RollbackEvidence.ID
	if _, err = s.saveGoalVerification(ctx, g, v, p, "failed"); err != nil {
		return nil, err
	}
	return map[string]any{"proposal_id": output.Proposal.ID, "rollback_artifact_id": output.RollbackEvidence.ID, "rolled_back": output.RolledBack}, verifyErr
}

func (s *Service) cancelGoalCommands(ctx context.Context, g taskengine.GoalRun) error {
	x, err := s.tasks.Execution(ctx, g.TaskID)
	if err != nil {
		return err
	}
	if x.Run == nil || x.Run.Owner != g.ID {
		return nil
	}
	for _, e := range x.Effects {
		// Another Stop/worker can interrupt execution before this traversal.
		// Its exact managed job still needs cancellation even though the effect
		// has already become uncertain. Uncertainty never relinquishes ownership.
		if e.Action != "workspace.run" || (e.State != taskengine.EffectDispatched && e.State != taskengine.EffectUncertain) {
			continue
		}
		job, err := s.products.FindJobByOperationID(ctx, e.OperationID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if job.State == "queued" || job.State == "running" {
			if _, err = s.products.CancelJob(ctx, job.ID); err != nil {
				// Completion can race Stop between the read and cancel update.
				// A confirmed terminal job already satisfies the stop request.
				current, readErr := s.products.GetJob(ctx, job.ID)
				if readErr != nil {
					return readErr
				}
				if current.State == "queued" || current.State == "running" {
					return err
				}
			}
		}
	}
	return nil
}

func (s *Service) waitGoalAndCancel(ctx context.Context, g taskengine.GoalRun, reason string) (taskengine.GoalRun, error) {
	waiting, err := s.tasks.WaitGoal(ctx, g.ID, reason)
	if err != nil {
		return waiting, err
	}
	if err = s.cancelGoalCommands(ctx, waiting); err != nil {
		return waiting, err
	}
	if err = s.tasks.InterruptGoalExecution(ctx, waiting.ID); err != nil {
		return waiting, err
	}
	return waiting, nil
}

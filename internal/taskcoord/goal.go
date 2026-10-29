package taskcoord

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"hermetrix-harness/internal/taskengine"
)

type StartGoalInput struct {
	GoalID                     string `json:"goal_id,omitempty"`
	VerificationTimeoutSeconds int    `json:"verification_timeout_seconds"`
	TaskID                     string `json:"task_id"`
	ExpectedTaskRevision       int    `json:"expected_task_revision"`
	Actor                      string `json:"actor"`
	ProviderID                 string `json:"provider_id"`
	ReviewerProviderID         string `json:"reviewer_provider_id"`
	ApplyChanges               bool   `json:"apply_changes"`
	MaxActions                 int    `json:"max_actions"`
	MaxAttempts                int    `json:"max_attempts"`
	MaxPlans                   int    `json:"max_plans"`
	MaxSeconds                 int    `json:"max_seconds"`
}

// StartGoal pins the exact profiles and requirement that the owner authorizes.
// It does not contact a model or start work in the request goroutine.
func (s *Service) StartGoal(ctx context.Context, in StartGoalInput) (taskengine.GoalRun, error) {
	task, err := s.tasks.Get(ctx, in.TaskID)
	if err != nil {
		return taskengine.GoalRun{}, err
	}
	if task.ProjectID == "" {
		return taskengine.GoalRun{}, fmt.Errorf("coding goals require a project")
	}
	projectRoot, err := s.products.ProjectRootIdentity(ctx, task.ProjectID)
	if err != nil {
		return taskengine.GoalRun{}, err
	}
	writer, err := s.providers.Get(ctx, in.ProviderID)
	if err != nil {
		return taskengine.GoalRun{}, err
	}
	reviewer, err := s.providers.Get(ctx, in.ReviewerProviderID)
	if err != nil {
		return taskengine.GoalRun{}, err
	}
	if !writer.Enabled || !reviewer.Enabled {
		return taskengine.GoalRun{}, fmt.Errorf("both goal providers must be enabled")
	}
	if err = validateTaskEgress(task, writer); err != nil {
		return taskengine.GoalRun{}, err
	}
	if err = validateTaskEgress(task, reviewer); err != nil {
		return taskengine.GoalRun{}, err
	}
	if in.MaxActions == 0 {
		in.MaxActions = 100
	}
	if in.MaxAttempts == 0 {
		in.MaxAttempts = 8
	}
	if in.MaxPlans == 0 {
		in.MaxPlans = 3
	}
	if in.MaxSeconds == 0 {
		in.MaxSeconds = 3600
	}
	if in.VerificationTimeoutSeconds == 0 {
		in.VerificationTimeoutSeconds = 120
	}
	if in.VerificationTimeoutSeconds < 1 || in.VerificationTimeoutSeconds > 86400 {
		return taskengine.GoalRun{}, fmt.Errorf("verification_timeout_seconds must be 1–86400")
	}
	config := taskengine.GoalConfig{
		VerificationTimeoutSeconds: in.VerificationTimeoutSeconds,
		ProjectRoot:                projectRoot,
		ProviderID:                 writer.ID, ProviderRevision: writer.Revision, ReviewerProviderID: reviewer.ID, ReviewerProviderRevision: reviewer.Revision,
		ApplyChanges: in.ApplyChanges, MaxActions: in.MaxActions, MaxAttempts: in.MaxAttempts, MaxPlans: in.MaxPlans, MaxSeconds: in.MaxSeconds}
	if in.GoalID != "" {
		return s.tasks.CreateGoalAtID(ctx, in.GoalID, in.TaskID, in.ExpectedTaskRevision, in.Actor, config)
	}
	return s.tasks.CreateGoal(ctx, in.TaskID, in.ExpectedTaskRevision, in.Actor, config)
}

// ServeGoals owns two bounded workers. A database claim prevents concurrent
// dispatch of the same goal. Model inference still uses the shared scheduler.
// Closing the server context cancels requests and joins both workers.
func (s *Service) ServeGoals(ctx context.Context) {
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					ids, err := s.tasks.ReadyGoals(ctx, 2)
					if err != nil {
						continue
					}
					for _, id := range ids {
						if ctx.Err() != nil {
							return
						}
						_, _ = s.AdvanceGoal(ctx, id)
					}
				}
			}
		}()
	}
	workers.Wait()
}

// AdvanceGoal executes at most one durable action. Each external effect retains
// its ordinary operation ID, policy checks, rollback and uncertainty semantics.
func (s *Service) AdvanceGoal(ctx context.Context, id string) (taskengine.GoalRun, error) {
	g, err := s.tasks.Goal(ctx, id)
	if err != nil {
		return g, err
	}
	if g.State != taskengine.GoalActive || g.ClaimToken != "" {
		return g, fmt.Errorf("goal is not idle and active")
	}
	snapshot, err := s.tasks.Execution(ctx, g.TaskID)
	if err != nil {
		return g, err
	}
	task := snapshot.Task
	projectRoot, rootErr := s.products.ProjectRootIdentity(ctx, task.ProjectID)
	if rootErr != nil || g.Config.ProjectRoot == "" || projectRoot != g.Config.ProjectRoot {
		return s.waitGoalAndCancel(ctx, g, "project root changed or was not frozen; start a goal for the reviewed workspace root")
	}
	if task.Revision != g.ExpectedTaskRevision || task.ActiveRequirementRevision != g.RequirementRevision {
		return s.waitGoalAndCancel(ctx, g, "task changed outside the runner; inspect and resume against its exact revision")
	}
	for _, pin := range []struct{ id, revision string }{{g.Config.ProviderID, g.Config.ProviderRevision}, {g.Config.ReviewerProviderID, g.Config.ReviewerProviderRevision}} {
		profile, loadErr := s.providers.Get(ctx, pin.id)
		if loadErr != nil || !profile.Enabled || profile.Revision != pin.revision {
			return s.waitGoalAndCancel(ctx, g, "provider changed or unavailable; start a new goal to authorize another profile")
		}
	}
	if g.Deadline.After(time.Now().UTC()) {
		pending, pollErr := s.goalVerificationPending(ctx, g, snapshot)
		if pollErr != nil {
			return s.waitGoalAndCancel(ctx, g, "verification receipt unavailable; inspect the existing operation before resuming")
		}
		if pending {
			if snapshot.Run != nil && snapshot.Run.LeaseExpiresAt.Before(time.Now().Add(5*time.Minute)) {
				if _, err = s.tasks.RenewRunLease(ctx, snapshot.Run.ID, snapshot.Run.LeaseToken, 15*time.Minute); err != nil {
					return s.waitGoalAndCancel(ctx, g, "verification run lease could not be renewed")
				}
			}
			return g, s.tasks.DeferGoalPoll(ctx, id)
		}
	}
	action, reason, err := s.nextGoalAction(ctx, g, snapshot)
	if err != nil {
		return g, err
	}
	if action == "" {
		return s.waitGoalAndCancel(ctx, g, reason)
	}
	g, intent, err := s.tasks.ClaimGoalAction(ctx, id, action)
	if err != nil {
		if current, loadErr := s.tasks.Goal(ctx, id); loadErr == nil && current.State == taskengine.GoalBudgetExhausted {
			if cancelErr := s.cancelGoalCommands(ctx, current); cancelErr != nil {
				return current, cancelErr
			}
			if pauseErr := s.tasks.InterruptGoalExecution(ctx, id); pauseErr != nil {
				return current, pauseErr
			}
			return current, err
		}
		return g, err
	}
	actionCtx, cancel := context.WithDeadline(ctx, g.Deadline)
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		lastRenewal := time.Now()
		for {
			select {
			case <-actionCtx.Done():
				return
			case <-ticker.C:
				current, loadErr := s.tasks.Goal(actionCtx, id)
				if loadErr != nil || current.State == taskengine.GoalStopping {
					cancel()
					return
				}
				if snapshot.Run != nil && snapshot.Run.State == taskengine.RunRunning && time.Since(lastRenewal) >= 30*time.Second {
					if _, leaseErr := s.tasks.RenewRunLease(actionCtx, snapshot.Run.ID, snapshot.Run.LeaseToken, 15*time.Minute); leaseErr != nil {
						cancel()
						return
					}
					lastRenewal = time.Now()
				}
			}
		}
	}()
	receipt, actionErr := s.executeGoalAction(actionCtx, g, action, snapshot)
	interrupted := actionCtx.Err() != nil
	cancel()
	<-monitorDone
	if receipt == nil {
		receipt = map[string]any{}
	}
	receipt["action"] = action
	state, reason := taskengine.GoalActive, ""
	if actionErr != nil {
		// Keep errors from third-party services out of persisted goal metadata.
		// Detailed evidence already belongs to the operation-bound artifacts.
		state, reason = taskengine.GoalWaiting, "action did not complete; inspect saved operation receipts before resuming"
		receipt["outcome"] = "requires_inspection"
	} else {
		receipt["outcome"] = "observed"
	}
	if (action == "verify" || action == "verify_collect") && receipt["rolled_back"] == true {
		state, reason = taskengine.GoalActive, "check failed; source restored; next attempt uses recorded failure evidence"
	}
	if action == "review" && receipt["rejected"] == true && receipt["rolled_back"] == true {
		state, reason = taskengine.GoalActive, "review rejected the change; source restored; investigate and retry"
	}
	if action == "propose" && receipt["complete_proposal_rejected"] == true {
		state, reason = taskengine.GoalActive, "complete proposal rejected before apply; next bounded attempt uses saved diagnostic"
		receipt["outcome"] = "proposal_rejected"
	}
	if actionErr == nil && action == "finish" {
		state, reason = taskengine.GoalCompleted, "all current completion gates passed"
	}
	if actionErr == nil && action == "review" {
		current, getErr := s.tasks.Get(ctx, g.TaskID)
		if getErr == nil && current.State == taskengine.StateCompleted {
			state, reason = taskengine.GoalCompleted, "all current completion gates passed"
		}
	}
	if action != "plan" {
		if current, getErr := s.tasks.Get(ctx, g.TaskID); getErr == nil && current.ActivePlanRevision != task.ActivePlanRevision {
			state, reason = taskengine.GoalWaiting, "plan changed during dispatch; inspect current authority before resuming"
		}
	}
	// Persist cancellation outcomes even when the server is shutting down.
	cleanup, closeCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer closeCleanup()
	if state == taskengine.GoalCompleted {
		release, scopeErr := s.guardGoalCompletionSnapshot(cleanup, g, snapshot)
		if scopeErr != nil {
			state, reason = taskengine.GoalWaiting, "verified source snapshot changed; inspect before closing this goal"
			receipt["outcome"] = "requires_inspection"
			actionErr = scopeErr
		} else {
			defer release()
		}
	}
	if interrupted && snapshot.Attempt != nil {
		// Managed commands have their own process lifetime; cancel only jobs
		// bound to this exact attempt rather than leaving them running on Stop.
		effects, loadErr := s.tasks.AttemptEffects(cleanup, snapshot.Attempt.ID)
		if loadErr == nil {
			for _, effect := range effects {
				if effect.Action == "workspace.run" && effect.State == taskengine.EffectDispatched {
					if job, jobErr := s.products.FindJobByOperationID(cleanup, effect.OperationID); jobErr == nil && (job.State == "queued" || job.State == "running") {
						_, _ = s.products.CancelJob(cleanup, job.ID)
					}
				}
			}
		}
	}
	finished, finishErr := s.tasks.FinishGoalAction(cleanup, id, intent.ID, state, reason, receipt)
	if finishErr != nil {
		return finished, finishErr
	}
	if finished.State == taskengine.GoalWaiting && (action == "verify" || action == "verify_collect") {
		if err := s.cancelGoalCommands(cleanup, finished); err != nil {
			return finished, err
		}
		if err := s.tasks.InterruptGoalExecution(cleanup, finished.ID); err != nil {
			return finished, err
		}
	}
	if finished.State == taskengine.GoalStopped || finished.State == taskengine.GoalBudgetExhausted || interrupted {
		// Stop can observe the effect before StartVerificationCommand has saved
		// its job. The action's final boundary must cancel again after dispatch
		// has settled, using the same operation rather than launching anything.
		if err := s.cancelGoalCommands(cleanup, finished); err != nil {
			return finished, err
		}
		if err := s.tasks.InterruptGoalExecution(cleanup, id); err != nil {
			return finished, err
		}
	}
	return finished, actionErr
}

func (s *Service) StopGoal(ctx context.Context, id string) (taskengine.GoalRun, error) {
	g, err := s.tasks.StopGoal(ctx, id)
	if err == nil {
		err = s.cancelGoalCommands(ctx, g)
	}
	if err == nil && g.State == taskengine.GoalStopped {
		err = s.tasks.InterruptGoalExecution(ctx, id)
	}
	return g, err
}

func (s *Service) nextGoalAction(ctx context.Context, g taskengine.GoalRun, x taskengine.ExecutionSnapshot) (string, string, error) {
	t := x.Task
	if t.State == taskengine.StateCancelled || t.State == taskengine.StateFailed {
		return "", "task reached a terminal failure or cancellation; objective remains unconfirmed", nil
	}
	if len(t.Requirement.Unknowns) > 0 {
		return "", "requirements contain unresolved unknowns", nil
	}
	if x.Proposal != nil && x.Proposal.State == taskengine.ProposalApplied {
		v, loadErr := s.tasks.GoalVerification(ctx, g.ID, x.Proposal.ID)
		if loadErr == nil && v.State == "active" {
			if err := s.validateAsyncVerificationScope(ctx, g, x, v); err != nil {
				return "", "verification scope or effects changed; inspect saved operation", nil
			}
			return "verify_collect", "", nil
		}
		if loadErr != nil && !errors.Is(loadErr, sql.ErrNoRows) {
			return "", "", loadErr
		}
		if loadErr == nil {
			return "", "interrupted verification requires inspection; command is not replayed", nil
		}
	}
	report, err := s.tasks.EvaluateCompletion(ctx, t.ID, t.Revision)
	if err != nil {
		return "", "", err
	}
	for _, gate := range report.Gates {
		if (gate.ID == "effects" || gate.ID == "desktop_effects" || gate.ID == "browser_effects") && gate.Status != taskengine.ValidationPass {
			return "", "existing effects require receipts or reconciliation before new work", nil
		}
	}
	if t.State == taskengine.StateCompleted || report.CanComplete {
		return "finish", "", nil
	}
	if report.AwaitingUserAcceptance {
		return "", "user acceptance required for the exact current evidence", nil
	}
	if x.Run != nil && x.Run.Owner != g.ID {
		openProposal := x.Proposal != nil && (x.Proposal.State == taskengine.ProposalPendingReview || x.Proposal.State == taskengine.ProposalApproved || x.Proposal.State == taskengine.ProposalApplying || x.Proposal.State == taskengine.ProposalApplied || x.Proposal.State == taskengine.ProposalAwaitingReview || x.Proposal.State == taskengine.ProposalRecoveryRequired)
		if x.Run.State == taskengine.RunRunning || openProposal {
			return "", "another execution owns unfinished work; reconcile it before starting this goal", nil
		}
	}
	if t.Requirement.Contract.RequirePlanReview && t.Plan.ReviewStatus == "pending" {
		return "", "current plan requires owner review", nil
	}
	if x.Proposal != nil && (x.Proposal.State == taskengine.ProposalApplying || x.Proposal.State == taskengine.ProposalApplyFailed || x.Proposal.State == taskengine.ProposalRecoveryRequired) {
		return "", "proposal apply requires inspection and reconciliation", nil
	}
	if x.Proposal != nil && x.Proposal.State == taskengine.ProposalAwaitingReview && x.Attempt != nil && x.Attempt.State == taskengine.AttemptInterrupted {
		return "recover_review", "", nil
	}
	if x.Proposal != nil && x.Proposal.State == taskengine.ProposalAwaitingReview && x.Attempt != nil && x.Attempt.State == taskengine.AttemptCompleted && t.State == taskengine.StatePaused {
		return "recover_approved_review", "", nil
	}
	escalation, err := s.tasks.PendingEscalation(ctx, t.ID)
	if err != nil {
		return "", "", err
	}
	if escalation != nil || t.ActivePlanRevision == 0 || t.Plan.ReviewStatus == "rejected" {
		return "plan", "", nil
	}
	if t.State == taskengine.StateVerifying {
		return "", "current evidence does not satisfy every completion gate", nil
	}
	if x.Run == nil || x.Run.PlanRevision != t.ActivePlanRevision || x.Run.State != taskengine.RunRunning {
		if t.State == taskengine.StateReady || t.State == taskengine.StatePaused {
			return "begin_run", "", nil
		}
		return "", "no current run authority; inspect the saved task state", nil
	}
	if x.Run.Owner != g.ID {
		return "", "another execution owns this task", nil
	}
	if !x.Run.LeaseExpiresAt.After(time.Now().UTC()) {
		return "", "run lease expired; use explicit run recovery before resuming", nil
	}
	if x.Proposal != nil && x.Proposal.State == taskengine.ProposalAwaitingReview && x.Attempt != nil && x.Attempt.State == taskengine.AttemptCompleted {
		return "review", "", nil
	}
	if x.Attempt == nil || x.Attempt.State == taskengine.AttemptCompleted || x.Attempt.State == taskengine.AttemptFailed {
		return "attempt", "", nil
	}
	if x.Attempt.State != taskengine.AttemptRunning {
		return "", "interrupted attempt requires explicit recovery", nil
	}
	if x.Proposal == nil {
		return "propose", "", nil
	}
	switch x.Proposal.State {
	case taskengine.ProposalPendingReview:
		if !g.Config.ApplyChanges {
			return "", "proposal needs owner review; automatic apply was not authorized", nil
		}
		return "approve", "", nil
	case taskengine.ProposalApproved:
		if !g.Config.ApplyChanges {
			return "", "approved proposal awaits explicit apply", nil
		}
		return "apply", "", nil
	case taskengine.ProposalApplied:
		return "verify", "", nil
	case taskengine.ProposalAwaitingReview:
		return "review", "", nil
	default:
		return "", "proposal requires an explicit recovery or review decision", nil
	}
}

func (s *Service) executeGoalAction(ctx context.Context, g taskengine.GoalRun, action string, x taskengine.ExecutionSnapshot) (map[string]any, error) {
	t := x.Task
	actor := "goal:" + g.ID + ":" + g.Actor
	var auth taskengine.RunAuthority
	if x.Run != nil {
		auth = taskengine.RunAuthority{RunID: x.Run.ID, LeaseToken: x.Run.LeaseToken}
	}
	switch action {
	case "plan":
		out, err := s.AutoPlan(ctx, AutoPlanInput{TaskID: t.ID, ExpectedTaskRevision: t.Revision, ProviderID: g.Config.ProviderID, Actor: actor})
		return map[string]any{"planner_run_id": out.Run.ID, "artifact_id": out.Artifact.ID, "plan_revision": out.Task.ActivePlanRevision}, err
	case "begin_run":
		run, err := s.tasks.BeginRun(ctx, taskengine.BeginRunInput{TaskID: t.ID, ExpectedTaskRevision: t.Revision, Owner: g.ID, LeaseDuration: 15 * time.Minute})
		return map[string]any{"run_id": run.ID}, err
	case "attempt":
		packet, err := s.tasks.BuildNextStepPacket(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		attempt, err := s.tasks.BeginStepAttempt(ctx, taskengine.BeginAttemptInput{RunID: auth.RunID, LeaseToken: auth.LeaseToken, StepKey: packet.Step.Key,
			ExpectedTaskRevision: t.Revision, ExpectedStepRevision: packet.Step.Revision, InputHash: packet.CanonicalPacketHash, Packet: &packet})
		return map[string]any{"attempt_id": attempt.ID, "packet_hash": packet.CanonicalPacketHash}, err
	case "propose":
		if out, handled, err := s.tryGoalReduction(ctx, g, x, auth); handled {
			return map[string]any{"proposal_id": out.Proposal.ID, "artifact_id": out.Artifact.ID, "operation_id": out.Effect.OperationID, "origin": "host_single_edit_reduction", "provider_called": false}, err
		}
		selection, err := s.SelectFiles(ctx, SelectFilesInput{AttemptID: x.Attempt.ID, ProviderID: g.Config.ProviderID, Authority: auth})
		if err != nil {
			return map[string]any{"selection_artifact_id": selection.Artifact.ID}, err
		}
		out, err := s.Propose(ctx, ProposalInput{AttemptID: x.Attempt.ID, ProviderID: g.Config.ProviderID, Authority: auth, Files: selection.Result.Files})
		receipt := map[string]any{"proposal_id": out.Proposal.ID, "artifact_id": out.Artifact.ID, "selection_artifact_id": selection.Artifact.ID}
		if err != nil {
			rejected, recoveryErr := s.failRejectedGoalProposal(ctx, x, out, err)
			if recoveryErr != nil {
				return receipt, recoveryErr
			}
			if rejected {
				receipt["complete_proposal_rejected"] = true
				receipt["failed_operation_id"] = out.Effect.OperationID
			}
		}
		return receipt, err
	case "approve":
		out, err := s.tasks.DecideCodeProposal(ctx, x.Proposal.ID, actor, taskengine.ProposalApproved,
			"owner authorized scoped automatic apply in goal "+g.ID+"; frozen writable paths and mandatory checks still apply; independent review follows verification", nil)
		return map[string]any{"proposal_id": out.ID, "authorization": "scoped_goal_consent"}, err
	case "apply":
		out, err := s.Apply(ctx, auth, x.Proposal.ID, actor)
		return map[string]any{"proposal_id": out.Proposal.ID, "effect_id": out.Effect.OperationID, "rollback_artifact_id": out.Proposal.RollbackArtifactID}, err
	case "verify":
		return s.beginGoalVerification(ctx, g, x, actor)
	case "verify_collect":
		return s.advanceGoalVerification(ctx, g, x)
	case "review":
		out, err := s.Review(ctx, auth, x.Proposal.ID, g.Config.ReviewerProviderID)
		return map[string]any{"proposal_id": out.Proposal.ID, "review_artifact_id": out.Artifact.ID, "rollback_artifact_id": out.RollbackEvidence.ID, "rolled_back": out.RolledBack, "rejected": out.Review.Verdict == "reject"}, err
	case "recover_review":
		_, err := s.tasks.ResumeObservedReview(ctx, taskengine.RecoverExpiredRunInput{TaskID: t.ID, ExpectedTaskRevision: t.Revision, Actor: actor})
		return map[string]any{"proposal_id": x.Proposal.ID}, err
	case "recover_approved_review":
		_, err := s.tasks.ResumeApprovedReview(ctx, taskengine.RecoverExpiredRunInput{TaskID: t.ID, ExpectedTaskRevision: t.Revision, Actor: actor})
		return map[string]any{"proposal_id": x.Proposal.ID}, err
	case "finish":
		if t.State != taskengine.StateCompleted {
			var err error
			t, err = s.tasks.CompleteTask(ctx, t.ID, t.Revision)
			if err != nil {
				return nil, err
			}
		}
		report, err := s.tasks.EvaluateCompletion(ctx, t.ID, t.Revision)
		return map[string]any{"completion_subject_hash": report.SubjectHash, "task_revision": t.Revision}, err
	default:
		return nil, fmt.Errorf("unsupported goal action %q", action)
	}
}

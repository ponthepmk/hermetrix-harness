package taskcoord

import (
	"context"
	"errors"
	"fmt"

	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

// failRejectedGoalProposal closes only a complete, rejected model response with
// a durable failed effect and no workspace effects. Transport failures, unknown
// outcomes and partially persisted proposals still require inspection. A new
// attempt is charged to the existing goal; the provider operation is never replayed.
func (s *Service) failRejectedGoalProposal(ctx context.Context, x taskengine.ExecutionSnapshot, out ProposalOutput, cause error) (bool, error) {
	var diagnostic *worker.Error
	if !errors.As(cause, &diagnostic) || diagnostic.Code != worker.ErrorInvalidProposal || diagnostic.Phase != worker.PhaseValidatingProposal ||
		x.Attempt == nil || x.Attempt.State != taskengine.AttemptRunning || out.Proposal.ID != "" || out.Artifact.ID != "" || out.Effect.OperationID == "" {
		return false, nil
	}
	effect, err := s.tasks.Effect(ctx, out.Effect.OperationID)
	if err != nil {
		return false, err
	}
	if effect.Action != proposalEffect || effect.State != taskengine.EffectObserved || effect.AttemptID != x.Attempt.ID || effect.TaskID != x.Task.ID ||
		effect.Receipt["status"] != "failed" || effect.Receipt["failure_code"] != string(worker.ErrorInvalidProposal) ||
		effect.Receipt["failure_phase"] != string(worker.PhaseValidatingProposal) {
		return false, nil
	}
	effects, err := s.tasks.AttemptEffects(ctx, x.Attempt.ID)
	if err != nil {
		return false, err
	}
	for _, e := range effects {
		if (e.Action != selectionEffect && e.Action != proposalEffect) || (e.State != taskengine.EffectObserved && e.State != taskengine.EffectReconciled) {
			return false, nil
		}
	}
	task, err := s.tasks.Get(ctx, x.Task.ID)
	if err != nil {
		return false, err
	}
	if task.Revision != x.Task.Revision || task.ActivePlanRevision != x.Task.ActivePlanRevision || task.ActiveRequirementRevision != x.Task.ActiveRequirementRevision {
		return false, taskengine.ErrStaleRevision
	}
	var step *taskengine.Step
	for i := range task.Plan.Steps {
		if task.Plan.Steps[i].ID == x.Attempt.StepID {
			step = &task.Plan.Steps[i]
			break
		}
	}
	if step == nil || step.State != taskengine.StepRunning {
		return false, fmt.Errorf("rejected proposal no longer belongs to the running step")
	}
	reason := "complete model proposal rejected before apply: " + diagnostic.Message
	if _, err = s.tasks.FailAttempt(ctx, x.Attempt.ID, reason); err != nil {
		return false, err
	}
	if _, err = s.tasks.FailStep(ctx, task.ID, step.Key, task.Revision, step.Revision, reason); err != nil {
		return false, err
	}
	_, err = s.tasks.RecordConfirmedFailure(ctx, taskengine.ConfirmedFailureInput{
		AttemptID: x.Attempt.ID, FailureKind: "execution", Message: reason, EvidenceRefs: []string{"effect:" + effect.OperationID},
	})
	return err == nil, err
}

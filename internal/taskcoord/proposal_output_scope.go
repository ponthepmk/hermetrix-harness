package taskcoord

import (
	"context"
	"fmt"
	"slices"

	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/taskscope"
)

// proposalWritablePaths resolves the authoritative writable path scope for a
// code proposal by binding it to the durable attempt packet. It never derives
// scope from artifact prose.
func (s *Service) proposalWritablePaths(ctx context.Context, proposal taskengine.CodeProposal, task taskengine.Task) ([]string, error) {
	var activeStep *taskengine.Step
	for i := range task.Plan.Steps {
		if task.Plan.Steps[i].ID == proposal.StepID {
			activeStep = &task.Plan.Steps[i]
			break
		}
	}
	if activeStep == nil {
		return nil, fmt.Errorf("step %q not found in active plan", proposal.StepID)
	}

	attempt, err := s.tasks.Attempt(ctx, proposal.AttemptID)
	if err != nil {
		return nil, err
	}

	if attempt.Packet == nil {
		if len(activeStep.OutputPaths) > 0 {
			return nil, fmt.Errorf("explicit step outputs require a durable attempt packet")
		}
		return taskscope.EffectiveWritablePaths(nil, task.Requirement.Contract.WritablePaths)
	}

	packet := attempt.Packet
	if err := taskengine.ValidateStepPacket(*packet); err != nil {
		return nil, err
	}

	if attempt.ID != proposal.AttemptID ||
		attempt.TaskID != proposal.TaskID ||
		attempt.StepID != proposal.StepID ||
		attempt.InputHash != proposal.PacketHash ||
		proposal.PacketHash != packet.CanonicalPacketHash ||
		packet.TaskID != proposal.TaskID ||
		packet.TaskID != task.ID ||
		packet.ProjectID != proposal.ProjectID ||
		packet.ProjectID != task.ProjectID ||
		packet.Step.ID != proposal.StepID ||
		packet.PlanRevision != task.ActivePlanRevision ||
		packet.Requirement.Revision != task.ActiveRequirementRevision {
		return nil, fmt.Errorf("attempt packet bindings do not match proposal and active task")
	}

	if !slices.Equal(packet.Step.OutputPaths, activeStep.OutputPaths) ||
		!slices.Equal(packet.Requirement.Contract.WritablePaths, task.Requirement.Contract.WritablePaths) {
		return nil, fmt.Errorf("packet output or writable scope diverges from active task state")
	}

	return taskscope.EffectiveWritablePaths(packet.Step.OutputPaths, packet.Requirement.Contract.WritablePaths)
}

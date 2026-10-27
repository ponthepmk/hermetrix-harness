package taskcoord

import (
	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/worker"
)

func restoreProposalKnowledge(task worker.Task, refs []worker.KnowledgeRef, preset inference.Preset) worker.Task {
	accepted := append([]worker.KnowledgeRef{}, task.ProjectBrainRefs...)
	for _, ref := range refs {
		trial := task
		trial.ProjectBrainRefs = append([]worker.KnowledgeRef{}, accepted...)
		trial.ProjectBrainRefs = append(trial.ProjectBrainRefs, ref)
		if proposalTaskFits(trial, preset) {
			accepted = trial.ProjectBrainRefs
		}
	}
	task.ProjectBrainRefs = accepted
	return task
}

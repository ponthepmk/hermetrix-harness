package taskcoord

import (
	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/worker"
)

func restoreProposalSkills(task worker.Task, refs []worker.SkillRef, preset inference.Preset) worker.Task {
	accepted := append([]worker.SkillRef{}, task.SkillRefs...)
	for _, ref := range refs {
		trial := task
		trial.SkillRefs = append([]worker.SkillRef{}, accepted...)
		trial.SkillRefs = append(trial.SkillRefs, ref)
		if proposalTaskFits(trial, preset) {
			accepted = trial.SkillRefs
		}
	}
	task.SkillRefs = accepted
	return task
}

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

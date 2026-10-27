package taskcoord

import (
	"errors"
	"strings"

	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

const newline = "\n"

func curateProposalTask(task worker.Task, step taskengine.Step, preset inference.Preset) (worker.Task, error) {
	if preset.ContextMax <= 0 {
		return worker.Task{}, errors.New("preset.ContextMax must be > 0")
	}
	if err := worker.ValidateTaskSourceResources(task); err != nil {
		return worker.Task{}, err
	}
	if proposalTaskFits(task, preset) {
		frozen, _, _, err := worker.FreezeTask(task)
		return frozen, err
	}
	candidate := task
	candidate.ProjectBrainRefs = nil
	if !proposalTaskFits(candidate, preset) {
		if len(task.FileViews) > 0 {
			return worker.Task{}, errors.New("original FileViews must be empty")
		}
		stepText := step.Title + newline + step.Instructions + newline + strings.Join(step.Checks, newline)
		curated, err := curateProposalSources(candidate, stepText, preset)
		if err != nil {
			return worker.Task{}, err
		}
		candidate = curated
	}
	candidate = restoreProposalKnowledge(candidate, task.ProjectBrainRefs, preset)
	frozen, _, _, err := worker.FreezeTask(candidate)
	return frozen, err
}

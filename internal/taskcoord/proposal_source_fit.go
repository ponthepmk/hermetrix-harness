package taskcoord

import (
	"errors"

	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/worker"
)

func curateProposalSources(task worker.Task, stepText string, preset inference.Preset) (worker.Task, error) {
	if preset.ContextMax <= 0 || len(task.FileViews) > 0 {
		return worker.Task{}, errors.New("invalid source-curation scope")
	}

	paths := proposalSourcePaths(task)
	limit := proposalInitialViewLimit(preset.ContextMax)

	for attempt := 0; attempt < 256; attempt++ {
		candidate := task
		candidate.ProjectBrainRefs = nil
		candidate.FileViews = make(map[string][]worker.BoundedFileView)

		for _, path := range paths {
			views, err := proposalFileViews(path, task.Files[path], stepText, limit)
			if err != nil {
				continue
			}
			candidate.FileViews[path] = views
			if proposalTaskFits(candidate, preset) {
				return candidate, nil
			}
		}

		if limit == 256 {
			break
		}
		limit = limit / 2
		if limit < 256 {
			limit = 256
		}
	}

	return worker.Task{}, errors.New("proposal source fit exhausted")
}

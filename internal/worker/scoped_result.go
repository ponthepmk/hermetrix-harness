package worker

// validateWorkerScopedResult validates a worker result against the frozen task
// snapshot. It is intended to be called by the exported ValidateResult wrapper.
func validateWorkerScopedResult(task Task, result Result) error {
	if err := validateAbsentFiles(task); err != nil {
		return err
	}

	task, canonical, _, err := prepareWorkerTask(task)
	if err != nil {
		return workerError(ErrorInvalidTask, PhaseValidatingInput, false, "cannot prepare worker task", err)
	}
	inputHash := Hash(string(canonical))

	if err := validateWorkerResultResources(result); err != nil {
		return invalidProposal("proposal resources are invalid")
	}

	if result.Status != "proposed_unverified" || result.InputSHA256 != inputHash || result.TaskID != task.ID ||
		result.ProposalID != proposalID(inputHash, result) || len(result.Changes) == 0 || len(result.Changes) > len(task.Files) {
		return invalidProposal("proposal identity does not match the task")
	}
	if err := validateRepairTargetCompleteness(task, result); err != nil {
		return invalidProposal(err.Error())
	}

	seen := map[string]bool{}
	for _, change := range result.Changes {
		before, ok := task.Files[change.Path]
		expectedHash := Hash(before)
		validStrategy := change.Strategy == "replace_file" || change.Strategy == "replace_text"
		if absentFile(task, change.Path) {
			expectedHash = "absent"
			validStrategy = change.Strategy == "create_file"
		}
		if !ok || seen[change.Path] || change.BeforeSHA256 != expectedHash || change.Content == before && !absentFile(task, change.Path) ||
			!writablePathAllowed(task, change.Path) || !validStrategy {
			return invalidProposal("proposal change does not match its preimage")
		}
		seen[change.Path] = true
		if _, historical := task.RepairDrafts[change.Path]; historical {
			if err := validateRepairDraftChange(task, change); err != nil {
				return invalidProposal("draft proposal does not match its frozen proof")
			}
			continue
		}
		if change.Repair != nil {
			return invalidProposal("unexpected historical draft proof")
		}

		if partial, ok := task.FileViews[change.Path]; ok && len(partial) > 0 {
			if change.Strategy != "replace_text" {
				return invalidProposal("partial change must use replace_text")
			}
			if len(change.Edits) == 0 {
				return invalidProposal("partial change requires edits")
			}
			rederived, strategy, err := MaterializeBoundedChange(change.Path, before, Hash(before), partial, nil, change.Edits)
			if err != nil {
				return invalidProposal("partial change cannot be rederived from frozen views")
			}
			if strategy != "replace_text" {
				return invalidProposal("partial change rederivation strategy mismatch")
			}
			if rederived != change.Content {
				return invalidProposal("partial change content does not match rederived content")
			}
		}
		if !partialExists(task, change.Path) {
			if len(change.Edits) != 0 {
				return invalidProposal("non-partial change must not contain edits")
			}
		}
	}
	return nil
}

func partialExists(task Task, path string) bool {
	views, ok := task.FileViews[path]
	return ok && len(views) > 0
}

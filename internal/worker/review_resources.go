package worker

import (
	"errors"
	"strings"
)

func validateReviewTaskResources(task ReviewTask) error {
	if strings.TrimSpace(task.TaskID) == "" || strings.TrimSpace(task.ProposalID) == "" || strings.TrimSpace(task.Objective) == "" {
		return errors.New("review task requires nonblank TaskID, ProposalID, and Objective")
	}
	if len(task.AcceptanceCriteria) == 0 || len(task.TestEvidence) == 0 {
		return errors.New("review task requires nonempty AcceptanceCriteria and TestEvidence")
	}
	if total := len(task.Files) + len(task.PartialFiles); total < 1 || total > 32 {
		return errors.New("review task requires 1 to 32 total files")
	}
	if len(task.Files) > 32 || len(task.AcceptanceCriteria) > 32 || len(task.Constraints) > 32 || len(task.StepChecks) > 32 || len(task.TestEvidence) > 64 {
		return errors.New("review task exceeds bounded collection limits")
	}
	remaining := maxWorkerWireBytes
	consume := func(s string) bool {
		if len(s) > remaining {
			return false
		}
		remaining -= len(s)
		return true
	}
	if !consume(task.SourceState) || !consume(task.TaskID) || !consume(task.ProposalID) || !consume(task.Objective) || !consume(task.StepTitle) || !consume(task.StepInstructions) {
		return errors.New("review task exceeds 128 KiB raw wire budget")
	}
	for _, s := range task.StepChecks {
		if !consume(s) {
			return errors.New("review task exceeds 128 KiB raw wire budget")
		}
	}
	for _, s := range task.AcceptanceCriteria {
		if !consume(s) {
			return errors.New("review task exceeds 128 KiB raw wire budget")
		}
	}
	for _, s := range task.Constraints {
		if !consume(s) {
			return errors.New("review task exceeds 128 KiB raw wire budget")
		}
	}
	for _, s := range task.TestEvidence {
		if !consume(s) {
			return errors.New("review task exceeds 128 KiB raw wire budget")
		}
	}
	for path, content := range task.Files {
		if !consume(path) || !consume(content) {
			return errors.New("review task exceeds 128 KiB raw wire budget")
		}
	}
	for path, partial := range task.PartialFiles {
		if len(partial.Regions) < 1 || len(partial.Regions) > 32 {
			return errors.New("each partial file requires 1 to 32 regions")
		}
		if !consume(path) || !consume(partial.BeforeSHA256) || !consume(partial.AfterSHA256) {
			return errors.New("review task exceeds 128 KiB raw wire budget")
		}
		for _, region := range partial.Regions {
			if !consume(region.Before.Path) || !consume(region.Before.FullSHA256) || !consume(region.Before.Content) || !consume(region.Before.RegionID) || !consume(region.AfterContent) {
				return errors.New("review task exceeds 128 KiB raw wire budget")
			}
		}
	}
	return nil
}

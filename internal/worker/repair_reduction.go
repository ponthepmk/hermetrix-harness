package worker

import (
	"errors"
	"slices"
	"strings"
)

// RepairSelection identifies one existing edit, never a new replacement. This
// bounded development strategy is linear, not a powerset search.
type RepairSelection struct {
	Path     string `json:"path"`
	TargetID string `json:"target_id"`
}

const MaxRepairReductionEdits = 4

// RepairSelections accepts only a complete, independently rederivable proposal
// consisting of named draft edits and retained companions. The coordinator must
// separately establish its actual failed check, authority and current preimages.
func RepairSelections(task Task, full Result) ([]RepairSelection, error) {
	if err := ValidateResult(task, full); err != nil {
		return nil, err
	}
	if task.RepairTargetVersion != RepairTargetVersion || len(task.FileViews) != 0 {
		return nil, errors.New("reduction requires versioned named repairs")
	}
	var selections []RepairSelection
	for _, change := range full.Changes {
		proof := change.Repair
		if proof == nil || len(proof.Edits) != 0 || (!proof.Retain && len(proof.TargetEdits) == 0) {
			return nil, errors.New("reduction cannot drop ordinary changes or exact edits")
		}
		for _, edit := range proof.TargetEdits {
			selections = append(selections, RepairSelection{Path: change.Path, TargetID: edit.TargetID})
		}
	}
	if len(selections) < 2 || len(selections) > MaxRepairReductionEdits {
		return nil, errors.New("reduction requires two to four named edits")
	}
	return selections, nil
}

// ReduceRepairResult retains one model-authored edit, restoring other proposed
// draft paths mechanically to their historical content. It produces UNVERIFIED
// source with zero new inference usage. No checks, review or writes occur here.
func ReduceRepairResult(task Task, full Result, nextTaskID string, selected RepairSelection) (Task, Result, error) {
	selections, err := RepairSelections(task, full)
	if err != nil {
		return Task{}, Result{}, err
	}
	if strings.TrimSpace(nextTaskID) == "" || !slices.Contains(selections, selected) {
		return Task{}, Result{}, errors.New("reduction must select an existing named edit and bound task")
	}
	task.ID = nextTaskID
	task, canonical, _, err := FreezeTask(task)
	if err != nil {
		return Task{}, Result{}, err
	}
	out := Result{Status: "proposed_unverified", TaskID: task.ID, InputSHA256: Hash(string(canonical)),
		Summary:           "Host single-edit reduction of complete native proposal " + full.ProposalID,
		Risks:             []string{"Unverified reduction; all mandatory checks and post-test review remain required."},
		RecommendedChecks: slices.Clone(full.RecommendedChecks)}
	for _, change := range full.Changes {
		var derived Change
		if change.Path == selected.Path {
			var edit DraftTargetEdit
			for _, candidate := range change.Repair.TargetEdits {
				if candidate.TargetID == selected.TargetID {
					edit = candidate
					break
				}
			}
			derived, err = materializeRepairTargetEdits(task, change.Path, []DraftTargetEdit{edit})
		} else {
			retain := true
			derived, err = materializeRepairDraft(task, change.Path, nil, nil, &retain)
		}
		if err != nil {
			return Task{}, Result{}, err
		}
		out.Changes = append(out.Changes, derived)
	}
	out.ProposalID = proposalID(out.InputSHA256, out)
	if err = ValidateResult(task, out); err != nil {
		return Task{}, Result{}, err
	}
	return task, out, nil
}

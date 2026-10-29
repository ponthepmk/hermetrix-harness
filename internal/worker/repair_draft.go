package worker

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

const MaxRepairDraftFiles = 2
const MaxRepairDraftBytes = 8192

func validateRepairDrafts(task Task) error {
	if len(task.RepairDrafts) == 0 {
		return nil
	}
	if len(task.RepairDrafts) > MaxRepairDraftFiles || len(task.WritablePaths) == 0 {
		return errors.New("repair drafts require bounded, explicit writable scope")
	}
	remaining := MaxRepairDraftBytes
	for _, draft := range task.RepairDrafts {
		if len(draft.Content) > remaining {
			return errors.New("repair draft content exceeds shared byte budget")
		}
		remaining -= len(draft.Content)
	}
	if err := validateAbsentFiles(task); err != nil {
		return err
	}
	for path, draft := range task.RepairDrafts {
		current, selected := task.Files[path]
		_, partial := task.FileViews[path]
		if !selected || !writablePathAllowed(task, path) || partial || !validDraftPath(path) {
			return errors.New("repair draft is outside selected whole-file writable scope")
		}
		if len(draft.SourceArtifactID) > 256 || strings.TrimSpace(draft.SourceArtifactID) == "" ||
			!validDraftSHA(draft.ProposalID) || !validDraftSHA(draft.SourceArtifactSHA256) ||
			!validDraftSHA(draft.InputSHA256) || !validDraftSHA(draft.SourceSHA256) {
			return errors.New("repair draft provenance is malformed")
		}
		if draft.Content == "" || !utf8.ValidString(draft.Content) ||
			Hash(draft.Content) != draft.SourceSHA256 || draft.Content == current {
			return errors.New("repair draft content does not match its historical snapshot")
		}
	}
	return nil
}

func validDraftSHA(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func validDraftPath(path string) bool {
	if strings.ContainsAny(path, "\\:") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func materializeRepairDraft(task Task, path string, content *string, edits []TextEdit, retain *bool) (Change, error) {
	if err := validateRepairDrafts(task); err != nil {
		return Change{}, err
	}
	draft, ok := task.RepairDrafts[path]
	if !ok || content != nil {
		return Change{}, errors.New("repair draft paths accept exact edits or explicit retention only")
	}
	proof := &DraftEditProof{SourceSHA256: draft.SourceSHA256}
	output := draft.Content
	if retain != nil {
		if !*retain || len(edits) != 0 {
			return Change{}, errors.New("draft retention must be true and exclusive of edits")
		}
		proof.Retain = true
	} else {
		if len(edits) == 0 {
			return Change{}, errors.New("draft repair requires exact edits")
		}
		var err error
		output, _, err = materializeChange(draft.Content, nil, edits)
		if err != nil {
			return Change{}, err
		}
		proof.Edits = slices.Clone(edits)
	}
	return repairDraftOutputChange(task, path, output, proof)
}

func repairDraftOutputChange(task Task, path, output string, proof *DraftEditProof) (Change, error) {
	if err := validateRepairSyntax(task, path, output); err != nil {
		return Change{}, err
	}
	before := task.Files[path]
	change := Change{Path: path, BeforeSHA256: Hash(before), Content: output, Strategy: "replace_file", Repair: proof}
	if absentFile(task, path) {
		change.BeforeSHA256, change.Strategy = "absent", "create_file"
	} else if output == before {
		return Change{}, errors.New("draft repair does not change the current snapshot")
	}
	return change, nil
}

func validateRepairDraftChange(task Task, change Change) error {
	draft, ok := task.RepairDrafts[change.Path]
	if !ok || change.Repair == nil || change.Repair.SourceSHA256 != draft.SourceSHA256 || len(change.Edits) != 0 {
		return errors.New("draft proof does not match frozen historical input")
	}
	if len(change.Repair.TargetEdits) > 0 {
		if len(change.Repair.Edits) != 0 || change.Repair.Retain {
			return errors.New("target proof cannot mix source forms")
		}
		derived, err := materializeRepairTargetEdits(task, change.Path, change.Repair.TargetEdits)
		if err != nil {
			return err
		}
		if derived.BeforeSHA256 != change.BeforeSHA256 || derived.Strategy != change.Strategy || derived.Content != change.Content {
			return errors.New("target proposal cannot be rederived from frozen proof")
		}
		return nil
	}
	var retain *bool
	if change.Repair.Retain {
		value := true
		retain = &value
	}
	derived, err := materializeRepairDraft(task, change.Path, nil, change.Repair.Edits, retain)
	if err != nil {
		return err
	}
	if derived.BeforeSHA256 != change.BeforeSHA256 || derived.Strategy != change.Strategy || derived.Content != change.Content {
		return errors.New("draft proposal cannot be rederived from its frozen proof")
	}
	return nil
}

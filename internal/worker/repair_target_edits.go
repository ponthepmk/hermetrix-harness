package worker

import (
	"errors"
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

func materializeRepairTargetEdits(task Task, path string, edits []DraftTargetEdit) (Change, error) {
	if task.RepairTargetVersion != RepairTargetVersion || len(edits) == 0 || len(edits) > maxRepairTargets {
		return Change{}, errors.New("named target edits require the frozen recipe and bounded edits")
	}
	remaining := maxWorkerRawProposalBytes
	if err := consumeRawBudget(&remaining, edits); err != nil {
		return Change{}, err
	}
	catalog, err := buildRepairTargets(task)
	if err != nil {
		return Change{}, err
	}
	draft, ok := task.RepairDrafts[path]
	if !ok {
		return Change{}, errors.New("target edit outside historical draft")
	}
	targets := map[string]RepairTarget{}
	for _, target := range catalog[path] {
		targets[target.ID] = target
	}
	type replacement struct {
		target RepairTarget
		new    string
	}
	resolved := make([]replacement, 0, len(edits))
	seen := map[string]bool{}
	for _, edit := range edits {
		target, found := targets[edit.TargetID]
		if !found || seen[edit.TargetID] || !utf8.ValidString(edit.New) {
			return Change{}, errors.New("unknown, duplicate or invalid target edit")
		}
		if edit.New == draft.Content[target.Start:target.End] {
			return Change{}, errors.New("target edit leaves the historical span unchanged")
		}
		seen[edit.TargetID] = true
		resolved = append(resolved, replacement{target, edit.New})
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].target.Start < resolved[j].target.Start })
	var output strings.Builder
	cursor := 0
	for _, edit := range resolved {
		if edit.target.Start < cursor {
			return Change{}, errors.New("target edit spans overlap")
		}
		output.WriteString(draft.Content[cursor:edit.target.Start])
		output.WriteString(edit.new)
		cursor = edit.target.End
	}
	output.WriteString(draft.Content[cursor:])
	return repairDraftOutputChange(task, path, output.String(), &DraftEditProof{SourceSHA256: draft.SourceSHA256, TargetEdits: slices.Clone(edits)})
}

// Parsing detects a malformed span replacement, not whether the repair is
// correct. The mandatory compiler and behavioral checks still decide that.
// Only the new recipe enforces this invariant; historical proposals retain
// their replay semantics and malformed input remains repairable.
func validateRepairSyntax(task Task, path, output string) error {
	if task.RepairTargetVersion != RepairTargetVersion || !strings.HasSuffix(path, ".go") {
		return nil
	}
	draft := task.RepairDrafts[path]
	if _, err := parser.ParseFile(token.NewFileSet(), path, draft.Content, parser.SkipObjectResolution); err != nil {
		return nil
	}
	_, err := parser.ParseFile(token.NewFileSet(), path, output, parser.SkipObjectResolution)
	if err == nil {
		return nil
	}
	// Parser messages can quote source. Return positions and fixed guidance only.
	var list scanner.ErrorList
	if errors.As(err, &list) && len(list) > 0 {
		return fmt.Errorf("repair introduces Go syntax errors at line %d column %d; replace only the selected span or retain an unaffected companion", list[0].Pos.Line, list[0].Pos.Column)
	}
	return errors.New("repair introduces Go syntax errors; replace only the selected span or retain an unaffected companion")
}

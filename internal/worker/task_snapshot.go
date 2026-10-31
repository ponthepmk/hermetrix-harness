// Package worker provides a proposal-only external model boundary. It never
// reads a workspace, executes commands, or applies model-generated changes.
package worker

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
)

// prepareWorkerTask returns an independent source snapshot, the canonical full
// task JSON, the provider wire JSON, and any error. The snapshot is a deep copy
// of the supplied task; the wire copy is a bounded copy with only the files
// covered by validated FileViews removed from its Files map.
func prepareWorkerTask(task Task) (Task, []byte, []byte, error) {
	if err := validateWorkerTaskResources(task); err != nil {
		return Task{}, nil, nil, err
	}
	if err := validateRepairDrafts(task); err != nil {
		return Task{}, nil, nil, err
	}

	snapshot := Task{
		ID:                  task.ID,
		Kind:                task.Kind,
		Brief:               task.Brief,
		AcceptanceCriteria:  slices.Clone(task.AcceptanceCriteria),
		Constraints:         slices.Clone(task.Constraints),
		RecommendedChecks:   slices.Clone(task.RecommendedChecks),
		ProjectBrainRefs:    slices.Clone(task.ProjectBrainRefs),
		SkillRefs:           slices.Clone(task.SkillRefs),
		Files:               maps.Clone(task.Files),
		FileViews:           maps.Clone(task.FileViews),
		WritablePaths:       slices.Clone(task.WritablePaths),
		AbsentFiles:         slices.Clone(task.AbsentFiles),
		RepairDrafts:        maps.Clone(task.RepairDrafts),
		RepairTargetVersion: task.RepairTargetVersion,
		FailureEvidence:     slices.Clone(task.FailureEvidence),
		MaxOutputTokens:     task.MaxOutputTokens,
	}

	for path, views := range snapshot.FileViews {
		if _, ok := snapshot.Files[path]; !ok {
			return Task{}, nil, nil, errors.New("file view references missing file")
		}
		if absentFile(snapshot, path) {
			return Task{}, nil, nil, errors.New("file view references absent file")
		}
		snapshot.FileViews[path] = slices.Clone(views)
		if err := validateBoundedViewSet(path, snapshot.Files[path], Hash(snapshot.Files[path]), snapshot.FileViews[path]); err != nil {
			return Task{}, nil, nil, err
		}
	}

	wire := Task{
		ID:                  snapshot.ID,
		Kind:                snapshot.Kind,
		Brief:               snapshot.Brief,
		AcceptanceCriteria:  slices.Clone(snapshot.AcceptanceCriteria),
		Constraints:         slices.Clone(snapshot.Constraints),
		RecommendedChecks:   slices.Clone(snapshot.RecommendedChecks),
		ProjectBrainRefs:    slices.Clone(snapshot.ProjectBrainRefs),
		SkillRefs:           slices.Clone(snapshot.SkillRefs),
		Files:               maps.Clone(snapshot.Files),
		FileViews:           maps.Clone(snapshot.FileViews),
		WritablePaths:       slices.Clone(snapshot.WritablePaths),
		AbsentFiles:         slices.Clone(snapshot.AbsentFiles),
		RepairDrafts:        maps.Clone(snapshot.RepairDrafts),
		RepairTargetVersion: snapshot.RepairTargetVersion,
		FailureEvidence:     slices.Clone(snapshot.FailureEvidence),
		MaxOutputTokens:     snapshot.MaxOutputTokens,
	}

	for path := range wire.FileViews {
		delete(wire.Files, path)
	}

	canonical, err := json.Marshal(snapshot)
	if err != nil {
		return Task{}, nil, nil, err
	}
	if len(canonical) > maxWorkerCanonicalBytes {
		return Task{}, nil, nil, errors.New("canonical task exceeds size limit")
	}

	wireJSON, err := json.Marshal(wire)
	if err == nil && snapshot.RepairTargetVersion != 0 {
		targets, targetErr := buildRepairTargets(snapshot)
		if targetErr != nil {
			return Task{}, nil, nil, targetErr
		}
		wireJSON, err = json.Marshal(struct {
			Task
			Targets       map[string][]RepairTarget `json:"repair_targets"`
			RequiredPaths []string                  `json:"repair_required_paths,omitempty"`
		}{wire, targets, requiredRepairPaths(snapshot)})
	}
	if err != nil {
		return Task{}, nil, nil, err
	}
	if len(wireJSON) > maxWorkerWireBytes {
		return Task{}, nil, nil, errors.New("task exceeds 128 KiB")
	}

	return snapshot, canonical, wireJSON, nil
}

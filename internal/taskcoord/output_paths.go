package taskcoord

import (
	"fmt"

	"hermetrix-harness/internal/taskscope"
	"hermetrix-harness/internal/worker"
)

// mergeStepOutputFiles merges explicit required step outputs with selected
// optional context files. When candidates are supplied it validates every
// path against the host manifest before merging. It never mutates its
// arguments and preserves all required outputs.
func mergeStepOutputFiles(outputs, selected []string, candidates []worker.FileCandidate) ([]string, error) {
	if len(outputs) == 0 {
		if selected == nil {
			return nil, nil
		}
		out := make([]string, len(selected))
		copy(out, selected)
		return out, nil
	}

	if err := taskscope.ValidateOutputPaths(outputs, outputs); err != nil {
		return nil, fmt.Errorf("taskcoord: required outputs: %w", err)
	}
	if len(selected) > 32 {
		return nil, fmt.Errorf("taskcoord: selected file count exceeds 32")
	}
	if err := taskscope.ValidateWritablePaths(selected); err != nil {
		return nil, fmt.Errorf("taskcoord: selected files: %w", err)
	}

	if candidates != nil {
		known := make(map[string]struct{}, len(candidates))
		for _, c := range candidates {
			if c.Path == "" {
				return nil, fmt.Errorf("taskcoord: host candidate has empty path")
			}
			if err := taskscope.ValidateWritablePaths([]string{c.Path}); err != nil {
				return nil, fmt.Errorf("taskcoord: host candidate path: %w", err)
			}
			if c.Bytes < 0 {
				return nil, fmt.Errorf("taskcoord: host candidate %q has negative bytes", c.Path)
			}
			if c.Absent && c.Bytes != 0 {
				return nil, fmt.Errorf("taskcoord: host candidate %q is absent but has non-zero bytes", c.Path)
			}
			if _, dup := known[c.Path]; dup {
				return nil, fmt.Errorf("taskcoord: duplicate host candidate %q", c.Path)
			}
			known[c.Path] = struct{}{}
		}
		for _, p := range outputs {
			if _, ok := known[p]; !ok {
				return nil, fmt.Errorf("taskcoord: required output %q not in host manifest; replan required", p)
			}
		}
		for _, p := range selected {
			if _, ok := known[p]; !ok {
				return nil, fmt.Errorf("taskcoord: selected file %q not in host manifest", p)
			}
		}
	}

	result := make([]string, 0, len(outputs)+len(selected))
	included := make(map[string]struct{}, len(outputs)+len(selected))
	for _, p := range outputs {
		result = append(result, p)
		included[p] = struct{}{}
	}
	for _, p := range selected {
		if len(result) >= 32 {
			break
		}
		if _, dup := included[p]; dup {
			continue
		}
		result = append(result, p)
		included[p] = struct{}{}
	}
	return result, nil
}

// requireAbsentStepOutputs verifies that every required output present in the
// host-observed absent list has a corresponding change whose path matches and
// whose BeforeSHA256 is the sentinel "absent". It is called after
// worker.ValidateResult and performs no filesystem I/O.
func requireAbsentStepOutputs(outputs, absent []string, changes []worker.Change) error {
	if len(outputs) == 0 {
		return nil
	}
	absentSet := make(map[string]struct{}, len(absent))
	for _, p := range absent {
		absentSet[p] = struct{}{}
	}
	byPath := make(map[string]worker.Change, len(changes))
	for _, c := range changes {
		byPath[c.Path] = c
	}
	for _, p := range outputs {
		if _, ok := absentSet[p]; !ok {
			continue
		}
		ch, ok := byPath[p]
		if !ok {
			return fmt.Errorf("taskcoord: required absent output %q has no change; incomplete output", p)
		}
		if ch.BeforeSHA256 != "absent" {
			return fmt.Errorf("taskcoord: required absent output %q BeforeSHA256 is not \"absent\"; incomplete output", p)
		}
	}
	return nil
}

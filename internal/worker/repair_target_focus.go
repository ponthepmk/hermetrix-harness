package worker

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type focusedRepairTarget struct {
	path   string
	target RepairTarget
}

// Locations in untrusted diagnostics are prioritization hints only. They cannot
// add source, expand write authority or establish that a span caused a failure.
func reportedRepairLines(task Task, path string, lineCount int) []int {
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	pattern := regexp.MustCompile(`(?i)` + strings.Join(parts, `[/\\]+`) + `:([0-9]+)(?::[0-9]+)?`)
	seen := map[int]bool{}
	var lines []int
	for _, evidence := range task.FailureEvidence {
		for _, match := range pattern.FindAllStringSubmatch(evidence.Output, 8) {
			line, err := strconv.Atoi(match[1])
			if err == nil && line > 0 && line <= lineCount && !seen[line] {
				seen[line] = true
				lines = append(lines, line)
				if len(lines) == 8 {
					return lines
				}
			}
		}
	}
	return lines
}

func focusedRepairOrder(task Task, byPath map[string][]RepairTarget) []focusedRepairTarget {
	paths := make([]string, 0, len(byPath))
	for path, targets := range byPath {
		paths = append(paths, path)
		content := task.RepairDrafts[path].Content
		starts := []int{0}
		for i := 0; i < len(content); i++ {
			if content[i] == '\n' {
				starts = append(starts, i+1)
			}
		}
		lines := reportedRepairLines(task, path, len(starts))
		distance := func(target RepairTarget) int {
			first := sort.SearchInts(starts, target.Start+1)
			last := sort.SearchInts(starts, target.End)
			best := 5 // Only a bounded neighborhood receives priority.
			for _, line := range lines {
				d := 0
				if line < first {
					d = first - line
				} else if line > last {
					d = line - last
				}
				best = min(best, d)
			}
			return best
		}
		// Stable ordering preserves the existing syntax/offset order among spans
		// with equal diagnostic distance, including when no locations match.
		sort.SliceStable(targets, func(i, j int) bool { return distance(targets[i]) < distance(targets[j]) })
		byPath[path] = targets
	}
	sort.Strings(paths)
	var ordered []focusedRepairTarget
	for round := 0; ; round++ {
		added := false
		for _, path := range paths {
			if round < len(byPath[path]) {
				ordered = append(ordered, focusedRepairTarget{path, byPath[path][round]})
				added = true
			}
		}
		if !added {
			return ordered
		}
	}
}

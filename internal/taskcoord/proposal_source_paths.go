package taskcoord

import (
	"hermetrix-harness/internal/worker"
	"sort"
)

func proposalSourcePaths(task worker.Task) []string {
	absent := make(map[string]struct{}, len(task.AbsentFiles))
	for _, name := range task.AbsentFiles {
		absent[name] = struct{}{}
	}
	var paths []string
	for path, source := range task.Files {
		if path == "" || source == "" {
			continue
		}
		if _, ok := absent[path]; ok {
			continue
		}
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		a := len(task.Files[paths[i]])
		b := len(task.Files[paths[j]])
		if a != b {
			return a > b
		}
		return paths[i] < paths[j]
	})
	return paths
}

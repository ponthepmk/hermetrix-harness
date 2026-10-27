package worker

import "sort"

func proposalSchemaForTask(task Task) map[string]any {
	schema := proposalSchema()
	allowedPaths := []string{}
	for path := range task.Files {
		if writablePathAllowed(task, path) {
			allowedPaths = append(allowedPaths, path)
		}
	}
	sort.Strings(allowedPaths)
	changes := schema["properties"].(map[string]any)["changes"].(map[string]any)
	changes["maxItems"] = len(allowedPaths)
	changes["items"].(map[string]any)["properties"].(map[string]any)["path"].(map[string]any)["enum"] = allowedPaths
	return schema
}

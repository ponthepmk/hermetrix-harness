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
	if len(task.RepairDrafts) > 0 {
		draftPaths := make([]string, 0, len(task.RepairDrafts))
		ordinaryPaths := []string{}
		for _, path := range allowedPaths {
			if _, draft := task.RepairDrafts[path]; !draft {
				ordinaryPaths = append(ordinaryPaths, path)
			}
		}
		for path := range task.RepairDrafts {
			draftPaths = append(draftPaths, path)
		}
		sort.Strings(draftPaths)
		item := changes["items"].(map[string]any)
		properties := item["properties"].(map[string]any)
		properties["retain_draft"] = map[string]any{"type": "boolean", "const": true}
		variants := []map[string]any{}
		form := func(name string, paths []string) map[string]any {
			return map[string]any{
				"type": "object", "required": []string{"path", name}, "additionalProperties": false,
				"properties": map[string]any{"path": map[string]any{"type": "string", "enum": paths}, name: properties[name]},
			}
		}
		if len(ordinaryPaths) > 0 {
			variants = append(variants, form("content", ordinaryPaths))
		} else {
			delete(properties, "content")
		}
		variants = append(variants, form("edits", allowedPaths), form("retain_draft", draftPaths))
		item["oneOf"] = variants
	}
	return schema
}

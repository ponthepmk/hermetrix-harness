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
	if required := requiredRepairPaths(task); len(required) > 1 {
		changes["minItems"] = len(required)
	}
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
		if namedRepairRecipe(task.RepairTargetVersion) {
			properties["retain_draft"].(map[string]any)["description"] = "Restore the complete unchanged historical companion. Use this when its code is unaffected by the observed failure."
			catalog, err := buildRepairTargets(task)
			if err == nil {
				ids := []string{}
				targetPaths := []string{}
				for path, targets := range catalog {
					targetPaths = append(targetPaths, path)
					for _, target := range targets {
						ids = append(ids, target.ID)
					}
				}
				sort.Strings(ids)
				sort.Strings(targetPaths)
				properties["target_edits"] = map[string]any{"type": "array", "description": "Localized historical span replacements. An unchanged companion uses retain_draft instead.", "minItems": 1, "maxItems": maxRepairTargets, "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"target_id", "new"}, "properties": map[string]any{"target_id": map[string]any{"type": "string", "enum": ids, "description": "Copy the ID matching the intended path and symbol in repair_targets."}, "new": map[string]any{"type": "string", "description": "Replacement for exactly that complete target span, not the entire file. A keyed value needs only its expression."}}}}
				variants = append(variants, form("target_edits", targetPaths))
				if task.RepairTargetVersion == AtomicRepairTargetVersion {
					schema["properties"].(map[string]any)["repair"] = map[string]any{
						"type": "object", "additionalProperties": false, "required": []string{"path", "target_id", "new"},
						"description": "One localized repair. The host restores every missing historical companion unchanged. Use changes instead only when multiple edits or ordinary file changes are needed.",
						"properties": map[string]any{
							"path":      map[string]any{"type": "string", "enum": targetPaths},
							"target_id": map[string]any{"type": "string", "enum": ids},
							"new":       map[string]any{"type": "string", "description": "Replacement for the complete selected span, not the file or preview."},
						},
					}
					schema["required"] = []string{}
					schema["oneOf"] = []map[string]any{{"required": []string{"repair"}, "not": map[string]any{"required": []string{"changes"}}}, {"required": []string{"changes"}, "not": map[string]any{"required": []string{"repair"}}}}
				}
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

package worker

import (
	"encoding/json"
	"os"
	"testing"

	"hermetrix-harness/internal/providers"
)

func decorateScopeGolden(t *testing.T, name string, request *providers.ChatRequest) {
	t.Helper()
	data, err := os.ReadFile("proposal_schema_scope_cases.json")
	if err != nil {
		t.Fatalf("read golden data: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("decode golden data: %v", err)
	}
	rows, ok := root["golden"].([]any)
	if !ok || len(rows) != 3 {
		t.Fatalf("golden data must contain exactly 3 rows")
	}
	var row map[string]any
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("golden row must be an object")
		}
		if m["name"] == name {
			row = m
			break
		}
	}
	if row == nil {
		t.Fatalf("golden row %q not found", name)
	}
	allowed, ok := row["allowed"].([]any)
	if !ok || len(allowed) == 0 {
		t.Fatalf("golden row %q missing allowed paths", name)
	}
	maxChanges, ok := row["max_changes"].(float64)
	if !ok {
		t.Fatalf("golden row %q missing max_changes", name)
	}
	existingDesc, ok := root["tool_description"].(string)
	if !ok || existingDesc == "" {
		t.Fatalf("golden row %q missing nonempty tool_description", name)
	}
	request.Tools[0].Function.Description = existingDesc

	params := request.Tools[0].Function.Parameters
	changes, ok := params["properties"].(map[string]any)["changes"].(map[string]any)
	if !ok {
		t.Fatalf("request schema missing changes")
	}
	if _, present := changes["maxItems"]; present {
		t.Fatalf("old frozen changes schema must not have maxItems")
	}
	if _, present := changes["items"].(map[string]any)["properties"].(map[string]any)["path"].(map[string]any)["enum"]; present {
		t.Fatalf("old frozen path schema must not have enum")
	}
	changes["maxItems"] = int(maxChanges)
	changes["items"].(map[string]any)["properties"].(map[string]any)["path"].(map[string]any)["enum"] = allowed
	changes["items"].(map[string]any)["oneOf"] = []map[string]any{{"required": []string{"content"}}, {"required": []string{"edits"}}}
}

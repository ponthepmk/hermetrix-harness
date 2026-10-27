package taskcoord

import (
	"encoding/json"

	"hermetrix-harness/internal/providers"
)

func boundedGoalChanges() (providers.Completion, error) {
	body := map[string]any{
		"summary":            "correct arithmetic",
		"recommended_checks": []string{"node test.js"},
		"changes": []any{
			map[string]any{
				"path": "sum.js",
				"edits": []any{
					map[string]any{"old": "a-b", "new": "a+b"},
				},
			},
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		return providers.Completion{}, err
	}
	return providers.Completion{
		FinishReason: "tool_calls",
		ToolCalls:    []providers.ToolCall{{Name: "submit_changes", Arguments: string(data)}},
	}, nil
}

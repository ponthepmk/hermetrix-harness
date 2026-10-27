package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"

	"hermetrix-harness/internal/providers"
)

func boundedGoalPlan(ctx context.Context, base *goalAdapter, p providers.Profile, key string, r providers.ChatRequest, emit func(providers.Delta) error) (providers.Completion, error) {
	completion, err := base.StreamChat(ctx, p, key, r, emit)
	if err != nil {
		return completion, err
	}
	if len(completion.ToolCalls) != 1 {
		return completion, fmt.Errorf("expected exactly one tool call")
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(completion.ToolCalls[0].Arguments), &args); err != nil {
		return completion, err
	}
	steps, ok := args["steps"].([]any)
	if !ok || len(steps) == 0 {
		return completion, fmt.Errorf("steps must be a nonempty array")
	}
	first, ok := steps[0].(map[string]any)
	if !ok {
		return completion, fmt.Errorf("first step must be an object")
	}
	instructions, ok := first["instructions"].(string)
	if !ok || instructions == "" {
		return completion, fmt.Errorf("first step instructions must be a string")
	}
	first["instructions"] = "Replace the exact quoted expression `a-b` in sum.js with addition, preserving test.js"
	args["steps"] = steps
	marshaled, err := json.Marshal(args)
	if err != nil {
		return completion, err
	}
	completion.ToolCalls[0].Arguments = string(marshaled)
	return completion, nil
}

package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
)

type outputPathsAdapter struct {
	base *goalAdapter
}

func (a *outputPathsAdapter) StreamChat(ctx context.Context, profile providers.Profile, model string, r providers.ChatRequest, emit func(providers.Delta) error) (providers.Completion, error) {
	completion, err := a.base.StreamChat(ctx, profile, model, r, emit)
	if err != nil {
		return completion, err
	}
	if len(r.Tools) == 0 || r.Tools[0].Function.Name != "submit_plan" || len(completion.ToolCalls) == 0 {
		return completion, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(completion.ToolCalls[0].Arguments), &payload); err != nil {
		return completion, err
	}
	steps, ok := payload["steps"].([]any)
	if !ok || len(steps) == 0 {
		return completion, fmt.Errorf("unexpected plan shape")
	}
	step, ok := steps[0].(map[string]any)
	if !ok {
		return completion, fmt.Errorf("unexpected step shape")
	}
	step["output_paths"] = []string{"sum.js"}
	body, err := json.Marshal(payload)
	if err != nil {
		return completion, err
	}
	completion.ToolCalls[0].Arguments = string(body)
	return completion, nil
}

func TestAutoPlanPropagatesOutputPaths(t *testing.T) {
	ctx := context.Background()
	c, ts, task, input, _, db := newGoalFixtureWithStore(t, &goalAdapter{}, taskengine.GoalContract{WritablePaths: []string{"sum.js", "later.js"}})

	base := &goalAdapter{}
	c.providers = providers.NewService(db, &outputPathsAdapter{base: base})

	out, err := c.AutoPlan(ctx, AutoPlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, ProviderID: input.ProviderID, Actor: "owner"})
	if err != nil {
		t.Fatalf("AutoPlan: %v", err)
	}
	if len(out.Result.Steps) != 1 {
		t.Fatalf("expected 1 planner step, got %d", len(out.Result.Steps))
	}
	if !reflect.DeepEqual(out.Result.Steps[0].OutputPaths, []string{"sum.js"}) {
		t.Fatalf("planner transport lost output paths: got %v", out.Result.Steps[0].OutputPaths)
	}
	if len(out.Task.Plan.Steps) != 1 {
		t.Fatalf("expected 1 persisted step, got %d", len(out.Task.Plan.Steps))
	}
	if !reflect.DeepEqual(out.Task.Plan.Steps[0].OutputPaths, []string{"sum.js"}) {
		t.Fatalf("LOST OUTPUTS: coordinator dropped output paths: got %v", out.Task.Plan.Steps[0].OutputPaths)
	}
	persisted, err := ts.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("ts.Get: %v", err)
	}
	if len(persisted.Plan.Steps) != 1 {
		t.Fatalf("expected 1 store-persisted step, got %d", len(persisted.Plan.Steps))
	}
	if !reflect.DeepEqual(persisted.Plan.Steps[0].OutputPaths, []string{"sum.js"}) {
		t.Fatalf("LOST OUTPUTS: store-persisted plan dropped output paths: got %v", persisted.Plan.Steps[0].OutputPaths)
	}
}

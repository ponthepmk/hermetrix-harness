package worker

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
)

func TestPlannerReturnsBoundedStructuredPlan(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	completion := providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_plan", Arguments: `{
        "reason":"implement then verify","steps":[{"key":"fix","title":"Fix","instructions":"Implement AC-1","requirement_ids":["AC-1"],"dependencies":[],"checks":["go test ./..."],"effect_scope":["provider.select_files","provider.propose","workspace.apply","workspace.run","provider.review"]}]}`}}}
	service := providers.NewService(dataStore, reviewAdapter{completion: completion})
	profile, err := service.Save(ctx, providers.SaveInput{Name: "planner", BaseURL: "https://plan.example/v1", Model: "plan-model", ContextWindow: 98304, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	result, err := PlanWithProviderService(ctx, service, profile, PlanTask{TaskID: "task-1", Objective: "fix", OriginalRequest: "fix it", Criteria: []string{"AC-1: passes"}}, Options{})
	if err != nil || len(result.Steps) != 1 || result.Steps[0].Key != "fix" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPlannerRejectsUnknownEffect(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	completion := providers.Completion{Content: `{"reason":"unsafe","steps":[{"key":"x","title":"X","instructions":"X","requirement_ids":["AC-1"],"dependencies":[],"checks":["x"],"effect_scope":["root.shell"]}]}`}
	service := providers.NewService(dataStore, reviewAdapter{completion: completion})
	profile, err := service.Save(ctx, providers.SaveInput{Name: "planner", BaseURL: "https://plan.example/v1", Model: "plan-model", ContextWindow: 98304, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = PlanWithProviderService(ctx, service, profile, PlanTask{TaskID: "task-1", Objective: "fix", OriginalRequest: "fix", Criteria: []string{"AC-1"}}, Options{}); err == nil {
		t.Fatal("planner accepted an unsupported effect")
	}
}

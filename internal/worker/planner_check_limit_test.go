package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
)

func runCheckLimit(t *testing.T, n int) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()

	checks := make([]string, n)
	for i := range checks {
		checks[i] = fmt.Sprintf("go test ./... -run TestCheck_%d", i)
	}

	steps := []map[string]any{{
		"key":              "fix",
		"title":            "Fix",
		"workspace_change": "Correct the implementation for AC-1",
		"instructions":     "Implement AC-1",
		"requirement_ids":  []string{"AC-1"},
		"dependencies":     []string{},
		"checks":           checks,
		"effect_scope":     []string{"provider.select_files", "provider.propose", "workspace.apply", "workspace.run", "provider.review"},
	}}

	response := map[string]any{"reason": "implement then verify", "steps": steps}
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}

	service := providers.NewService(dataStore, reviewAdapter{completion: providers.Completion{Content: string(body)}})
	profile, err := service.Save(ctx, providers.SaveInput{Name: "planner", BaseURL: "https://plan.example/v1", Model: "plan-model", ContextWindow: 98304, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}

	result, err := PlanWithProviderService(ctx, service, profile, PlanTask{TaskID: "task-1", Objective: "fix", OriginalRequest: "fix it", Criteria: []string{"AC-1: passes"}}, Options{})
	if n <= 32 {
		if err != nil {
			t.Fatalf("n=%d: expected success, got %v", n, err)
		}
		if len(result.Steps) != 1 || len(result.Steps[0].Checks) != n {
			t.Fatalf("n=%d: steps=%d checks=%d", n, len(result.Steps), len(result.Steps[0].Checks))
		}
		return
	}
	if err == nil {
		t.Fatalf("n=%d: expected over-limit error, got success", n)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "over-limit") {
		t.Fatalf("n=%d: error does not contain over-limit: %v", n, err)
	}
}

func TestPlannerCheckLimit(t *testing.T) {
	for _, n := range []int{1, 32, 33, 77} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			runCheckLimit(t, n)
		})
	}
}

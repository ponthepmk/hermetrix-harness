package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
)

func TestPlannerDiagnosticsShape(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := providers.NewService(dataStore, reviewAdapter{})
	profile, err := service.Save(ctx, providers.SaveInput{
		Name: "planner", BaseURL: "https://planner.example/v1", Model: "test", ContextWindow: 98304, MaxOutputTokens: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	task := PlanTask{
		TaskID: "task-diag", Objective: "Build feature", OriginalRequest: "Build the feature",
		Criteria: []string{"AC-1: feature works"}, WritablePaths: []string{"internal/feature"},
	}
	cases := []struct {
		name       string
		completion providers.Completion
		wantDiag   []string
		sentinels  []string
	}{
		{
			name:       "zero-tools-reasoning",
			completion: providers.Completion{Reasoning: "reasoning-sentinel-zero"},
			wantDiag:   []string{"tool_calls=0", "content_present=false", "sole_name_matches=false", "reasoning_present=true"},
			sentinels:  []string{"reasoning-sentinel-zero"},
		},
		{
			name: "unexpected-name",
			completion: providers.Completion{
				Content:   "content-sentinel-one",
				Reasoning: "reasoning-sentinel-one",
				ToolCalls: []providers.ToolCall{{Name: "unexpected-tool", Arguments: "args-sentinel-one credential-sentinel-secret", ID: "id-sentinel-one"}},
			},
			wantDiag:  []string{"tool_calls=1", "content_present=true", "sole_name_matches=false", "reasoning_present=true"},
			sentinels: []string{"content-sentinel-one", "unexpected-tool", "args-sentinel-one", "credential-sentinel-secret", "id-sentinel-one", "reasoning-sentinel-one"},
		},
		{
			name: "two-raw-tools",
			completion: providers.Completion{ToolCalls: []providers.ToolCall{
				{Name: "raw-tool-a", Arguments: "raw-args-a", ID: "raw-id-a"},
				{Name: "raw-tool-b", Arguments: "raw-args-b", ID: "raw-id-b"},
			}},
			wantDiag:  []string{"tool_calls=2", "content_present=false", "sole_name_matches=false", "reasoning_present=false"},
			sentinels: []string{"raw-tool-a", "raw-args-a", "raw-id-a", "raw-tool-b", "raw-args-b", "raw-id-b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := providers.NewService(dataStore, reviewAdapter{completion: tc.completion})
			_, err := PlanWithProviderService(ctx, svc, profile, task, Options{})
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "expected one complete submit_plan call") {
				t.Fatalf("prefix missing: %v", err)
			}
			for _, want := range tc.wantDiag {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("diag %q missing: %v", want, err)
				}
			}
			errJSON, _ := json.Marshal(err.Error())
			for _, s := range tc.sentinels {
				if strings.Contains(err.Error(), s) {
					t.Errorf("sentinel %q leaked in error", s)
				}
				if strings.Contains(string(errJSON), s) {
					t.Errorf("sentinel %q leaked in json", s)
				}
			}
		})
	}
}

func TestPlannerDiagnosticsLengthReason(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := providers.NewService(dataStore, reviewAdapter{
		completion: providers.Completion{FinishReason: "length", Usage: providers.Usage{CompletionTokens: 4096}},
	})
	profile, err := service.Save(ctx, providers.SaveInput{
		Name: "planner", BaseURL: "https://planner.example/v1", Model: "test", ContextWindow: 98304, MaxOutputTokens: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	task := PlanTask{
		TaskID: "task-diag", Objective: "Build feature", OriginalRequest: "Build the feature",
		Criteria: []string{"AC-1: feature works"}, WritablePaths: []string{"internal/feature"},
	}
	_, err = PlanWithProviderService(ctx, service, profile, task, Options{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bounded output limit") {
		t.Fatalf("output-limit diagnostic missing: %v", err)
	}
	if strings.Contains(err.Error(), "tool_calls=") {
		t.Errorf("shape metadata should not appear in length error: %v", err)
	}
}

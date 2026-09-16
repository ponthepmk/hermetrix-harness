package worker

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
)

type reviewAdapter struct{ completion providers.Completion }

func (a reviewAdapter) StreamChat(context.Context, providers.Profile, string, providers.ChatRequest, func(providers.Delta) error) (providers.Completion, error) {
	return a.completion, nil
}

func TestIndependentReviewIsStrictAndStructured(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	completion := providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{
		Name: "submit_review", Arguments: `{"verdict":"approve","rationale":"implementation and test evidence cover AC-1","findings":[]}`,
	}}}
	service := providers.NewService(dataStore, reviewAdapter{completion: completion})
	profile, err := service.Save(ctx, providers.SaveInput{Name: "reviewer", BaseURL: "https://review.example/v1", Model: "review-model", ContextWindow: 98304, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	result, err := ReviewWithProviderService(ctx, service, profile, ReviewTask{TaskID: "task-1", ProposalID: "proposal-1",
		Objective: "fix add", AcceptanceCriteria: []string{"AC-1: addition returns sum"}, Files: map[string]string{"sum.go": "package sample"},
		TestEvidence: []string{"go test ./...: completed, job:1"}}, Options{})
	if err != nil || result.Verdict != "approve" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestIndependentReviewRejectsApprovalWithFindings(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	completion := providers.Completion{Content: `{"verdict":"approve","rationale":"looks fine","findings":["unresolved defect"]}`}
	service := providers.NewService(dataStore, reviewAdapter{completion: completion})
	profile, err := service.Save(ctx, providers.SaveInput{Name: "reviewer", BaseURL: "https://review.example/v1", Model: "review-model", ContextWindow: 98304, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ReviewWithProviderService(ctx, service, profile, ReviewTask{TaskID: "task-1", ProposalID: "proposal-1",
		Objective: "fix", AcceptanceCriteria: []string{"AC-1"}, Files: map[string]string{"a.go": "x"}, TestEvidence: []string{"test passed"}}, Options{})
	if err == nil {
		t.Fatal("approval with unresolved findings was accepted")
	}
}

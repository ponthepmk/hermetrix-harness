package worker

import (
	"context"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
)

type reviewAdapter struct {
	completion providers.Completion
	onRequest  func(providers.ChatRequest)
}

func (a reviewAdapter) StreamChat(_ context.Context, _ providers.Profile, _ string, request providers.ChatRequest, _ func(providers.Delta) error) (providers.Completion, error) {
	if a.onRequest != nil {
		a.onRequest(request)
	}
	return a.completion, nil
}

func TestStepReviewDoesNotRequireLaterPlanDeliverables(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	var request providers.ChatRequest
	service := providers.NewService(dataStore, reviewAdapter{
		completion: providers.Completion{Content: `{"verdict":"approve","rationale":"plan and plan checks pass","findings":[]}`},
		onRequest:  func(got providers.ChatRequest) { request = got },
	})
	profile, err := service.Save(ctx, providers.SaveInput{Name: "step-reviewer", BaseURL: "https://review.example/v1", Model: "review-model", ContextWindow: 98304, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ReviewWithProviderService(ctx, service, profile, ReviewTask{
		TaskID: "task-cms", ProposalID: "proposal-plan", Objective: "Build and test a three-page CMS",
		StepTitle: "Write the CMS plan", StepInstructions: "Document architecture and acceptance checks in PLAN.md",
		StepChecks:         []string{"rg -n AC-1 PLAN.md"},
		AcceptanceCriteria: []string{"AC-1: CRUD, public draft isolation, and mobile/desktop tests"},
		Files:              map[string]string{"PLAN.md": "# CMS plan\nAC-1\n"}, TestEvidence: []string{"rg -n AC-1 PLAN.md: passed"},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 2 || !strings.Contains(request.Messages[0].Content, "do not require outputs or functional tests assigned to later steps") ||
		!strings.Contains(request.Messages[1].Content, `"step_title":"Write the CMS plan"`) ||
		!strings.Contains(request.Messages[1].Content, `"step_checks":["rg -n AC-1 PLAN.md"]`) {
		t.Fatalf("review request did not preserve the current step scope: %+v", request.Messages)
	}
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

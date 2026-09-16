package worker

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
)

func TestFileSelectionIsBoundedToCandidates(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := providers.NewService(dataStore, reviewAdapter{completion: providers.Completion{FinishReason: "tool_calls",
		ToolCalls: []providers.ToolCall{{Name: "submit_file_selection", Arguments: `{"files":["internal/app.go"],"rationale":"contains the implementation"}`}}}})
	profile, err := service.Save(ctx, providers.SaveInput{Name: "selector", BaseURL: "https://selector.example/v1", Model: "selector", ContextWindow: 98304, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	dispatched := false
	result, err := SelectFilesWithProviderService(ctx, service, profile, FileSelectionTask{TaskID: "task-1", Objective: "fix",
		StepTitle: "Fix", StepInstructions: "repair behavior", AcceptanceCriteria: []string{"AC-1: works"},
		Candidates: []FileCandidate{{Path: "internal/app.go", Bytes: 10}, {Path: "internal/app_test.go", Bytes: 20}}},
		Options{BeforeProviderRequest: func() error { dispatched = true; return nil }})
	if err != nil || !dispatched || len(result.Files) != 1 || result.Files[0] != "internal/app.go" {
		t.Fatalf("result=%+v dispatched=%v err=%v", result, dispatched, err)
	}
}

func TestFileSelectionRejectsUnknownPath(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := providers.NewService(dataStore, reviewAdapter{completion: providers.Completion{Content: `{"files":[".env"],"rationale":"try secret"}`}})
	profile, err := service.Save(ctx, providers.SaveInput{Name: "selector", BaseURL: "https://selector.example/v1", Model: "selector", ContextWindow: 98304, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	_, err = SelectFilesWithProviderService(ctx, service, profile, FileSelectionTask{TaskID: "task-1", Objective: "fix",
		StepTitle: "Fix", StepInstructions: "repair", AcceptanceCriteria: []string{"AC-1"},
		Candidates: []FileCandidate{{Path: "main.go", Bytes: 10}}}, Options{})
	if err == nil {
		t.Fatal("selector accepted a path outside the manifest")
	}
}

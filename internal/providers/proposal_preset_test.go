package providers

import (
	"context"
	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/store"
	"testing"
)

type proposalBudgetAdapter struct{ maxTokens int }

func (a *proposalBudgetAdapter) StreamChat(_ context.Context, _ Profile, _ string, request ChatRequest, _ func(Delta) error) (Completion, error) {
	a.maxTokens = request.MaxTokens
	return Completion{Content: "{}", FinishReason: "stop", Usage: Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}}, nil
}

func TestProposalOutputBudgetIsEffectiveAndHistoryUnchanged(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	adapter := &proposalBudgetAdapter{}
	s := NewService(db, adapter)
	p := Profile{ContextWindow: 65536, MaxOutputTokens: 16384}
	first, err := s.ResolveCodeProposalPreset(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if first.GenerationCap != 16384 || first.Role != "worker" {
		t.Fatalf("profile allowance was silently reduced: %+v", first)
	}
	profile, err := s.Save(ctx, SaveInput{Name: "proposal budget", BaseURL: "http://127.0.0.1:8088/v1", Model: "fixture", ContextWindow: 65536, MaxOutputTokens: 16384})
	if err != nil {
		t.Fatal(err)
	}
	bound := inference.WithOwner(ctx, inference.Owner{Kind: "task", ID: "budget-fixture", PresetID: first.ID, PresetRevision: first.Revision, PresetRole: first.Role})
	if _, err = s.StreamChat(bound, profile, ChatRequest{MaxTokens: 16384, Messages: []Message{{Role: "user", Content: "bounded source"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if adapter.maxTokens != 16384 {
		t.Fatalf("wire request silently capped at %d", adapter.maxTokens)
	}
	second, err := s.ResolveCodeProposalPreset(ctx, p)
	if err != nil || first != second {
		t.Fatalf("contract unstable: %+v %v", second, err)
	}
	old, err := inference.LoadPreset(ctx, db.DB, "worker-max", 1)
	if err != nil || old.GenerationCap != 8192 {
		t.Fatal("historical preset rewritten", err)
	}
	p.ContextWindow = 32768
	p.MaxOutputTokens = 16384
	fitted, err := s.ResolveCodeProposalPreset(ctx, p)
	if err != nil || fitted.ContextMax+fitted.GenerationCap+512 > p.ContextWindow {
		t.Fatalf("context overrun: %+v %v", fitted, err)
	}
}

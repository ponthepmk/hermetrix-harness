package providers

import (
	"context"
	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/store"
	"testing"
)

type reviewContractAdapter struct{ request ChatRequest }

func (a *reviewContractAdapter) StreamChat(_ context.Context, _ Profile, _ string, r ChatRequest, _ func(Delta) error) (Completion, error) {
	a.request = r
	return Completion{Content: "{}", FinishReason: "stop"}, nil
}
func TestReviewUsesDedicatedAnswerContractOnWire(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	adapter := &reviewContractAdapter{}
	s := NewService(db, adapter)
	profile, err := s.Save(ctx, SaveInput{Name: "review", BaseURL: "http://127.0.0.1:8088/v1", Model: "fixture", ContextWindow: 65536, MaxOutputTokens: 8192})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.ResolveCodeReviewPreset(ctx, profile)
	if err != nil {
		t.Fatal(err)
	}
	bound := inference.WithOwner(ctx, inference.Owner{Kind: "task", ID: "review-proof", PresetID: p.ID, PresetRevision: p.Revision, PresetRole: p.Role})
	_, err = s.StreamChat(bound, profile, ChatRequest{MaxTokens: 8192, Messages: []Message{{Role: "user", Content: "read-only final source"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := adapter.request
	if r.MaxTokens != 4096 || r.Reasoning == nil || r.Reasoning.Mode != "disabled" || r.Reasoning.TokenCap != 0 {
		t.Fatalf("debug contract leaked into review: %+v", r)
	}
	again, err := s.ResolveCodeReviewPreset(ctx, profile)
	if err != nil || again != p {
		t.Fatal("immutable review identity changed", err)
	}
	old, err := inference.LoadPreset(ctx, db.DB, "worker-debug", 1)
	if err != nil || old.ReasoningMode != "bounded" || old.GenerationCap != 2524 {
		t.Fatal("historical debugging preset changed", err)
	}
}

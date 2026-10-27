package taskcoord

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
)

func TestCodingOperationPlansWithinLocalBudget(t *testing.T) {
	f := newCodingOperationFixture(t, &goalAdapter{})
	ctx := context.Background()

	profile, err := f.coordinator.providers.Get(ctx, f.input.Goal.ProviderID)
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}

	saved, err := f.coordinator.providers.Save(ctx, providers.SaveInput{
		ID:              profile.ID,
		Name:            profile.Name,
		AdapterKind:     profile.AdapterKind,
		BaseURL:         profile.BaseURL,
		Model:           profile.Model,
		ContextWindow:   16384,
		MaxOutputTokens: 3072,
	})
	if err != nil {
		t.Fatalf("save profile: %v", err)
	}
	if saved.ID != profile.ID || saved.Name != profile.Name ||
		saved.AdapterKind != profile.AdapterKind || saved.BaseURL != profile.BaseURL ||
		saved.Model != profile.Model || saved.ContextWindow != 16384 ||
		saved.MaxOutputTokens != 3072 {
		t.Fatalf("saved profile mismatch: %+v", saved)
	}

	result, err := f.operations.Start(ctx, f.input)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if result.Goal == nil {
		t.Fatal("start goal nil")
	}
	if result.Goal.Config.ProviderRevision != saved.Revision {
		t.Fatalf("provider revision = %q, want %q", result.Goal.Config.ProviderRevision, saved.Revision)
	}
	if f.adapter.plans != 0 || f.adapter.proposals != 0 || f.adapter.reviews != 0 {
		t.Fatalf("pre-advance counters plans=%d proposals=%d reviews=%d", f.adapter.plans, f.adapter.proposals, f.adapter.reviews)
	}

	result, err = f.operations.Advance(ctx, f.input.OperationID)
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if result.Goal.State != taskengine.GoalActive {
		t.Fatalf("goal state = %v, want %v", result.Goal.State, taskengine.GoalActive)
	}
	if result.Goal.ActionsUsed != 1 {
		t.Fatalf("actions used = %d, want 1", result.Goal.ActionsUsed)
	}
	if f.adapter.plans != 1 || f.adapter.proposals != 0 || f.adapter.reviews != 0 {
		t.Fatalf("counters plans=%d proposals=%d reviews=%d, want 1 0 0", f.adapter.plans, f.adapter.proposals, f.adapter.reviews)
	}
	if len(result.Task.Plan.Steps) != 1 {
		t.Fatalf("plan steps = %d, want 1", len(result.Task.Plan.Steps))
	}
	assertCodingOperationCounts(t, f.data, 2, 1, 1)
}

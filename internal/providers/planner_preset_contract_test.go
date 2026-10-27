package providers

import (
	"context"
	"testing"

	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/store"
)

func TestPlannerPresetFitsLocalBudget(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := NewService(db, nil)

	profile := Profile{ContextWindow: 16384, MaxOutputTokens: 3072}
	snapshot := profile

	planner, err := inference.LoadPreset(ctx, db.DB, "planner", 1)
	if err != nil {
		t.Fatalf("load planner@1: %v", err)
	}

	preset, err := s.ResolvePlannerPreset(ctx, profile, "planner", 1)
	if err != nil {
		t.Fatalf("resolve planner preset: %v", err)
	}
	if preset.Role != "planner" {
		t.Fatalf("role = %q, want planner", preset.Role)
	}
	if preset.AnswerReserve != 3072 || preset.GenerationCap != 3072 {
		t.Fatalf("answer reserve = %d, want 3072", preset.AnswerReserve)
	}
	if preset.ReasoningMode != "disabled" || preset.ReasoningTokenCap != 0 {
		t.Fatalf("reasoning = %q/%d, want disabled/0", preset.ReasoningMode, preset.ReasoningTokenCap)
	}
	if preset.ContextMax <= 0 || preset.ContextTarget <= 0 {
		t.Fatalf("context max/target = %d/%d, want positive", preset.ContextMax, preset.ContextTarget)
	}
	if preset.ContextMax+preset.GenerationCap+512 > profile.ContextWindow {
		t.Fatalf("context overrun: %d+%d+512 > %d", preset.ContextMax, preset.GenerationCap, profile.ContextWindow)
	}
	if preset.ID == "planner" || preset.ID == "" || preset.ContentDigest == "" {
		t.Fatalf("derived identity invalid: %+v", preset)
	}

	same, err := s.ResolvePlannerPreset(ctx, profile, "planner", 1)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if same != preset {
		t.Fatalf("resolve not stable: %+v vs %+v", same, preset)
	}

	if profile != snapshot {
		t.Fatalf("input profile changed: %+v", profile)
	}

	after, err := inference.LoadPreset(ctx, db.DB, "planner", 1)
	if err != nil {
		t.Fatalf("reload planner@1: %v", err)
	}
	if after != planner {
		t.Fatalf("historical planner@1 changed: %+v", after)
	}

	large := Profile{ContextWindow: 98304, MaxOutputTokens: 8192}
	builtin, err := inference.LoadPreset(ctx, db.DB, "planner", 1)
	if err != nil {
		t.Fatalf("load builtin planner: %v", err)
	}
	fitted, err := s.ResolvePlannerPreset(ctx, large, "planner", 1)
	if err != nil {
		t.Fatalf("large profile resolve: %v", err)
	}
	if fitted != builtin {
		t.Fatalf("large profile did not retain builtin: %+v", fitted)
	}

	for _, name := range []string{"worker-medium", "output0", "context3584"} {
		var p Profile
		switch name {
		case "worker-medium":
			p = Profile{ContextWindow: 16384, MaxOutputTokens: 3072}
		case "output0":
			p = Profile{ContextWindow: 16384, MaxOutputTokens: 0}
		case "context3584":
			p = Profile{ContextWindow: 3584, MaxOutputTokens: 3072}
		}
		presetID := "planner"
		if name == "worker-medium" {
			presetID = "worker-medium"
		}
		got, err := s.ResolvePlannerPreset(ctx, p, presetID, 1)
		if err == nil || got != (inference.Preset{}) {
			t.Fatalf("%s: expected error and zero preset, got %+v %v", name, got, err)
		}
	}
}

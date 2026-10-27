package providers

import (
	"context"
	"database/sql"
	"testing"

	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/store"
)

func TestPlannerPresetWireUsesFittedContract(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	adapter := &presetCaptureAdapter{}
	service := NewService(db, adapter)

	profile, err := service.Save(ctx, SaveInput{
		Name:            "planner wire local",
		BaseURL:         "http://127.0.0.1:8088/v1",
		Model:           "fixture",
		ContextWindow:   16384,
		MaxOutputTokens: 3072,
	})
	if err != nil {
		t.Fatal(err)
	}

	fitted, err := service.ResolvePlannerPreset(ctx, profile, "planner", 1)
	if err != nil {
		t.Fatal(err)
	}

	ownerCtx := inference.WithOwner(ctx, inference.Owner{
		Kind:           "task",
		ID:             "planner-wire-local",
		PresetID:       fitted.ID,
		PresetRevision: fitted.Revision,
		PresetRole:     fitted.Role,
	})

	if _, err = service.StreamChat(ownerCtx, profile, ChatRequest{
		MaxTokens: 1,
		Messages:  []Message{{Role: "user", Content: "one"}},
	}, nil); err != nil {
		t.Fatal(err)
	}

	if adapter.request.MaxTokens != 3072 {
		t.Fatalf("wire max tokens %d, expected 3072", adapter.request.MaxTokens)
	}
	if adapter.request.Reasoning == nil || adapter.request.Reasoning.Mode != "disabled" || adapter.request.Reasoning.TokenCap != 0 {
		t.Fatalf("reasoning wire %+v", adapter.request.Reasoning)
	}
	if fitted.Role != "planner" {
		t.Fatalf("preset role %q, expected planner", fitted.Role)
	}

	var presetID, effectiveParameterDigest, state string
	var presetRevision int
	if err = db.DB.QueryRowContext(ctx, `SELECT preset_id, preset_revision, effective_parameter_digest, state FROM inference_usage_ledger WHERE owner_id = ?`,
		"planner-wire-local").Scan(&presetID, &presetRevision, &effectiveParameterDigest, &state); err != nil {
		t.Fatal(err)
	}
	if presetID != fitted.ID || presetRevision != fitted.Revision {
		t.Fatalf("ledger preset %+v, expected %+v", sql.Row{}, fitted)
	}
	if effectiveParameterDigest != inference.EffectiveParameterDigest(fitted) {
		t.Fatalf("ledger digest %q, expected %q", effectiveParameterDigest, inference.EffectiveParameterDigest(fitted))
	}
	if state != "reconciled" {
		t.Fatalf("ledger state %q, expected reconciled", state)
	}
}

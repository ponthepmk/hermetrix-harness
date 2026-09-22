package inference

import (
	"context"
	"strings"
	"testing"

	"hermetrix-harness/internal/store"
)

func TestContextBoundPresetKeepsReservationsAndHasStableImmutableIdentity(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	base, err := LoadPreset(ctx, dataStore.DB, "planner", 1)
	if err != nil {
		t.Fatal(err)
	}
	fitted, err := ResolvePresetForContext(ctx, dataStore.DB, "planner", 1, 61440, 8192, 512)
	if err != nil {
		t.Fatal(err)
	}
	if fitted.ContextMax != 55808 || fitted.ContextTarget != base.ContextTarget || fitted.GenerationCap != base.GenerationCap ||
		fitted.ReasoningTokenCap != base.ReasoningTokenCap || fitted.AnswerReserve != base.AnswerReserve ||
		fitted.Temperature != base.Temperature || fitted.Revision != 1 || !strings.HasPrefix(fitted.ID, "planner-fit-") || fitted.ContentDigest == base.ContentDigest {
		t.Fatalf("base=%+v fitted=%+v", base, fitted)
	}
	repeated, err := ResolvePresetForContext(ctx, dataStore.DB, "planner", 1, 61440, 8192, 512)
	if err != nil || repeated != fitted {
		t.Fatalf("derived preset was not reused: %+v err=%v", repeated, err)
	}
	unchanged, err := LoadPreset(ctx, dataStore.DB, "planner", 1)
	if err != nil || unchanged != base {
		t.Fatalf("original contract changed: %+v err=%v", unchanged, err)
	}
	full, err := ResolvePresetForContext(ctx, dataStore.DB, "planner", 1, 98304, 8192, 512)
	if err != nil || full != base {
		t.Fatalf("full context should retain original identity: %+v err=%v", full, err)
	}
	compact, err := ResolvePresetForContext(ctx, dataStore.DB, "planner", 1, 32768, 8192, 512)
	if err != nil || compact.ContextMax != 27136 || compact.ContextTarget != compact.ContextMax || compact.ID == fitted.ID {
		t.Fatalf("compact contract=%+v err=%v", compact, err)
	}
	for _, budgets := range [][3]int{{61440, 4096, 512}, {5600, 8192, 512}, {61440, 8192, -1}} {
		if _, err := ResolvePresetForContext(ctx, dataStore.DB, "planner", 1, budgets[0], budgets[1], budgets[2]); err == nil {
			t.Fatalf("invalid context/output/safety reservations accepted: %v", budgets)
		}
	}
}

func TestPresetRevisionsAreImmutableAndDigestBound(t *testing.T) {
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	ctx := context.Background()
	input := Preset{ID: "custom-worker", Role: "worker", ContextTarget: 8000, ContextMax: 16000, ReasoningMode: "disabled", AnswerReserve: 1000, GenerationCap: 1000, Temperature: .1}
	first, err := CreatePreset(ctx, dataStore.DB, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreatePreset(ctx, dataStore.DB, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || second.Revision != 2 || first.ContentDigest == second.ContentDigest {
		t.Fatalf("revisions first=%+v second=%+v", first, second)
	}
	if _, err = dataStore.DB.Exec(`UPDATE inference_presets SET generation_cap=2000 WHERE id='custom-worker' AND revision=1`); err == nil {
		t.Fatal("immutable preset was updated")
	}
	items, err := ListPresets(ctx, dataStore.DB)
	if err != nil || len(items) < 5 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
}

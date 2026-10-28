package taskcoord

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

type plannerPolicyCase struct {
	Name        string
	URL         string
	Evidence    string
	Fingerprint string
	Context     int
	Output      int
	Rejected    bool
	WantLarge   bool
}

func TestPlannerPresetPolicyFrozenCases(t *testing.T) {
	data, err := os.ReadFile("planner_policy_cases.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var rows []plannerPolicyCase
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(rows) != 10 {
		t.Fatalf("expected 10 rows, got %d", len(rows))
	}

	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	ps := providers.NewService(db, nil)
	c := New(taskengine.NewService(db), nil, ps)

	for i, row := range rows {
		profile := providers.Profile{
			Name:                 row.Name,
			BaseURL:              row.URL,
			Model:                "fixture",
			ContextWindow:        row.Context,
			MaxOutputTokens:      row.Output,
			ContextEvidence:      row.Evidence,
			RuntimeFingerprintID: row.Fingerprint,
			Enabled:              true,
		}
		profileCopy := profile

		preset, err := c.selectPlannerPreset(ctx, profile, row.Rejected)
		if err != nil {
			t.Fatalf("case %d %q: selectPlannerPreset: %v", i, row.Name, err)
		}

		expectedID := "planner"
		if row.WantLarge {
			expectedID = "planner-large"
		}
		expected, err := ps.ResolvePlannerPreset(ctx, profile, expectedID, 1)
		if err != nil {
			t.Fatalf("case %d %q: resolve expected preset %q: %v", i, row.Name, expectedID, err)
		}
		if preset.ID != expected.ID || preset.Revision != expected.Revision || preset.AnswerReserve != expected.AnswerReserve || preset.GenerationCap != expected.GenerationCap {
			t.Fatalf("case %d %q: preset mismatch: %+v vs %+v", i, row.Name, preset, expected)
		}

		if err := inference.ValidatePreset(preset, profile.ContextWindow, profile.MaxOutputTokens, 512); err != nil {
			t.Fatalf("case %d %q: ValidatePreset: %v", i, row.Name, err)
		}

		if profile != profileCopy {
			t.Fatalf("case %d %q: caller profile changed", i, row.Name)
		}
	}
}

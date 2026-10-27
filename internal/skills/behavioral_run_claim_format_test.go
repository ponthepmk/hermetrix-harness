package skills

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestBehavioralRunClaimRejectsMalformedInputs(t *testing.T) {
	ctx := context.Background()

	service, _ := testService(t)
	candidate, err := service.CreateCandidate(ctx, candidateInput("claim-format", "Claim format", "# Claim format\n\n1. Claim once."))
	if err != nil {
		t.Fatal(err)
	}

	bindings, err := loadBindingsCases(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) == 0 {
		t.Fatal("no bindings cases")
	}
	first := bindings[0]
	if len(first.CaseIDs) == 0 {
		t.Fatal("case ids empty")
	}

	run, err := service.PrepareBehavioralRun(ctx, "claim-format-run", candidate.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "claim-format-run" {
		t.Fatalf("prepared run id mismatch: %s", run.ID)
	}
	if run.State != BehavioralRunPrepared {
		t.Fatalf("prepared state mismatch: %s", run.State)
	}

	type caseRow struct {
		runID    string
		sha      string
		bindings BehavioralRunBindings
	}

	tests := []caseRow{
		{"", run.ManifestSHA256, first},
		{"  ", run.ManifestSHA256, first},
		{run.ID, "", first},
		{run.ID, strings.Repeat("A", 64), first},
	}

	for _, tc := range tests {
		claimed, err := service.ClaimBehavioralRun(ctx, tc.runID, tc.sha, tc.bindings)
		if !errors.Is(err, ErrBehavioralRunBindings) {
			t.Fatalf("claim error: %v", err)
		}
		if !reflect.DeepEqual(claimed, BehavioralRun{}) {
			t.Fatalf("claim returned nonzero run: %+v", claimed)
		}
	}

	copied := first
	copied.CaseIDs = nil

	claimed, err := service.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, copied)
	if !errors.Is(err, ErrBehavioralRunBindings) {
		t.Fatalf("claim error: %v", err)
	}
	if !reflect.DeepEqual(claimed, BehavioralRun{}) {
		t.Fatalf("claim returned nonzero run: %+v", claimed)
	}
	if first.CaseIDs == nil {
		t.Fatal("original CaseIDs mutated")
	}

	full, err := service.GetBehavioralRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full, run) {
		t.Fatalf("full record mismatch: %+v", full)
	}

	claimed, err = service.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, first)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != run.ID {
		t.Fatalf("claimed run id mismatch: %s", claimed.ID)
	}
	if claimed.State != BehavioralRunDispatched {
		t.Fatalf("claimed state mismatch: %s", claimed.State)
	}
	if claimed.ManifestSHA256 != run.ManifestSHA256 {
		t.Fatalf("claimed manifest sha mismatch")
	}
	if !reflect.DeepEqual(claimed.Manifest, run.Manifest) {
		t.Fatal("claimed manifest mismatch")
	}
}

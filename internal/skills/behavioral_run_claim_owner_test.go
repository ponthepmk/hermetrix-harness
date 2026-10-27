package skills

import (
	"context"
	"errors"
	"hermetrix-harness/internal/identity"
	"reflect"
	"testing"
	"time"
)

func TestBehavioralRunClaimDeniesMissingAndUnknownPrincipal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	service, dataStore := testService(t)
	candidate, err := service.CreateCandidate(ctx, candidateInput("claim-owner", "Claim owner", "# Claim owner\n\n1. Owner claim."))
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

	run, err := service.PrepareBehavioralRun(ctx, "claim-owner-run", candidate.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "claim-owner-run" {
		t.Fatalf("prepared run id mismatch: %s", run.ID)
	}
	if run.State != BehavioralRunPrepared {
		t.Fatalf("prepared state mismatch: %s", run.State)
	}

	missing, err := service.ClaimBehavioralRun(ctx, "missing-run", run.ManifestSHA256, first)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing run claim error: %v", err)
	}
	if !reflect.DeepEqual(missing, BehavioralRun{}) {
		t.Fatal("missing run claim returned nonzero run")
	}

	unknownCtx, cancelUnknown := context.WithTimeout(ctx, 10*time.Second)
	defer cancelUnknown()
	unknownCtx = identity.WithPrincipal(unknownCtx, "unknown-principal")

	unknown, err := service.ClaimBehavioralRun(unknownCtx, run.ID, run.ManifestSHA256, first)
	if err == nil {
		t.Fatal("unknown principal claim succeeded")
	}
	if !reflect.DeepEqual(unknown, BehavioralRun{}) {
		t.Fatal("unknown principal claim returned nonzero run")
	}

	owner, err := service.GetBehavioralRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(owner, run) {
		t.Fatal("owner get mismatch")
	}

	claimed, err := service.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, first)
	if err != nil {
		t.Fatalf("owner claim error: %v", err)
	}
	if claimed.ID != run.ID {
		t.Fatalf("claimed run id mismatch: %s", claimed.ID)
	}
	if claimed.State != BehavioralRunDispatched {
		t.Fatalf("claimed state mismatch: %s", claimed.State)
	}
	if claimed.ManifestSHA256 != run.ManifestSHA256 {
		t.Fatal("claimed manifest sha mismatch")
	}
	if !reflect.DeepEqual(claimed.Manifest, run.Manifest) {
		t.Fatal("claimed manifest mismatch")
	}

	var count int
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("run count=%d err=%v", count, err)
	}

	var candidateCount int
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM candidate_behavioral_evals`).Scan(&candidateCount); err != nil || candidateCount != 0 {
		t.Fatalf("candidate eval count=%d err=%v", candidateCount, err)
	}
}

package skills

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestBehavioralRunClaimReplayNeverAuthorizesAgain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	service, dataStore := testService(t)
	candidate, err := service.CreateCandidate(ctx, candidateInput("claim-replay", "Claim replay", "# Claim replay\n\n1. Claim once."))
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

	run, err := service.PrepareBehavioralRun(ctx, "claim-replay-run", candidate.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "claim-replay-run" {
		t.Fatalf("prepared run id mismatch: %s", run.ID)
	}
	if run.State != BehavioralRunPrepared {
		t.Fatalf("prepared state mismatch: %s", run.State)
	}

	claimed, err := service.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, first)
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

	full, err := service.GetBehavioralRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full, claimed) {
		t.Fatal("full record mismatch")
	}

	replay, err := service.PrepareBehavioralRun(ctx, run.ID, candidate.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, claimed) {
		t.Fatal("replay mismatch")
	}

	again, err := service.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, first)
	if !errors.Is(err, ErrBehavioralRunUncertain) {
		t.Fatalf("second claim error: %v", err)
	}
	if !reflect.DeepEqual(again, BehavioralRun{}) {
		t.Fatal("second claim returned nonzero run")
	}

	final, err := service.GetBehavioralRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(final, claimed) {
		t.Fatal("final record mismatch")
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

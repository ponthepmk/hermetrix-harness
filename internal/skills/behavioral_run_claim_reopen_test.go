package skills

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"hermetrix-harness/internal/store"
)

func TestBehavioralRunClaimSurvivesReopenWithoutReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	root := t.TempDir()
	dataStore, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := dataStore.Close(); err != nil {
			t.Fatalf("close dataStore: %v", err)
		}
	}()

	service := NewService(dataStore)
	candidate, err := service.CreateCandidate(ctx, candidateInput("claim-reopen", "Claim survives reopen", "# Procedure\n\n1. Snapshot."))
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

	hostID := "claim-reopen"
	run, err := service.PrepareBehavioralRun(ctx, hostID, candidate.ID, first)
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := service.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, first)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed.ID != run.ID {
		t.Fatalf("claimed ID mismatch: %q", claimed.ID)
	}
	if claimed.ManifestSHA256 != run.ManifestSHA256 {
		t.Fatalf("claimed manifest hash mismatch: %q", claimed.ManifestSHA256)
	}
	if claimed.State != BehavioralRunDispatched {
		t.Fatalf("claimed state %q, want %q", claimed.State, BehavioralRunDispatched)
	}
	expected := run
	expected.State = BehavioralRunDispatched
	if !reflect.DeepEqual(claimed, expected) {
		t.Fatalf("claimed record mismatch: %+v", claimed)
	}
	if err := dataStore.Close(); err != nil {
		t.Fatal(err)
	}
	dataStore, err = store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	service = NewService(dataStore)

	reopened, err := service.GetBehavioralRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reopened, claimed) {
		t.Fatalf("reopened run mismatch: %+v", reopened)
	}

	reclaim, err := service.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, first)
	if !errors.Is(err, ErrBehavioralRunUncertain) {
		t.Fatalf("reclaim error %v, want ErrBehavioralRunUncertain", err)
	}
	if !reflect.DeepEqual(reclaim, BehavioralRun{}) {
		t.Fatalf("reclaim returned non-zero: %+v", reclaim)
	}

	replay, err := service.PrepareBehavioralRun(ctx, run.ID, candidate.ID, first)
	if err != nil {
		t.Fatalf("prepare replay: %v", err)
	}
	if !reflect.DeepEqual(replay, claimed) {
		t.Fatalf("replay mismatch: %+v", replay)
	}

	var count int
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("behavioral_eval_runs count=%d err=%v", count, err)
	}
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM candidate_behavioral_evals`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("candidate_behavioral_evals count=%d err=%v", count, err)
	}
}

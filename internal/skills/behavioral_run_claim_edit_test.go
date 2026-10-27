package skills

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"hermetrix-harness/internal/store"
)

func TestBehavioralRunClaimRefusesEditedCandidate(t *testing.T) {
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
	candidate, err := service.CreateCandidate(ctx, candidateInput("claim-edit", "Edited candidate", "# Procedure\n\n1. Snapshot."))
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

	hostID := "claim-edit"
	run, err := service.PrepareBehavioralRun(ctx, hostID, candidate.ID, first)
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := service.GetCandidate(ctx, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateCandidate(ctx, candidate.ID, UpdateCandidateInput{
		Markdown:         reloaded.Markdown + "\n2. Additional step.",
		Actor:            "test",
		ExpectedRevision: reloaded.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision <= reloaded.Revision {
		t.Fatalf("updated revision %d not greater than %d", updated.Revision, reloaded.Revision)
	}
	if updated.CandidateHash == reloaded.CandidateHash {
		t.Fatal("candidate hash unchanged after update")
	}
	if updated.State != "needs_review" || updated.Checks.Passed != true {
		t.Fatalf("updated candidate state=%q checks.passed=%v", updated.State, updated.Checks.Passed)
	}

	claim, err := service.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, first)
	if err == nil {
		t.Fatalf("claim succeeded after candidate edit: %+v", claim)
	}
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("claim error %q not ErrRevisionConflict", err)
	}
	if !reflect.DeepEqual(claim, BehavioralRun{}) {
		t.Fatalf("claim returned nonzero run: %+v", claim)
	}

	snapshot, err := service.GetBehavioralRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot, run) {
		t.Fatalf("run changed after candidate edit: %+v", snapshot)
	}

	var count int
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("behavioral_eval_runs count=%d err=%v", count, err)
	}
	var evalCount int
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM candidate_behavioral_evals`).Scan(&evalCount); err != nil || evalCount != 0 {
		t.Fatalf("candidate_behavioral_evals count=%d err=%v", evalCount, err)
	}
}

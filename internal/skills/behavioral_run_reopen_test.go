package skills

import (
	"context"
	"reflect"
	"testing"
	"time"

	"hermetrix-harness/internal/store"
)

func TestBehavioralRunReopenPinsHistory(t *testing.T) {
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
	candidate, err := service.CreateCandidate(ctx, candidateInput("run-history", "Durable history", "# Procedure\n\n1. Snapshot."))
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

	hostID := "history-run"
	run, err := service.PrepareBehavioralRun(ctx, hostID, candidate.ID, first)
	if err != nil {
		t.Fatal(err)
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
	if !reflect.DeepEqual(reopened, run) {
		t.Fatalf("reopened run mismatch: %+v", reopened)
	}

	replay, err := service.PrepareBehavioralRun(ctx, run.ID, candidate.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, run) {
		t.Fatalf("replay mismatch: %+v", replay)
	}

	candidate, err = service.GetCandidate(ctx, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateCandidate(ctx, candidate.ID, UpdateCandidateInput{
		Markdown:         candidate.Markdown + "\n2. Extra detail.",
		Actor:            "test",
		ExpectedRevision: candidate.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision <= candidate.Revision {
		t.Fatalf("updated revision %d not greater than %d", updated.Revision, candidate.Revision)
	}
	if updated.CandidateHash == candidate.CandidateHash {
		t.Fatal("candidate hash unchanged after update")
	}

	snapshot, err := service.GetBehavioralRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot, run) {
		t.Fatalf("old run changed after candidate update: %+v", snapshot)
	}

	if _, err := service.PrepareBehavioralRun(ctx, run.ID, candidate.ID, first); err != nil {
		t.Fatalf("prepare old id with updated candidate: %v", err)
	}

	newRun, err := service.PrepareBehavioralRun(ctx, "history-run-new", candidate.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if newRun.Manifest.CandidateRevision != updated.Revision || newRun.Manifest.CandidateHash != updated.CandidateHash {
		t.Fatalf("new run did not pin updated candidate: %+v", newRun.Manifest)
	}

	var count int
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("behavioral_eval_runs count=%d err=%v", count, err)
	}
}

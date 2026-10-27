package skills

import (
	"context"
	"testing"

	"hermetrix-harness/internal/store"
)

func TestBehavioralRunUpgradeFrom78(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dataStore, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(dataStore)
	candidate, err := service.CreateCandidate(ctx, candidateInput("run-upgrade", "Upgrade persistence", "# Procedure\n\n1. Verify migration."))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err = service.GetCandidate(ctx, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.OwnerPrincipalID == "" {
		t.Fatal("candidate.OwnerPrincipalID must be nonempty")
	}
	if _, err := dataStore.DB.Exec(`DROP TABLE IF EXISTS behavioral_eval_runs`); err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.DB.Exec(`PRAGMA user_version=78`); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.Close(); err != nil {
		t.Fatal(err)
	}
	dataStore, err = store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	var version int
	if err := dataStore.DB.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != store.CurrentSchemaVersion {
		t.Fatalf("user_version=%d err=%v", version, err)
	}
	var count int
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='behavioral_eval_runs'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("behavioral_eval_runs count=%d err=%v", count, err)
	}
	got, err := NewService(dataStore).GetCandidate(ctx, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != candidate.ID || got.CandidateHash != candidate.CandidateHash || got.CandidateBlobRef != candidate.CandidateBlobRef || got.Revision != candidate.Revision || got.OwnerPrincipalID != candidate.OwnerPrincipalID {
		t.Fatalf("candidate changed: %+v", got)
	}
}

package skills

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/identity"
)

func TestBehavioralRunPrepareReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	service, dataStore := testService(t)
	candidate, err := service.CreateCandidate(ctx, candidateInput("prepared-run", "Pinned run", "# Procedure\n\n1. Pin run."))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := dataStore.OwnerPrincipalID(ctx)
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
	run, err := service.PrepareBehavioralRun(ctx, "fixture-prepare", candidate.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "fixture-prepare" || run.State != BehavioralRunPrepared {
		t.Fatalf("run id/state: %s %s", run.ID, run.State)
	}
	if run.Manifest.CandidateID != candidate.ID || run.Manifest.CandidateRevision != candidate.Revision || run.Manifest.CandidateHash != candidate.CandidateHash || run.Manifest.CandidateBlobRef != candidate.CandidateBlobRef {
		t.Fatal("manifest candidate mismatch")
	}
	if run.Manifest.OwnerPrincipalID != owner {
		t.Fatal("manifest owner mismatch")
	}
	if run.StartedAt.IsZero() || run.StartedAt.Location() != time.UTC {
		t.Fatal("started at zero or not UTC")
	}
	if run.CompletedAt != nil {
		t.Fatal("completed at not nil")
	}
	if run.EvaluationID != "" || run.ResultSHA256 != "" {
		t.Fatal("evaluation id or result sha set")
	}
	firstCaseID := first.CaseIDs[0]
	first.CaseIDs[0] = "mutated"
	if run.Manifest.Bindings.CaseIDs[0] != firstCaseID {
		t.Fatalf("original run bindings case id changed: %s", run.Manifest.Bindings.CaseIDs[0])
	}
	first.CaseIDs[0] = firstCaseID
	same, err := service.GetBehavioralRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(same, run) || same.ID != run.ID || same.ManifestSHA256 != run.ManifestSHA256 || same.State != run.State || !same.StartedAt.Equal(run.StartedAt) {
		t.Fatal("get mismatch")
	}
	replay, err := service.PrepareBehavioralRun(ctx, run.ID, candidate.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay, run) {
		t.Fatal("replay mismatch")
	}
	var count int
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("run count=%d err=%v", count, err)
	}
	conflicting := first
	conflicting.DatasetSHA256 = strings.Repeat("f", 64)
	_, err = service.PrepareBehavioralRun(ctx, run.ID, candidate.ID, conflicting)
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected revision conflict, got %v", err)
	}
	unknown, err := service.GetBehavioralRun(ctx, "missing")
	if !errors.Is(err, ErrNotFound) || unknown.ID != "" {
		t.Fatalf("expected not found, got %v %+v", err, unknown)
	}
	ctx2 := identity.WithPrincipal(ctx, "unknown-principal")
	if denied, err := service.GetBehavioralRun(ctx2, run.ID); err == nil || denied.ID != "" {
		t.Fatalf("expected not found for unknown principal, got %v", err)
	}
	newID := "new-run-id"
	if _, err := service.PrepareBehavioralRun(ctx, newID, "missing-candidate", first); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found for missing candidate, got %v", err)
	}
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("run count after negative calls=%d err=%v", count, err)
	}
}

func loadBindingsCases(t *testing.T) ([]BehavioralRunBindings, error) {
	data, err := os.ReadFile("behavioral_run_binding_cases.json")
	if err != nil {
		return nil, err
	}
	var root struct {
		BindingsCases []struct {
			Bindings BehavioralRunBindings `json:"bindings"`
		} `json:"bindings_cases"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	out := make([]BehavioralRunBindings, 0, len(root.BindingsCases))
	for _, w := range root.BindingsCases {
		out = append(out, w.Bindings)
	}
	return out, nil
}

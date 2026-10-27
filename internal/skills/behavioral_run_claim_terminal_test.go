package skills

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestBehavioralRunClaimRefusesUncertainAndClosedStates(t *testing.T) {
	ctx := context.Background()

	service, dataStore := testService(t)
	candidate, err := service.CreateCandidate(ctx, candidateInput("claim-terminal", "Claim terminal", "# Claim terminal\n\n1. Claim once."))
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

	run, err := service.PrepareBehavioralRun(ctx, "claim-terminal-run", candidate.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "claim-terminal-run" {
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

	baseline, err := service.GetBehavioralRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseline, claimed) {
		t.Fatal("baseline record mismatch")
	}

	states := []string{"uncertain", "aborted", "stale"}
	for _, state := range states {
		res, err := dataStore.DB.Exec(`UPDATE behavioral_eval_runs SET state=? WHERE id=?`, state, run.ID)
		if err != nil {
			t.Fatalf("update state %s: %v", state, err)
		}
		count, err := res.RowsAffected()
		if err != nil {
			t.Fatalf("rows affected for state %s: %v", state, err)
		}
		if count != 1 {
			t.Fatalf("expected exactly 1 row updated for state %s, got %d", state, count)
		}

		runID := run.ID
		sha := run.ManifestSHA256
		runCopy := baseline
		copy := runCopy
		copy.State = state

		next, err := service.ClaimBehavioralRun(ctx, runID, sha, first)
		if state == "uncertain" {
			if !errors.Is(err, ErrBehavioralRunUncertain) {
				t.Fatalf("claim after %s error: %v", state, err)
			}
		} else {
			if !errors.Is(err, ErrRevisionConflict) {
				t.Fatalf("claim after %s error: %v", state, err)
			}
		}
		if !reflect.DeepEqual(next, BehavioralRun{}) {
			t.Fatalf("claim after %s returned nonzero run: %+v", state, next)
		}

		got, err := service.GetBehavioralRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("get after %s: %v", state, err)
		}
		if !reflect.DeepEqual(got, copy) {
			t.Fatalf("record after %s mismatch: %+v", state, got)
		}
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

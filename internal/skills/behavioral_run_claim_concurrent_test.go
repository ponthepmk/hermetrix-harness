package skills

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestBehavioralRunConcurrentClaimOneWinner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	service, dataStore := testService(t)
	candidate, err := service.CreateCandidate(ctx, candidateInput("concurrent-claim", "Concurrent claim", "# Procedure\n\n1. Claim concurrently."))
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

	run, err := service.PrepareBehavioralRun(ctx, "concurrent-claim-run", candidate.ID, first)
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		run BehavioralRun
		err error
	}

	results := make(chan result, 2)
	start := make(chan struct{})

	for i := 0; i < 2; i++ {
		go func() {
			<-start
			run, err := service.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, first)
			results <- result{run: run, err: err}
		}()
	}

	close(start)

	var got [2]result
	for i := 0; i < 2; i++ {
		select {
		case r := <-results:
			got[i] = r
		case <-ctx.Done():
			t.Fatal("timeout waiting for concurrent claim results")
		}
	}

	var success, uncertain int
	for _, r := range got {
		switch {
		case r.err == nil:
			success++
		case errors.Is(r.err, ErrBehavioralRunUncertain):
			uncertain++
		default:
			t.Fatalf("unexpected claim error: %v", r.err)
		}
	}
	if success != 1 || uncertain != 1 {
		t.Fatalf("success=%d uncertain=%d", success, uncertain)
	}

	winner := got[0]
	if winner.err != nil {
		winner = got[1]
	}
	if winner.run.ID != run.ID {
		t.Fatalf("winner id mismatch: %s", winner.run.ID)
	}
	if winner.run.State != BehavioralRunDispatched {
		t.Fatalf("winner state=%q", winner.run.State)
	}
	if winner.run.ManifestSHA256 != run.ManifestSHA256 {
		t.Fatalf("winner manifest sha mismatch")
	}
	if !reflect.DeepEqual(winner.run.Manifest, run.Manifest) {
		t.Fatal("winner manifest mismatch")
	}
	if winner.run.ResultSHA256 != run.ResultSHA256 {
		t.Fatalf("winner result sha mismatch: %s", winner.run.ResultSHA256)
	}
	if winner.run.EvaluationID != run.EvaluationID {
		t.Fatalf("winner evaluation id mismatch: %s", winner.run.EvaluationID)
	}
	if winner.run.StartedAt != run.StartedAt {
		t.Fatalf("winner started at mismatch: %v", winner.run.StartedAt)
	}
	if winner.run.CompletedAt != run.CompletedAt {
		t.Fatalf("winner completed at mismatch: %v", winner.run.CompletedAt)
	}

	loser := got[0]
	if loser.err == nil {
		loser = got[1]
	}
	if !reflect.DeepEqual(loser.run, BehavioralRun{}) {
		t.Fatalf("loser run not zero: %+v", loser.run)
	}

	var count int
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("run count=%d err=%v", count, err)
	}

	same, err := service.GetBehavioralRun(ctx, winner.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(same, winner.run) {
		t.Fatal("get mismatch")
	}
}

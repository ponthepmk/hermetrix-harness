package skills

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestBehavioralRunConcurrentPrepareIsOneRecord(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	service, dataStore := testService(t)
	candidate, err := service.CreateCandidate(ctx, candidateInput("concurrent-run", "Concurrent run", "# Procedure\n\n1. Prepare concurrently."))
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

	type result struct {
		run BehavioralRun
		err error
	}

	results := make(chan result, 2)
	start := make(chan struct{})

	for i := 0; i < 2; i++ {
		go func() {
			<-start
			run, err := service.PrepareBehavioralRun(ctx, "concurrent-owned-run", candidate.ID, first)
			results <- result{run: run, err: err}
		}()
	}

	close(start)

	var got [2]result
	for i := 0; i < 2; i++ {
		select {
		case r := <-results:
			got[i] = r
			if r.err != nil {
				t.Fatalf("prepare error: %v", r.err)
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for concurrent prepare results")
		}
	}

	for _, r := range got {
		if r.run.ID != "concurrent-owned-run" {
			t.Fatalf("run id mismatch: %s", r.run.ID)
		}
	}

	if !reflect.DeepEqual(got[0].run, got[1].run) {
		t.Fatal("concurrent prepare runs differ")
	}

	var count int
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("run count=%d err=%v", count, err)
	}

	same, err := service.GetBehavioralRun(ctx, got[0].run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(same, got[0].run) {
		t.Fatal("get mismatch")
	}
}

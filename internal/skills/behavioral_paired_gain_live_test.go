package skills

import (
	"context"
	"encoding/json"
	"testing"
)

// TestBehavioralPairedGainRecomputesTotals proves the paired-evidence rule:
// aggregate counters are recomputed from the paired cases, not trusted from
// the caller. The unchanged implementation retains the caller counters, so
// this test fails against it.
func TestBehavioralPairedGainRecomputesTotals(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	candidate := improvementAwaitingEval(t, service)

	cases := []json.RawMessage{
		json.RawMessage(`{"case_id":"A","baseline_passed":true,"candidate_passed":true}`),
		json.RawMessage(`{"case_id":"B","baseline_passed":true,"candidate_passed":true}`),
		json.RawMessage(`{"case_id":"C","baseline_passed":true,"candidate_passed":true}`),
		json.RawMessage(`{"case_id":"D","baseline_passed":false,"candidate_passed":true}`),
		json.RawMessage(`{"case_id":"E","baseline_passed":false,"candidate_passed":false}`),
	}
	result, err := json.Marshal(map[string]any{"cases": cases})
	if err != nil {
		t.Fatal(err)
	}

	eval, err := service.RecordBehavioralEval(ctx, BehavioralEvalInput{
		CandidateID:     candidate.ID,
		BaseVersionID:   candidate.BaseVersionID,
		Tasks:           5,
		BaselinePassed:  0,
		CandidatePassed: 0,
		Result:          json.RawMessage(result),
	})
	if err != nil {
		t.Fatalf("RecordBehavioralEval: %v", err)
	}
	if eval.State != EvalPassed {
		t.Fatalf("state = %q, want passed", eval.State)
	}
	if eval.Tasks != 5 {
		t.Fatalf("tasks = %d, want 5", eval.Tasks)
	}
	if eval.BaselinePassed != 3 {
		t.Fatalf("baseline_passed = %d, want 3", eval.BaselinePassed)
	}
	if eval.CandidatePassed != 4 {
		t.Fatalf("candidate_passed = %d, want 4", eval.CandidatePassed)
	}
	if eval.Regressions != 0 {
		t.Fatalf("regressions = %d, want 0", eval.Regressions)
	}
}

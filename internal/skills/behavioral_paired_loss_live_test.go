package skills

import (
	"context"
	"encoding/json"
	"testing"
)

// TestBehavioralPairedLossCannotBeOffset proves the paired-evidence rule:
// a candidate that loses one previously-passing case must fail even when the
// aggregate totals are tied. The unchanged implementation accepts tied totals,
// so this test fails against it.
func TestBehavioralPairedLossCannotBeOffset(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	candidate := improvementAwaitingEval(t, service)

	cases := []json.RawMessage{
		json.RawMessage(`{"case_id":"A","baseline_passed":true,"candidate_passed":false}`),
		json.RawMessage(`{"case_id":"B","baseline_passed":false,"candidate_passed":true}`),
		json.RawMessage(`{"case_id":"C","baseline_passed":true,"candidate_passed":true}`),
		json.RawMessage(`{"case_id":"D","baseline_passed":true,"candidate_passed":true}`),
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
		BaselinePassed:  3,
		CandidatePassed: 3,
		Result:          json.RawMessage(result),
	})
	if err != nil {
		t.Fatalf("RecordBehavioralEval: %v", err)
	}
	if eval.State != EvalFailed {
		t.Fatalf("state = %q, want failed", eval.State)
	}
	if eval.Regressions != 1 {
		t.Fatalf("regressions = %d, want 1", eval.Regressions)
	}
}

package skills

import (
	"context"
	"encoding/json"
	"testing"
)

func TestBehavioralPairedMissingBooleanEvidence(t *testing.T) {
	service, _ := testService(t)
	candidate := improvementAwaitingEval(t, service)

	input := BehavioralEvalInput{
		CandidateID:       candidate.ID,
		BaseVersionID:     candidate.BaseVersionID,
		Tasks:             5,
		BaselinePassed:    3,
		CandidatePassed:   3,
		FalseSuccessDelta: 0,
		Result:            json.RawMessage(`{"cases":[{"case_id":"A","baseline_passed":true,"candidate_passed":true},{"case_id":"B","baseline_passed":true,"candidate_passed":true},{"case_id":"C","baseline_passed":true,"candidate_passed":true},{"case_id":"D","baseline_passed":false,"candidate_passed":false},{"case_id":"E","baseline_passed":false}]}`),
	}

	eval, err := service.RecordBehavioralEval(context.Background(), input)
	if err != nil {
		return
	}
	if eval.State == EvalPassed {
		t.Fatalf("missing candidate_passed for case E must not be treated as passing; got state %q", eval.State)
	}
}

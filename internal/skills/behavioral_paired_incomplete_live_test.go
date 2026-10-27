package skills

import (
	"context"
	"encoding/json"
	"testing"
)

var incompleteFixture = json.RawMessage(`{"cases":[{"case_id":"A","baseline_passed":true,"candidate_passed":true},{"case_id":"B","baseline_passed":true,"candidate_passed":true},{"case_id":"C","baseline_passed":true,"candidate_passed":true},{"case_id":"D","baseline_passed":false,"candidate_passed":false}]}`)

func TestBehavioralPairedIncompleteEvidence(t *testing.T) {
	service, _ := testService(t)
	candidate := improvementAwaitingEval(t, service)

	eval, err := service.RecordBehavioralEval(context.Background(), BehavioralEvalInput{
		CandidateID:       candidate.ID,
		BaseVersionID:     candidate.BaseVersionID,
		Tasks:             5,
		BaselinePassed:    3,
		CandidatePassed:   3,
		FalseSuccessDelta: 0,
		Result:            incompleteFixture,
	})
	if err != nil {
		return
	}
	if eval.State == EvalPassed {
		t.Fatalf("incomplete evidence must not pass: state=%s", eval.State)
	}
}

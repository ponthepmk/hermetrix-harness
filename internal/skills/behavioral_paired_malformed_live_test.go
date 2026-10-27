package skills

import (
	"context"
	"encoding/json"
	"testing"
)

func TestBehavioralPairedMalformedEvidence(t *testing.T) {
	service, _ := testService(t)
	candidate := improvementAwaitingEval(t, service)

	input := BehavioralEvalInput{
		CandidateID:       candidate.ID,
		BaseVersionID:     candidate.BaseVersionID,
		Tasks:             5,
		BaselinePassed:    3,
		CandidatePassed:   3,
		FalseSuccessDelta: 0,
		Result:            json.RawMessage(`{"cases":[`),
	}

	eval, err := service.RecordBehavioralEval(context.Background(), input)
	if err != nil {
		return
	}
	if eval.State == EvalPassed {
		t.Fatalf("malformed evidence accepted as passing: %q", eval.State)
	}
}

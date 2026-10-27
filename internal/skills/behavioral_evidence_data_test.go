package skills

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"errors"
	"fmt"
)

func loadBehavioralCases(t *testing.T) (aggregate []BehavioralEvalInput, stored []BehavioralEval, good json.RawMessage) {
	t.Helper()
	data, err := os.ReadFile("behavioral_evidence_cases.json")
	if err != nil {
		t.Fatalf("read behavioral_evidence_cases.json: %v", err)
	}
	var root struct {
		Aggregate []BehavioralEvalInput `json:"aggregate"`
		Stored    []BehavioralEval      `json:"stored"`
		Good      json.RawMessage       `json:"good"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("unmarshal behavioral_evidence_cases.json: %v", err)
	}
	return root.Aggregate, root.Stored, root.Good
}

func TestBehavioralAggregateEvidenceGate(t *testing.T) {
	ctx := context.Background()
	aggregate, _, _ := loadBehavioralCases(t)
	for i, row := range aggregate {
		t.Run(fmt.Sprintf("aggregate[%d]", i), func(t *testing.T) {
			service, _ := testService(t)
			candidate := improvementAwaitingEval(t, service)
			input := row
			input.CandidateID = candidate.ID
			input.BaseVersionID = candidate.BaseVersionID
			eval, err := service.RecordBehavioralEval(ctx, input)
			if err != nil {
				t.Fatalf("RecordBehavioralEval: %v", err)
			}
			if eval.State != EvalInconclusive {
				t.Fatalf("state = %q, want inconclusive", eval.State)
			}
			if err := service.requireCurrentBehavioralEval(ctx, candidate); !errors.Is(err, ErrBehavioralEvalRequired) {
				t.Fatalf("requireCurrentBehavioralEval: %v", err)
			}
			if _, err := service.PromoteCandidate(ctx, candidate.ID, "test", candidate.Revision); !errors.Is(err, ErrBehavioralEvalRequired) {
				t.Fatalf("PromoteCandidate: %v", err)
			}
			skill, err := service.GetSkill(ctx, candidate.TargetSkillID)
			if err != nil {
				t.Fatalf("GetSkill: %v", err)
			}
			if skill.CurrentVersionID != candidate.BaseVersionID {
				t.Fatalf("CurrentVersionID = %q, want %q", skill.CurrentVersionID, candidate.BaseVersionID)
			}
		})
	}
}

func TestBehavioralHistoricalEvidenceGate(t *testing.T) {
	ctx := context.Background()
	_, stored, _ := loadBehavioralCases(t)
	for i, row := range stored {
		t.Run(fmt.Sprintf("stored[%d]", i), func(t *testing.T) {
			service, _ := testService(t)
			candidate := improvementAwaitingEval(t, service)
			input := BehavioralEvalInput{
				CandidateID:     candidate.ID,
				BaseVersionID:   candidate.BaseVersionID,
				Tasks:           row.Tasks,
				BaselinePassed:  row.BaselinePassed,
				CandidatePassed: row.CandidatePassed,
			}
			eval, err := service.RecordBehavioralEval(ctx, input)
			if err != nil {
				t.Fatalf("RecordBehavioralEval: %v", err)
			}
			if eval.ID == "" {
				t.Fatalf("eval.ID empty")
			}
			res, err := service.store.DB.ExecContext(ctx, `UPDATE candidate_behavioral_evals
			SET state=?, tasks=?, baseline_passed=?, candidate_passed=?, regressions=?, result_json=?
			WHERE id=?`, row.State, row.Tasks, row.BaselinePassed, row.CandidatePassed, row.Regressions, string(row.Result), eval.ID)
			if err != nil {
				t.Fatalf("ExecContext: %v", err)
			}
			affected, err := res.RowsAffected()
			if err != nil {
				t.Fatalf("RowsAffected: %v", err)
			}
			if affected != 1 {
				t.Fatalf("RowsAffected: got %d, want 1", affected)
			}
			if err := service.requireCurrentBehavioralEval(ctx, candidate); !errors.Is(err, ErrBehavioralEvalRequired) {
				t.Fatalf("requireCurrentBehavioralEval: %v", err)
			}
			if _, err := service.PromoteCandidate(ctx, candidate.ID, "test", candidate.Revision); !errors.Is(err, ErrBehavioralEvalRequired) {
				t.Fatalf("PromoteCandidate: %v", err)
			}
		})
	}
}

func TestBehavioralPairedEvidencePromotion(t *testing.T) {
	ctx := context.Background()
	_, _, good := loadBehavioralCases(t)
	service, _ := testService(t)
	candidate := improvementAwaitingEval(t, service)
	eval, err := service.RecordBehavioralEval(ctx, BehavioralEvalInput{
		CandidateID:     candidate.ID,
		BaseVersionID:   candidate.BaseVersionID,
		Tasks:           5,
		BaselinePassed:  0,
		CandidatePassed: 0,
		Result:          good,
	})
	if err != nil {
		t.Fatalf("RecordBehavioralEval: %v", err)
	}
	if eval.Tasks != 5 || eval.BaselinePassed != 5 || eval.CandidatePassed != 5 || eval.Regressions != 0 {
		t.Fatalf("derived eval = %+v", eval)
	}
	if eval.State != EvalPassed {
		t.Fatalf("state = %q, want passed", eval.State)
	}
	if err := service.requireCurrentBehavioralEval(ctx, candidate); err != nil {
		t.Fatalf("requireCurrentBehavioralEval: %v", err)
	}
	skill, err := service.PromoteCandidate(ctx, candidate.ID, "test", candidate.Revision)
	if err != nil {
		t.Fatalf("PromoteCandidate: %v", err)
	}
	if skill.CurrentVersionID == candidate.BaseVersionID {
		t.Fatalf("CurrentVersionID unchanged: %q", skill.CurrentVersionID)
	}
}

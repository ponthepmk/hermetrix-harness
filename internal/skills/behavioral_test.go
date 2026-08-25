package skills

import (
	"context"
	"errors"

	"testing"
)

// improvementAwaitingEval builds a promoted skill and an improvement candidate
// that has cleared every other gate, so the only thing left between it and
// promotion is the behavioral evaluation.
func improvementAwaitingEval(t *testing.T, service *Service) Candidate {
	t.Helper()
	ctx := context.Background()
	base, err := service.CreateCandidate(ctx, candidateInput("graded-skill", "A graded procedure",
		"# Base\nDo the safe thing."))
	if err != nil {
		t.Fatal(err)
	}
	skill, err := service.PromoteCandidate(ctx, base.ID, "user", base.Revision)
	if err != nil {
		t.Fatal(err)
	}
	input := candidateInput("graded-skill", "A graded procedure", "# Revised\nDo the safer thing.")
	input.ChangeKind, input.TargetSkillID, input.BaseVersionID = "improve", skill.ID, skill.CurrentVersionID
	candidate, err := service.CreateCandidate(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Checks.ReplayRequired {
		t.Fatal("premise broken: an improvement should require replay, and with it the behavioral gate")
	}
	return candidate
}

// TestPromotionRefusesACandidateThatWasNeverEvaluated closes the Phase 8 gate:
// the promotion API rejects a candidate whose evaluation state is not_run or
// inconclusive, and this is the test that proves the refusal.
//
// Promotion already checked candidate state, revision, the check set,
// deterministic replay and capability review. None of those answered whether an
// agent following the new version still does the work: replay compares lexical
// fixtures, so it catches a Skill whose wording drifted rather than one whose
// procedure got worse.
func TestPromotionRefusesACandidateThatWasNeverEvaluated(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	candidate := improvementAwaitingEval(t, service)

	state, err := service.LatestBehavioralEval(ctx, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.State != EvalNotRun {
		t.Fatalf("state = %q, want not_run", state.State)
	}
	if _, err := service.PromoteCandidate(ctx, candidate.ID, "user", candidate.Revision); !errors.Is(err,
		ErrBehavioralEvalRequired) {
		t.Fatalf("an unevaluated improvement was promoted: %v", err)
	}
}

// An evaluation that could not decide is not evidence the candidate is safe.
// Both ways a run comes back uninformative -- a provider that kept failing, and
// a sample too small to separate a regression from noise -- have to block.
func TestAnInconclusiveEvaluationDoesNotUnblockPromotion(t *testing.T) {
	ctx := context.Background()
	for _, testCase := range []struct {
		name  string
		input BehavioralEvalInput
	}{
		{"provider kept failing", BehavioralEvalInput{Tasks: 20, BaselinePassed: 20, CandidatePassed: 20,
			Error: "provider returned HTTP 429"}},
		{"sample too small", BehavioralEvalInput{Tasks: MinimumBehavioralTasks - 1,
			BaselinePassed: MinimumBehavioralTasks - 1, CandidatePassed: MinimumBehavioralTasks - 1}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			service, _ := testService(t)
			candidate := improvementAwaitingEval(t, service)
			input := testCase.input
			input.CandidateID = candidate.ID
			eval, err := service.RecordBehavioralEval(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if eval.State != EvalInconclusive {
				t.Fatalf("state = %q, want inconclusive", eval.State)
			}
			if _, err := service.PromoteCandidate(ctx, candidate.ID, "user",
				candidate.Revision); !errors.Is(err, ErrBehavioralEvalRequired) {
				t.Fatalf("an inconclusive evaluation unblocked promotion: %v", err)
			}
		})
	}
}

// The verdict is derived from the numbers, not accepted from the caller, so a
// runner cannot report a pass beside a regression.
func TestARegressionOrAFalseSuccessFailsTheEvaluation(t *testing.T) {
	ctx := context.Background()
	for _, testCase := range []struct {
		name  string
		input BehavioralEvalInput
	}{
		{"did the work worse", BehavioralEvalInput{Tasks: 20, BaselinePassed: 18, CandidatePassed: 15}},
		{"claimed work it did not do", BehavioralEvalInput{Tasks: 20, BaselinePassed: 18,
			CandidatePassed: 18, FalseSuccessDelta: 1}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			service, _ := testService(t)
			candidate := improvementAwaitingEval(t, service)
			input := testCase.input
			input.CandidateID = candidate.ID
			eval, err := service.RecordBehavioralEval(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if eval.State != EvalFailed {
				t.Fatalf("state = %q, want failed", eval.State)
			}
			if _, err := service.PromoteCandidate(ctx, candidate.ID, "user",
				candidate.Revision); !errors.Is(err, ErrBehavioralEvalRequired) {
				t.Fatalf("a failing evaluation unblocked promotion: %v", err)
			}
		})
	}
}

// An evaluation describes the candidate it read. Editing the candidate
// afterwards leaves an evaluation of something that no longer exists, and
// inheriting it would let any change ride in on an older version's result.
//
// The binding is asserted directly rather than through PromoteCandidate,
// because an edited candidate leaves needs_review and is refused earlier for
// that reason -- which would make this test pass without ever exercising the
// binding it is about.
func TestAnEvaluationDoesNotSurviveEditingTheCandidate(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	candidate := improvementAwaitingEval(t, service)
	passBehavioralEval(t, service, candidate.ID)

	// Premise: as it stands, the evaluation satisfies the gate.
	if err := service.requireCurrentBehavioralEval(ctx, candidate); err != nil {
		t.Fatalf("a fresh passing evaluation did not satisfy the gate: %v", err)
	}

	edited, err := service.UpdateCandidate(ctx, candidate.ID, UpdateCandidateInput{
		Markdown:         "# Revised\nDo something different.",
		Actor:            "user",
		ExpectedRevision: candidate.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Revision == candidate.Revision && edited.CandidateHash == candidate.CandidateHash {
		t.Fatal("premise broken: editing changed neither the revision nor the hash")
	}
	if err := service.requireCurrentBehavioralEval(ctx, edited); !errors.Is(err,
		ErrBehavioralEvalRequired) {
		t.Fatalf("an evaluation of the previous revision still satisfied the gate: %v", err)
	}
	// And the stored evaluation is still there, still passing -- it is the
	// binding that refuses it, not a deletion.
	stored, err := service.LatestBehavioralEval(ctx, candidate.ID)
	if err != nil || stored.State != EvalPassed {
		t.Fatalf("stored evaluation = %+v err=%v", stored, err)
	}
}

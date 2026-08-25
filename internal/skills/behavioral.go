package skills

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"hermetrix-harness/internal/identity"
)

// The Phase 8 gate reads: the promotion API refuses a candidate whose
// behavioral evaluation state is not_run or inconclusive, and a test proves the
// refusal.
//
// Until now Promote checked five things -- candidate state, revision, the
// check set, deterministic replay, and capability review -- and none of them
// answered whether an agent following the new version still does the work. The
// audit recorded replay as "lifecycle gate จริง แต่ไม่ใช่ agent/tool behavioral
// eval": replay compares lexical fixtures, which catches a Skill whose wording
// drifted, not one whose procedure got worse.
//
// This is the missing gate, bound the same way replay is: to an exact candidate
// revision and content hash, so an evaluation cannot be inherited by a later
// edit of the same candidate.
const behavioralRunnerRevision = "hermetrix-behavioral-eval-v1"

// Evaluation states. Anything that is not EvalPassed blocks promotion.
const (
	// EvalNotRun means no evaluation exists for this exact candidate revision.
	EvalNotRun = "not_run"
	// EvalPassed means the candidate did the work at least as well as the
	// version it would replace.
	EvalPassed = "passed"
	// EvalFailed means it did the work worse.
	EvalFailed = "failed"
	// EvalInconclusive means the run could not decide -- too few tasks, or the
	// provider failed often enough that the numbers mean nothing. It is
	// deliberately not a pass: an evaluation that could not read the candidate
	// is not evidence the candidate is safe.
	EvalInconclusive = "inconclusive"
)

// ErrBehavioralEvalRequired is returned by promotion when no passing
// evaluation is bound to the exact candidate being promoted.
var ErrBehavioralEvalRequired = errors.New("candidate behavioral evaluation is missing, stale or not passing")

// BehavioralEval records one evaluation of a candidate against the version it
// would replace.
type BehavioralEval struct {
	ID                string          `json:"id"`
	CandidateID       string          `json:"candidate_id"`
	CandidateRevision int             `json:"candidate_revision"`
	CandidateHash     string          `json:"candidate_hash"`
	BaseVersionID     string          `json:"base_version_id,omitempty"`
	RunnerRevision    string          `json:"runner_revision"`
	State             string          `json:"state"`
	Tasks             int             `json:"tasks"`
	BaselinePassed    int             `json:"baseline_passed"`
	CandidatePassed   int             `json:"candidate_passed"`
	Regressions       int             `json:"regressions"`
	FalseSuccessDelta int             `json:"false_success_delta"`
	Result            json.RawMessage `json:"result,omitempty"`
	Error             string          `json:"error,omitempty"`
	StartedAt         time.Time       `json:"started_at"`
	CompletedAt       *time.Time      `json:"completed_at,omitempty"`
}

// BehavioralEvalInput is what a runner reports back after evaluating a
// candidate. The verdict is derived here rather than supplied, so a caller
// cannot hand in "passed" alongside numbers that say otherwise.
type BehavioralEvalInput struct {
	CandidateID       string          `json:"-"`
	BaseVersionID     string          `json:"base_version_id,omitempty"`
	Tasks             int             `json:"tasks"`
	BaselinePassed    int             `json:"baseline_passed"`
	CandidatePassed   int             `json:"candidate_passed"`
	FalseSuccessDelta int             `json:"false_success_delta"`
	Result            json.RawMessage `json:"result,omitempty"`
	Error             string          `json:"error,omitempty"`
}

// MinimumBehavioralTasks is the sample below which a run cannot conclude
// anything. One task agreeing proves nothing about a procedure.
const MinimumBehavioralTasks = 5

// RecordBehavioralEval stores an evaluation against the candidate as it stands
// right now. If the candidate has been edited since the run started, the
// evaluation describes something that no longer exists and is refused.
func (s *Service) RecordBehavioralEval(ctx context.Context, input BehavioralEvalInput) (BehavioralEval, error) {
	candidate, err := s.GetCandidate(ctx, input.CandidateID)
	if err != nil {
		return BehavioralEval{}, err
	}
	now := time.Now().UTC()
	eval := BehavioralEval{ID: identity.New("beval"), CandidateID: candidate.ID,
		CandidateRevision: candidate.Revision, CandidateHash: candidate.CandidateHash,
		BaseVersionID: input.BaseVersionID, RunnerRevision: behavioralRunnerRevision,
		Tasks: input.Tasks, BaselinePassed: input.BaselinePassed, CandidatePassed: input.CandidatePassed,
		FalseSuccessDelta: input.FalseSuccessDelta, Result: input.Result, Error: input.Error,
		StartedAt: now, CompletedAt: &now}
	eval.Regressions = input.BaselinePassed - input.CandidatePassed
	if eval.Regressions < 0 {
		eval.Regressions = 0
	}
	eval.State = behavioralVerdict(eval)
	result := eval.Result
	if len(result) == 0 {
		result = json.RawMessage("{}")
	}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO candidate_behavioral_evals(id,candidate_id,
      candidate_revision,candidate_hash,base_version_id,runner_revision,state,tasks,baseline_passed,
      candidate_passed,regressions,false_success_delta,result_json,error,started_at,completed_at)
      VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, eval.ID, eval.CandidateID, eval.CandidateRevision,
		eval.CandidateHash, eval.BaseVersionID, eval.RunnerRevision, eval.State, eval.Tasks,
		eval.BaselinePassed, eval.CandidatePassed, eval.Regressions, eval.FalseSuccessDelta,
		string(result), eval.Error, formatTime(eval.StartedAt), formatTime(*eval.CompletedAt))
	if err != nil {
		return BehavioralEval{}, err
	}
	return eval, nil
}

// behavioralVerdict is deliberately conservative in both directions a run can
// be uninformative: a provider that kept failing, and a sample too small to
// separate a real regression from noise. Neither is a pass.
func behavioralVerdict(eval BehavioralEval) string {
	switch {
	case eval.Error != "":
		return EvalInconclusive
	case eval.Tasks < MinimumBehavioralTasks:
		return EvalInconclusive
	case eval.FalseSuccessDelta > 0:
		// Claiming the work was done while it was not is worse than doing it
		// worse, and carries no tolerance anywhere else in this system either.
		return EvalFailed
	case eval.Regressions > 0:
		return EvalFailed
	default:
		return EvalPassed
	}
}

// LatestBehavioralEval returns the most recent evaluation for a candidate, or
// a not_run placeholder when there is none. Callers get a state to show rather
// than an error to special-case.
func (s *Service) LatestBehavioralEval(ctx context.Context, candidateID string) (BehavioralEval, error) {
	row := s.store.DB.QueryRowContext(ctx, `SELECT id,candidate_id,candidate_revision,candidate_hash,
      base_version_id,runner_revision,state,tasks,baseline_passed,candidate_passed,regressions,
      false_success_delta,result_json,error,started_at,completed_at FROM candidate_behavioral_evals
      WHERE candidate_id=? ORDER BY started_at DESC LIMIT 1`, candidateID)
	var eval BehavioralEval
	var result, started string
	var completed sql.NullString
	err := row.Scan(&eval.ID, &eval.CandidateID, &eval.CandidateRevision, &eval.CandidateHash,
		&eval.BaseVersionID, &eval.RunnerRevision, &eval.State, &eval.Tasks, &eval.BaselinePassed,
		&eval.CandidatePassed, &eval.Regressions, &eval.FalseSuccessDelta, &result, &eval.Error,
		&started, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return BehavioralEval{CandidateID: candidateID, State: EvalNotRun}, nil
	}
	if err != nil {
		return BehavioralEval{}, err
	}
	eval.Result = json.RawMessage(result)
	eval.StartedAt, _ = parseTime(started)
	if completed.Valid {
		value, _ := parseTime(completed.String)
		eval.CompletedAt = &value
	}
	return eval, nil
}

// requireCurrentBehavioralEval is the promotion gate.
//
// The binding is exact, for the same reason replay's is: an evaluation of
// revision 3 says nothing about revision 4, and a candidate whose content
// changed under the same revision number is a different candidate. Anything
// short of a passing evaluation of exactly this candidate blocks promotion.
func (s *Service) requireCurrentBehavioralEval(ctx context.Context, candidate Candidate) error {
	eval, err := s.LatestBehavioralEval(ctx, candidate.ID)
	if err != nil {
		return err
	}
	if eval.State != EvalPassed || eval.CandidateHash != candidate.CandidateHash ||
		eval.CandidateRevision != candidate.Revision {
		return fmt.Errorf("%w: state %s", ErrBehavioralEvalRequired, eval.State)
	}
	return nil
}

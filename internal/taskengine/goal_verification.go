package taskengine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// GoalVerification is a bounded checkpoint, not permission to replay a command.
// Only the current goal action claim can advance it. Progress contains durable
// effect/job IDs; process results remain in their original managed-job evidence.
type GoalVerification struct {
	ProposalID string          `json:"proposal_id"`
	GoalID     string          `json:"goal_id"`
	Revision   int             `json:"revision"`
	State      string          `json:"state"`
	Progress   json.RawMessage `json:"progress"`
}

func (s *Service) GoalVerification(ctx context.Context, goalID, proposalID string) (GoalVerification, error) {
	if _, err := s.Goal(ctx, goalID); err != nil {
		return GoalVerification{}, err
	}
	var v GoalVerification
	var body string
	err := s.store.DB.QueryRowContext(ctx, `SELECT proposal_id,goal_id,revision,state,progress_json FROM task_goal_verifications WHERE goal_id=? AND proposal_id=?`, goalID, proposalID).Scan(&v.ProposalID, &v.GoalID, &v.Revision, &v.State, &body)
	v.Progress = json.RawMessage(body)
	return v, err
}

func (s *Service) SaveGoalVerification(ctx context.Context, g GoalRun, proposalID string, revision int, state string, progress []byte) (GoalVerification, error) {
	if g.ClaimToken == "" || len(progress) > 1<<20 || !json.Valid(progress) || (state != "active" && state != "completed" && state != "failed") {
		return GoalVerification{}, fmt.Errorf("verification requires an action claim and bounded valid progress")
	}
	if _, err := s.Goal(ctx, g.ID); err != nil {
		return GoalVerification{}, err
	}
	now := formatTime(time.Now().UTC())
	var n int64
	if revision == 0 {
		result, err := s.store.DB.ExecContext(ctx, `INSERT INTO task_goal_verifications(proposal_id,goal_id,revision,state,progress_json,updated_at)
   SELECT p.id,g.id,1,?,?,? FROM task_goal_runs g JOIN task_code_proposals p ON p.task_id=g.task_id
   WHERE g.id=? AND g.claim_token=? AND g.state='active' AND p.id=? AND p.state='applied'`, state, string(progress), now, g.ID, g.ClaimToken, proposalID)
		if err != nil {
			return GoalVerification{}, err
		}
		n, err = result.RowsAffected()
		if err != nil {
			return GoalVerification{}, err
		}
	} else {
		result, err := s.store.DB.ExecContext(ctx, `UPDATE task_goal_verifications SET revision=revision+1,state=?,progress_json=?,updated_at=?
   WHERE proposal_id=? AND goal_id=? AND revision=? AND state='active' AND EXISTS(SELECT 1 FROM task_goal_runs WHERE id=? AND claim_token=? AND state='active')`, state, string(progress), now, proposalID, g.ID, revision, g.ID, g.ClaimToken)
		if err != nil {
			return GoalVerification{}, err
		}
		n, err = result.RowsAffected()
		if err != nil {
			return GoalVerification{}, err
		}
	}
	if n != 1 {
		return GoalVerification{}, ErrStaleRevision
	}
	return s.GoalVerification(ctx, g.ID, proposalID)
}

// DeferGoalPoll schedules observation without charging an action/attempt or
// occupying a worker. A deadline always remains immediately eligible.
func (s *Service) DeferGoalPoll(ctx context.Context, id string) error {
	if _, err := s.Goal(ctx, id); err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err := s.store.DB.ExecContext(ctx, `UPDATE task_goal_runs SET poll_after=?,updated_at=? WHERE id=? AND state='active' AND claim_token=''`, formatTime(now.Add(time.Second)), formatTime(now), id)
	return err
}

func (s *Service) VerificationEffectsMatch(ctx context.Context, taskID, operationID string) (bool, error) {
	if _, err := s.Get(ctx, taskID); err != nil {
		return false, err
	}
	var n int
	err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_effect_intents WHERE task_id=? AND state IN ('planned','dispatched','uncertain') AND (operation_id<>? OR state='uncertain')`, taskID, operationID).Scan(&n)
	return n == 0, err
}

func (s *Service) HasGoalVerification(ctx context.Context, proposalID string) (bool, error) {
	if _, err := s.GetCodeProposal(ctx, proposalID); err != nil {
		return false, err
	}
	var n int
	err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_goal_verifications WHERE proposal_id=?`, proposalID).Scan(&n)
	return n > 0, err
}

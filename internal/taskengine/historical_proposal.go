package taskengine

import (
	"context"
	"database/sql"
	"errors"
)

// FailedProposalForLog returns the verification-failed code proposal linked to the
// most recent step failure for the given task, step, and artifact. It is a read-only
// diagnostic lookup and does not load source, parse timestamps, or mutate the database.
func (s *Service) FailedProposalForLog(ctx context.Context, taskID, stepID, artifactID string) (CodeProposal, bool, error) {
	if taskID == "" || stepID == "" || artifactID == "" {
		return CodeProposal{}, false, nil
	}

	const query = `SELECT p.id
	  FROM task_code_proposals AS p
	  JOIN task_step_failures AS f
	    ON f.attempt_id = p.attempt_id
	   AND f.task_id = p.task_id
	   AND f.step_id = p.step_id
	 WHERE p.state = ?
	   AND f.diff_artifact_id = p.artifact_id
	   AND f.task_id = ?
	   AND f.step_id = ?
	   AND EXISTS (
	         SELECT 1
	           FROM json_each(f.evidence_refs_json) AS e
	          WHERE e.value = ?
       )
	 ORDER BY f.created_at DESC, f.id DESC
	 LIMIT 1`

	var id string
	row := s.store.DB.QueryRowContext(ctx, query, ProposalVerifyFailed, taskID, stepID, "artifact:"+artifactID)
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CodeProposal{}, false, nil
		}
		return CodeProposal{}, false, err
	}

	proposal, err := s.GetCodeProposal(ctx, id)
	if err != nil {
		return CodeProposal{}, false, err
	}
	if proposal.TaskID != taskID || proposal.StepID != stepID || proposal.State != ProposalVerifyFailed {
		return CodeProposal{}, false, errors.New("proposal verification mismatch")
	}
	return proposal, true, nil
}

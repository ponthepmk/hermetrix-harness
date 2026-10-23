package taskengine

import "context"

type ReviewFailureRef struct {
	ArtifactID string
	ProposalID string
	StepID     string
	PacketHash string
}

// RejectedReviewEvidence returns observed review receipts for the active
// requirements, so an owner revision cannot inherit stale reviewer findings.
func (s *Service) RejectedReviewEvidence(ctx context.Context, taskID string) ([]ReviewFailureRef, error) {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT json_extract(e.receipt_json,'$.artifact_id'),p.id,p.step_id,p.packet_hash
		FROM task_effect_intents e JOIN task_code_proposals p ON p.attempt_id=e.attempt_id
		JOIN task_steps st ON st.id=p.step_id JOIN task_plan_revisions pr ON pr.task_id=st.task_id AND pr.revision=st.plan_revision
		WHERE p.task_id=? AND pr.requirement_revision=? AND e.action='provider.review' AND e.state IN ('observed','reconciled')
		AND json_extract(e.receipt_json,'$.verdict')='reject' AND json_extract(e.receipt_json,'$.artifact_id') IS NOT NULL
		ORDER BY e.rowid DESC LIMIT 2`, task.ID, task.ActiveRequirementRevision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ReviewFailureRef
	for rows.Next() {
		var item ReviewFailureRef
		if err = rows.Scan(&item.ArtifactID, &item.ProposalID, &item.StepID, &item.PacketHash); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

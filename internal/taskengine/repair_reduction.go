package taskengine

import (
	"context"
	"encoding/json"
	"fmt"
)

// UsedRepairReductionTargets records host derivations, not provider usage. The
// source proposal has at most four eligible targets. A target is never retried
// after a durable derivation, even if a later check or review rejects it.
func (s *Service) UsedRepairReductionTargets(ctx context.Context, taskID, sourceProposalID string) ([]string, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT receipt_json FROM task_effect_intents
		WHERE task_id=? AND action='proposal.reduce' AND state IN ('observed','reconciled')
		AND json_extract(receipt_json,'$.source_proposal_id')=? ORDER BY created_at,id LIMIT 5`, taskID, sourceProposalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var body string
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		var receipt struct {
			TargetID string `json:"target_id"`
		}
		if err = json.Unmarshal([]byte(body), &receipt); err != nil || len(receipt.TargetID) != 64 {
			return nil, fmt.Errorf("invalid reduction history")
		}
		result = append(result, receipt.TargetID)
	}
	return result, rows.Err()
}

// ProposalFailureRefs returns only evidence linked to this failed proposal's
// registered source artifact. Callers still verify actual artifact checksums.
func (s *Service) ProposalFailureRefs(ctx context.Context, p CodeProposal) ([]string, error) {
	var body string
	err := s.store.DB.QueryRowContext(ctx, `SELECT evidence_refs_json FROM task_step_failures
		WHERE task_id=? AND step_id=? AND attempt_id=? AND diff_artifact_id=?
		ORDER BY created_at DESC,id DESC LIMIT 1`, p.TaskID, p.StepID, p.AttemptID, p.ArtifactID).Scan(&body)
	if err != nil {
		return nil, err
	}
	var refs []string
	if err = json.Unmarshal([]byte(body), &refs); err != nil {
		return nil, err
	}
	return refs, nil
}

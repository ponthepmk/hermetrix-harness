package taskengine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

const (
	ProposalPendingReview    = "pending_review"
	ProposalApproved         = "approved"
	ProposalRejected         = "rejected"
	ProposalApplying         = "applying"
	ProposalApplied          = "applied"
	ProposalApplyFailed      = "apply_failed"
	ProposalAwaitingReview   = "awaiting_post_review"
	ProposalVerified         = "verified"
	ProposalVerifyFailed     = "verification_failed"
	ProposalReviewRejected   = "post_review_rejected"
	ProposalRecoveryRequired = "recovery_required"
)

type CodeProposal struct {
	ID                     string    `json:"id"`
	TaskID                 string    `json:"task_id"`
	StepID                 string    `json:"step_id"`
	AttemptID              string    `json:"attempt_id"`
	ProjectID              string    `json:"project_id"`
	PacketHash             string    `json:"packet_hash"`
	ProviderID             string    `json:"provider_id"`
	ProviderRevision       string    `json:"provider_revision"`
	ArtifactID             string    `json:"artifact_id"`
	RollbackArtifactID     string    `json:"rollback_artifact_id,omitempty"`
	VerificationArtifactID string    `json:"verification_artifact_id,omitempty"`
	ResultHash             string    `json:"result_hash"`
	State                  string    `json:"state"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

func (s *Service) BeginCodeProposalApply(ctx context.Context, proposalID, rollbackArtifactID string) (CodeProposal, error) {
	if strings.TrimSpace(rollbackArtifactID) == "" {
		return CodeProposal{}, fmt.Errorf("rollback artifact is required before apply")
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE task_code_proposals
		SET state='applying',rollback_artifact_id=?,updated_at=? WHERE id=? AND state='approved'`,
		rollbackArtifactID, formatTime(time.Now().UTC()), proposalID)
	if err != nil {
		return CodeProposal{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return CodeProposal{}, fmt.Errorf("proposal apply binding is stale or invalid")
	}
	return s.GetCodeProposal(ctx, proposalID)
}

func (s *Service) BindCodeProposalVerification(ctx context.Context, proposalID, verificationArtifactID string) (CodeProposal, error) {
	if strings.TrimSpace(verificationArtifactID) == "" {
		return CodeProposal{}, fmt.Errorf("verification artifact is required before post-review")
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE task_code_proposals
		SET state='awaiting_post_review',verification_artifact_id=?,updated_at=? WHERE id=? AND state='applied'`,
		verificationArtifactID, formatTime(time.Now().UTC()), proposalID)
	if err != nil {
		return CodeProposal{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return CodeProposal{}, fmt.Errorf("proposal verification binding is stale or invalid")
	}
	return s.GetCodeProposal(ctx, proposalID)
}

func (s *Service) TransitionCodeProposal(ctx context.Context, proposalID, from, to string) (CodeProposal, error) {
	allowed := (from == ProposalApproved && to == ProposalApplying) ||
		(from == ProposalApplying && (to == ProposalApplied || to == ProposalApplyFailed)) ||
		(from == ProposalApplied && (to == ProposalAwaitingReview || to == ProposalVerifyFailed)) ||
		(from == ProposalAwaitingReview && (to == ProposalVerified || to == ProposalReviewRejected))
	if !allowed {
		return CodeProposal{}, fmt.Errorf("invalid proposal transition %s -> %s", from, to)
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE task_code_proposals SET state=?,updated_at=? WHERE id=? AND state=?`,
		to, formatTime(time.Now().UTC()), proposalID, from)
	if err != nil {
		return CodeProposal{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return CodeProposal{}, fmt.Errorf("proposal transition is stale or invalid")
	}
	return s.GetCodeProposal(ctx, proposalID)
}

func (s *Service) RecordPostCodeReview(ctx context.Context, proposalID, reviewer, verdict, rationale string, findings []string) (CodeReview, error) {
	reviewer, verdict, rationale = strings.TrimSpace(reviewer), strings.TrimSpace(verdict), strings.TrimSpace(rationale)
	if reviewer == "" || rationale == "" || (verdict != ProposalApproved && verdict != ProposalRejected) {
		return CodeReview{}, fmt.Errorf("post-review requires reviewer, rationale and approve/reject verdict")
	}
	var state string
	if err := s.store.DB.QueryRowContext(ctx, `SELECT state FROM task_code_proposals WHERE id=?`, proposalID).Scan(&state); err != nil {
		return CodeReview{}, err
	}
	if state != ProposalAwaitingReview {
		return CodeReview{}, fmt.Errorf("proposal cannot receive post-review from state %s", state)
	}
	now := time.Now().UTC()
	item := CodeReview{ID: identity.New("codereview"), ProposalID: proposalID, Reviewer: reviewer, Verdict: verdict,
		Rationale: rationale, Findings: cleanStrings(findings), CreatedAt: now}
	encoded, _ := json.Marshal(item.Findings)
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO task_code_reviews(id,proposal_id,reviewer,verdict,rationale,findings_json,created_at)
		VALUES(?,?,?,?,?,?,?)`, item.ID, item.ProposalID, item.Reviewer, item.Verdict, item.Rationale, string(encoded), formatTime(now))
	return item, err
}

type CodeReview struct {
	ID         string    `json:"id"`
	ProposalID string    `json:"proposal_id"`
	Reviewer   string    `json:"reviewer"`
	Verdict    string    `json:"verdict"`
	Rationale  string    `json:"rationale"`
	Findings   []string  `json:"findings"`
	CreatedAt  time.Time `json:"created_at"`
}

type RecordCodeProposalInput struct {
	ID, TaskID, StepID, AttemptID, ProjectID, PacketHash string
	ProviderID, ProviderRevision, ArtifactID, ResultHash string
}

func (s *Service) RecordCodeProposal(ctx context.Context, input RecordCodeProposalInput) (CodeProposal, error) {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.ArtifactID) == "" || strings.TrimSpace(input.ResultHash) == "" {
		return CodeProposal{}, fmt.Errorf("proposal id, artifact and result hash are required")
	}
	var taskID, stepID, state string
	if err := s.store.DB.QueryRowContext(ctx, `SELECT task_id,step_id,state FROM task_step_attempts WHERE id=?`, input.AttemptID).
		Scan(&taskID, &stepID, &state); err != nil {
		return CodeProposal{}, err
	}
	if state != AttemptRunning || taskID != input.TaskID || stepID != input.StepID {
		return CodeProposal{}, fmt.Errorf("proposal does not match a running attempt")
	}
	now := time.Now().UTC()
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO task_code_proposals
		(id,task_id,step_id,attempt_id,project_id,packet_hash,provider_id,provider_revision,artifact_id,result_hash,state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,'pending_review',?,?)`, input.ID, input.TaskID, input.StepID, input.AttemptID,
		input.ProjectID, input.PacketHash, input.ProviderID, input.ProviderRevision, input.ArtifactID, input.ResultHash,
		formatTime(now), formatTime(now))
	if err != nil {
		return CodeProposal{}, err
	}
	return s.GetCodeProposal(ctx, input.ID)
}

func (s *Service) DecideCodeProposal(ctx context.Context, proposalID, reviewer, verdict, rationale string, findings []string) (CodeProposal, error) {
	reviewer, verdict, rationale = strings.TrimSpace(reviewer), strings.TrimSpace(verdict), strings.TrimSpace(rationale)
	if reviewer == "" || rationale == "" || (verdict != ProposalApproved && verdict != ProposalRejected) {
		return CodeProposal{}, fmt.Errorf("reviewer, rationale and approve/reject verdict are required")
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return CodeProposal{}, err
	}
	defer tx.Rollback()
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM task_code_proposals WHERE id=?`, proposalID).Scan(&state); err != nil {
		if err == sql.ErrNoRows {
			return CodeProposal{}, ErrNotFound
		}
		return CodeProposal{}, err
	}
	if state != ProposalPendingReview {
		return CodeProposal{}, fmt.Errorf("proposal cannot be reviewed from state %s", state)
	}
	now := time.Now().UTC()
	encoded, _ := json.Marshal(cleanStrings(findings))
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_code_reviews(id,proposal_id,reviewer,verdict,rationale,findings_json,created_at)
		VALUES(?,?,?,?,?,?,?)`, identity.New("codereview"), proposalID, reviewer, verdict, rationale, string(encoded), formatTime(now)); err != nil {
		return CodeProposal{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE task_code_proposals SET state=?,updated_at=? WHERE id=? AND state='pending_review'`, verdict, formatTime(now), proposalID)
	if err != nil {
		return CodeProposal{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return CodeProposal{}, fmt.Errorf("proposal review raced with another decision")
	}
	if err = tx.Commit(); err != nil {
		return CodeProposal{}, err
	}
	return s.GetCodeProposal(ctx, proposalID)
}

func (s *Service) GetCodeProposal(ctx context.Context, id string) (CodeProposal, error) {
	var item CodeProposal
	var created, updated string
	err := s.store.DB.QueryRowContext(ctx, `SELECT id,task_id,step_id,attempt_id,project_id,packet_hash,provider_id,
		provider_revision,artifact_id,result_hash,COALESCE(rollback_artifact_id,''),COALESCE(verification_artifact_id,''),state,created_at,updated_at FROM task_code_proposals WHERE id=?`, id).
		Scan(&item.ID, &item.TaskID, &item.StepID, &item.AttemptID, &item.ProjectID, &item.PacketHash, &item.ProviderID,
			&item.ProviderRevision, &item.ArtifactID, &item.ResultHash, &item.RollbackArtifactID, &item.VerificationArtifactID,
			&item.State, &created, &updated)
	if err == sql.ErrNoRows {
		return item, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

package agentplatform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hermetrix-harness/internal/taskengine"
)

func (s *Service) Intake(ctx context.Context, raw []byte) (AssignmentReceipt, error) {
	// An authenticated exact replay of an already-admitted immutable payload is
	// historical recovery, not a fresh authority grant. Permit it after claim
	// expiry without weakening validation for a new assignment.
	envelope, err := VerifyEnvelope(raw, s.trust, "TaskAssignment", TaskAssignmentRevision)
	if err != nil {
		return AssignmentReceipt{}, err
	}
	if err = validateSchema("task_assignment", envelope.Payload); err != nil {
		return AssignmentReceipt{}, fmt.Errorf("%w: assignment schema: %v", ErrValidation, err)
	}
	var assignment TaskAssignment
	if err = decodeStrict(envelope.Payload, &assignment); err != nil {
		return AssignmentReceipt{}, fmt.Errorf("%w: assignment: %v", ErrValidation, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if receipt, found, err := s.existingAssignment(ctx, assignment.AssignmentID); err != nil {
		return AssignmentReceipt{}, err
	} else if found {
		if receipt.AssignmentRevision == assignment.AssignmentRevision && receipt.PayloadDigest == envelope.PayloadDigest {
			return receipt, nil
		}
		if receipt.AssignmentRevision == assignment.AssignmentRevision {
			return AssignmentReceipt{}, ErrDigestConflict
		}
		return AssignmentReceipt{}, fmt.Errorf("%w: assignment supersession", ErrUnsupported)
	}
	if _, assignment, err = ValidateAssignment(raw, s.trust); err != nil {
		return AssignmentReceipt{}, err
	}
	binding, err := s.resolveBinding(ctx, assignment)
	if err != nil {
		return AssignmentReceipt{}, err
	}

	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return AssignmentReceipt{}, err
	}
	defer tx.Rollback()
	criteria := make([]taskengine.Criterion, 0, len(assignment.AcceptanceCriteria))
	for _, item := range assignment.AcceptanceCriteria {
		criteria = append(criteria, taskengine.Criterion{ID: item.ID, Description: item.Description})
	}
	created, err := s.tasks.CreateInTx(ctx, tx, taskengine.CreateTaskInput{ProjectID: binding.ProjectID,
		Title: assignment.Title, Objective: assignment.Goal, OriginalRequest: assignment.OriginalRequest,
		Constraints: assignment.Constraints, Criteria: criteria, Actor: "agent-platform:" + assignment.AgentID, EgressPolicy: "local_only"})
	if err != nil {
		return AssignmentReceipt{}, err
	}
	claimBytes, _ := json.Marshal(assignment.AuthorizationClaim)
	now := formatContractTime(s.trust.now())
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_platform_assignments
		(platform_id,assignment_id,assignment_revision,platform_task_id,platform_run_id,node_id,agent_id,binding_id,binding_revision,
		 harness_task_id,payload_digest,immutable_envelope,accepted_claim,access,intake_state,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,'projected',?)`, s.trust.PlatformID, assignment.AssignmentID, assignment.AssignmentRevision,
		assignment.TaskID, assignment.PlatformRunID, assignment.NodeID, assignment.AgentID, binding.ID, binding.Revision, created.TaskID,
		envelope.PayloadDigest, raw, claimBytes, assignment.AccessScope.Access, now); err != nil {
		return s.concurrentIntakeResult(ctx, tx, assignment, envelope.PayloadDigest, err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_platform_streams
		(platform_id,platform_run_id,assignment_id,assignment_revision,next_sequence,acked_sequence,updated_at)
		VALUES(?,?,?,?,1,0,?)`, s.trust.PlatformID, assignment.PlatformRunID, assignment.AssignmentID,
		assignment.AssignmentRevision, now); err != nil {
		return s.concurrentIntakeResult(ctx, tx, assignment, envelope.PayloadDigest, err)
	}
	if err = tx.Commit(); err != nil {
		return AssignmentReceipt{}, err
	}
	return AssignmentReceipt{PlatformID: s.trust.PlatformID, AssignmentID: assignment.AssignmentID,
		AssignmentRevision: assignment.AssignmentRevision, PlatformTaskID: assignment.TaskID, PlatformRunID: assignment.PlatformRunID,
		HarnessTaskID: created.TaskID, PayloadDigest: envelope.PayloadDigest, Access: assignment.AccessScope.Access,
		State: "projected", CreatedAt: mustParseTime(now)}, nil
}

func (s *Service) concurrentIntakeResult(ctx context.Context, tx *sql.Tx, assignment TaskAssignment, digest string, original error) (AssignmentReceipt, error) {
	_ = tx.Rollback()
	receipt, found, err := s.existingAssignment(ctx, assignment.AssignmentID)
	if err == nil && found && receipt.AssignmentRevision == assignment.AssignmentRevision && receipt.PayloadDigest == digest {
		return receipt, nil
	}
	if err != nil {
		return AssignmentReceipt{}, err
	}
	return AssignmentReceipt{}, original
}

func (s *Service) existingAssignment(ctx context.Context, assignmentID string) (AssignmentReceipt, bool, error) {
	var item AssignmentReceipt
	var created string
	err := s.store.DB.QueryRowContext(ctx, `SELECT platform_id,assignment_id,assignment_revision,platform_task_id,platform_run_id,
		harness_task_id,payload_digest,access,intake_state,created_at FROM agent_platform_assignments WHERE platform_id=? AND assignment_id=?`,
		s.trust.PlatformID, strings.TrimSpace(assignmentID)).Scan(&item.PlatformID, &item.AssignmentID, &item.AssignmentRevision,
		&item.PlatformTaskID, &item.PlatformRunID, &item.HarnessTaskID, &item.PayloadDigest, &item.Access, &item.State, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return AssignmentReceipt{}, false, nil
	}
	if err != nil {
		return AssignmentReceipt{}, false, err
	}
	item.CreatedAt = mustParseTime(created)
	return item, true, nil
}

func mustParseTime(value string) time.Time {
	parsed, _ := time.Parse("2006-01-02T15:04:05.000Z", value)
	return parsed
}

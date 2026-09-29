package projectbrain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrCandidateConflict = errors.New("Project Brain candidate identity conflict")
var ErrExportRevoked = errors.New("Project Brain candidate export approval is no longer valid")

// CandidateMessage is an already compiled, owner-selected proposal. Queueing
// never verifies work or grants knowledge admission; Pi reviews it separately.
type CandidateMessage struct {
	CandidateID   string
	ProjectID     string
	TaskID        string
	PayloadDigest string
	Payload       json.RawMessage
	Approval      CandidateExportApproval
}

type SubmissionReceipt struct {
	Status        string `json:"status"`
	CandidateID   string `json:"candidate_id"`
	PayloadDigest string `json:"payload_digest"`
	Commit        string `json:"commit"`
}

type CandidateSubmitter interface {
	SubmitCandidate(context.Context, json.RawMessage) (SubmissionReceipt, error)
}

type CandidateDelivery struct {
	CandidateID   string    `json:"candidate_id"`
	ProjectID     string    `json:"project_id"`
	TaskID        string    `json:"task_id"`
	PayloadDigest string    `json:"payload_digest"`
	State         string    `json:"state"`
	Attempts      int       `json:"attempts"`
	LastError     string    `json:"last_error,omitempty"`
	RemoteCommit  string    `json:"remote_commit,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// CandidateOutbox is deliberately separate from the durable Task Engine and
// its EffectIntent recovery. A transport retry can only resend the same inert
// candidate payload, never execute a task action.
type CandidateOutbox struct {
	DB        *sql.DB
	Submitter CandidateSubmitter
	Authorize func(context.Context, CandidateMessage) error
	Now       func() time.Time
}

func (o CandidateOutbox) now() time.Time {
	if o.Now != nil {
		return o.Now().UTC()
	}
	return time.Now().UTC()
}

func (o CandidateOutbox) Queue(ctx context.Context, msg CandidateMessage) (CandidateDelivery, error) {
	if o.DB == nil || strings.TrimSpace(msg.CandidateID) == "" || strings.TrimSpace(msg.ProjectID) == "" ||
		strings.TrimSpace(msg.TaskID) == "" || !versionPattern.MatchString(msg.PayloadDigest) ||
		len(msg.Payload) == 0 || len(msg.Payload) > 24000 || !json.Valid(msg.Payload) {
		return CandidateDelivery{}, errors.New("complete, bounded Project Brain candidate is required")
	}
	if msg.Approval.ProjectID != msg.ProjectID || msg.Approval.HarnessTaskID != msg.TaskID ||
		msg.Approval.TaskRevision < 1 || strings.TrimSpace(msg.Approval.Actor) == "" ||
		msg.Approval.ApprovedAt.IsZero() || !versionPattern.MatchString(msg.Approval.ContentDigest) ||
		len(msg.Approval.EvidenceDigests) == 0 {
		return CandidateDelivery{}, errors.New("durable explicit candidate export approval is required")
	}
	approvalJSON, err := json.Marshal(msg.Approval)
	if err != nil {
		return CandidateDelivery{}, err
	}
	var embedded struct {
		CandidateID   string `json:"candidate_id"`
		ProjectID     string `json:"project_id"`
		PayloadDigest string `json:"payload_digest"`
	}
	if json.Unmarshal(msg.Payload, &embedded) != nil || embedded.CandidateID != msg.CandidateID ||
		embedded.ProjectID != msg.ProjectID || embedded.PayloadDigest != msg.PayloadDigest {
		return CandidateDelivery{}, errors.New("candidate payload does not match outbox identity")
	}
	var canonical map[string]json.RawMessage
	if json.Unmarshal(msg.Payload, &canonical) != nil {
		return CandidateDelivery{}, errors.New("candidate payload is invalid")
	}
	delete(canonical, "payload_digest")
	unsigned, err := json.Marshal(canonical)
	if err != nil {
		return CandidateDelivery{}, err
	}
	wantDigest, err := candidateJCSDigest(unsigned)
	if err != nil || wantDigest != msg.PayloadDigest {
		return CandidateDelivery{}, errors.New("candidate JCS payload digest does not match immutable message")
	}
	if existing, err := o.Get(ctx, msg.CandidateID); err == nil {
		if existing.PayloadDigest != msg.PayloadDigest || existing.ProjectID != msg.ProjectID || existing.TaskID != msg.TaskID {
			return CandidateDelivery{}, ErrCandidateConflict
		}
		if existing.State == "blocked" {
			var oldJSON string
			if err := o.DB.QueryRowContext(ctx, `SELECT approval_json FROM project_brain_candidate_outbox WHERE candidate_id=?`,
				msg.CandidateID).Scan(&oldJSON); err != nil {
				return CandidateDelivery{}, err
			}
			var old CandidateExportApproval
			if json.Unmarshal([]byte(oldJSON), &old) != nil || !msg.Approval.ApprovedAt.After(old.ApprovedAt) {
				return existing, nil
			}
			now := o.now().Format(time.RFC3339Nano)
			_, err := o.DB.ExecContext(ctx, `UPDATE project_brain_candidate_outbox SET state='pending',
				approval_json=?,last_error='',next_attempt_at=?,updated_at=?
				WHERE candidate_id=? AND state='blocked' AND payload_digest=?`,
				string(approvalJSON), now, now, msg.CandidateID, msg.PayloadDigest)
			if err != nil {
				return CandidateDelivery{}, err
			}
			return o.Get(ctx, msg.CandidateID)
		}
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return CandidateDelivery{}, err
	}
	now := o.now().Format(time.RFC3339Nano)
	_, err = o.DB.ExecContext(ctx, `INSERT INTO project_brain_candidate_outbox
		(candidate_id,project_id,task_id,payload_json,payload_digest,approval_json,state,next_attempt_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,'pending',?,?,?)`, msg.CandidateID, msg.ProjectID, msg.TaskID,
		string(msg.Payload), msg.PayloadDigest, string(approvalJSON), now, now, now)
	if err != nil {
		if existing, getErr := o.Get(ctx, msg.CandidateID); getErr == nil {
			if existing.PayloadDigest != msg.PayloadDigest || existing.ProjectID != msg.ProjectID || existing.TaskID != msg.TaskID {
				return CandidateDelivery{}, ErrCandidateConflict
			}
			return existing, nil
		}
		return CandidateDelivery{}, err
	}
	return o.Get(ctx, msg.CandidateID)
}

func (o CandidateOutbox) Get(ctx context.Context, candidateID string) (CandidateDelivery, error) {
	var item CandidateDelivery
	var updated string
	err := o.DB.QueryRowContext(ctx, `SELECT candidate_id,project_id,task_id,payload_digest,state,
		attempt_count,last_error,remote_commit,updated_at FROM project_brain_candidate_outbox WHERE candidate_id=?`,
		candidateID).Scan(&item.CandidateID, &item.ProjectID, &item.TaskID, &item.PayloadDigest,
		&item.State, &item.Attempts, &item.LastError, &item.RemoteCommit, &updated)
	if err != nil {
		return item, err
	}
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return item, nil
}

func (o CandidateOutbox) Recover(ctx context.Context) (int64, error) {
	if o.DB == nil {
		return 0, errors.New("Project Brain outbox database is unavailable")
	}
	now := o.now().Format(time.RFC3339Nano)
	result, err := o.DB.ExecContext(ctx, `UPDATE project_brain_candidate_outbox SET state='pending',
		next_attempt_at=?,updated_at=?,last_error='delivery_interrupted' WHERE state='sending'`, now, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func retryDelay(attempt int) time.Duration {
	if attempt > 8 {
		attempt = 8
	}
	if attempt < 1 {
		attempt = 1
	}
	return time.Duration(1<<uint(attempt-1)) * time.Minute
}

// Drain sends due candidates in stable order. Pi's candidate ID and JCS digest
// make crash-after-remote-commit replay idempotent. No task or Effect APIs are
// reachable from this method.
func (o CandidateOutbox) Drain(ctx context.Context, limit int) (int, error) {
	if o.DB == nil || o.Submitter == nil || o.Authorize == nil {
		return 0, errors.New("Project Brain candidate delivery is not configured")
	}
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	now := o.now().Format(time.RFC3339Nano)
	rows, err := o.DB.QueryContext(ctx, `SELECT candidate_id FROM project_brain_candidate_outbox
		WHERE state='pending' AND next_attempt_at<=? ORDER BY created_at,candidate_id LIMIT ?`, now, limit)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	delivered := 0
	for _, id := range ids {
		result, err := o.DB.ExecContext(ctx, `UPDATE project_brain_candidate_outbox SET state='sending',
			attempt_count=attempt_count+1,updated_at=? WHERE candidate_id=? AND state='pending'`, now, id)
		if err != nil {
			return delivered, err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			continue
		}
		var payload, projectID, taskID, approvalJSON string
		var attempt int
		var digest string
		if err := o.DB.QueryRowContext(ctx, `SELECT project_id,task_id,payload_json,payload_digest,approval_json,attempt_count
			FROM project_brain_candidate_outbox WHERE candidate_id=? AND state='sending'`, id).
			Scan(&projectID, &taskID, &payload, &digest, &approvalJSON, &attempt); err != nil {
			return delivered, err
		}
		var approval CandidateExportApproval
		if json.Unmarshal([]byte(approvalJSON), &approval) != nil {
			return delivered, errors.New("stored Project Brain approval is invalid")
		}
		message := CandidateMessage{CandidateID: id, ProjectID: projectID, TaskID: taskID,
			PayloadDigest: digest, Payload: json.RawMessage(payload), Approval: approval}
		sendErr := o.Authorize(ctx, message)
		var receipt SubmissionReceipt
		if sendErr == nil {
			receipt, sendErr = o.Submitter.SubmitCandidate(ctx, message.Payload)
		}
		if sendErr == nil && (receipt.CandidateID != id || receipt.PayloadDigest != digest ||
			(receipt.Status != "submitted_unverified" && receipt.Status != "already_submitted")) {
			sendErr = errors.New("invalid receipt")
		}
		if sendErr != nil {
			state, code := "pending", "submit_unavailable"
			if errors.Is(sendErr, ErrCandidateConflict) {
				state, code = "conflict", "idempotency_conflict"
			} else if errors.Is(sendErr, ErrExportRevoked) {
				state, code = "blocked", "export_revoked"
			} else if sendErr.Error() == "invalid receipt" {
				state, code = "conflict", "invalid_receipt"
			}
			_, updateErr := o.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE project_brain_candidate_outbox
				SET state=?,next_attempt_at=?,last_error=?,updated_at=? WHERE candidate_id=? AND state='sending'`,
				state, o.now().Add(retryDelay(attempt)).Format(time.RFC3339Nano), code,
				o.now().Format(time.RFC3339Nano), id)
			if updateErr != nil {
				return delivered, fmt.Errorf("record Project Brain delivery failure: %w", updateErr)
			}
			continue
		}
		_, err = o.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE project_brain_candidate_outbox
			SET state='submitted',remote_commit=?,last_error='',submitted_at=?,updated_at=?
			WHERE candidate_id=? AND state='sending'`, receipt.Commit, o.now().Format(time.RFC3339Nano),
			o.now().Format(time.RFC3339Nano), id)
		if err != nil {
			return delivered, err
		}
		delivered++
	}
	return delivered, nil
}

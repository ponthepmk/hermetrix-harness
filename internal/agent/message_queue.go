package agent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"hermetrix-harness/internal/durability"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

type QueueInput struct {
	Content        string `json:"content"`
	IdempotencyKey string `json:"idempotency_key"`
	Mode           string `json:"mode"`
	TurnID         string `json:"turn_id,omitempty"`
}

type QueuedMessage struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Mode      string `json:"mode"`
	State     string `json:"state"`
	TurnID    string `json:"turn_id,omitempty"`
	Error     string `json:"error,omitempty"`
	CreatedAt string `json:"created_at"`
}

func (s *Service) QueueMessage(ctx context.Context, sessionID string, input QueueInput) (QueuedMessage, error) {
	input.Content = strings.TrimSpace(input.Content)
	if input.Mode == "" {
		input.Mode = "queue"
	}
	if input.Content == "" || len(input.Content) > maxUserMessage || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 || (input.Mode != "queue" && input.Mode != "steer") {
		return QueuedMessage{}, fmt.Errorf("valid text, mode and idempotency_key are required")
	}
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return QueuedMessage{}, err
	}
	if session.TeamChild {
		return QueuedMessage{}, fmt.Errorf("agent team sessions cannot queue owner chat turns")
	}
	sum := sha256.Sum256([]byte(input.Mode + "\x00" + input.TurnID + "\x00" + input.Content))
	digest := hex.EncodeToString(sum[:])
	var existing, hash string
	err = s.store.DB.QueryRowContext(ctx, `SELECT id,payload_hash FROM agent_message_queue WHERE session_id=? AND idempotency_key=?`, sessionID, input.IdempotencyKey).Scan(&existing, &hash)
	if err == nil {
		if digest != hash {
			return QueuedMessage{}, fmt.Errorf("queue key already used for different content")
		}
		return s.queuedMessage(ctx, sessionID, existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return QueuedMessage{}, err
	}
	if input.Mode == "steer" && (input.TurnID == "" || input.TurnID != session.ActiveTurnID) {
		return QueuedMessage{}, fmt.Errorf("steer requires the exact active turn")
	}
	now := formatTime(time.Now().UTC())
	id := identity.New("queued")
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return QueuedMessage{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_message_queue(id,session_id,owner_principal_id,project_id,provider_id,contract_revision,cache_epoch,content,mode,base_turn_id,idempotency_key,payload_hash,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'queued',?,?)`, id, session.ID, session.OwnerPrincipalID, session.ProjectID, session.ProviderID, session.ContractRevision, session.CacheEpoch, input.Content, input.Mode, input.TurnID, input.IdempotencyKey, digest, now, now); err != nil {
		return QueuedMessage{}, err
	}
	if input.Mode == "steer" {
		if _, err = tx.ExecContext(ctx, `UPDATE agent_message_queue SET state='needs_review',error='Direction changed; review earlier queued messages',updated_at=? WHERE session_id=? AND id<>? AND state='queued'`, now, sessionID, id); err != nil {
			return QueuedMessage{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return QueuedMessage{}, err
	}
	if input.Mode == "steer" {
		if _, err = s.StopTurn(ctx, sessionID, input.TurnID); err != nil {
			durability.Exec("quarantine queued steer after stop failure").Observe(s.store.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE agent_message_queue SET state='needs_review',error='Active turn changed before steer; refresh before continuing',updated_at=? WHERE id=? AND state='queued'`, now, id))
			return QueuedMessage{}, err
		}
	}
	return s.queuedMessage(ctx, sessionID, id)
}

func (s *Service) queuedMessage(ctx context.Context, sessionID, id string) (QueuedMessage, error) {
	var item QueuedMessage
	err := s.store.DB.QueryRowContext(ctx, `SELECT id,session_id,content,mode,state,turn_id,error,created_at FROM agent_message_queue WHERE session_id=? AND id=?`, sessionID, id).Scan(&item.ID, &item.SessionID, &item.Content, &item.Mode, &item.State, &item.TurnID, &item.Error, &item.CreatedAt)
	return item, err
}

func (s *Service) ListQueuedMessages(ctx context.Context, sessionID string) ([]QueuedMessage, error) {
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,session_id,content,mode,state,turn_id,error,created_at FROM agent_message_queue WHERE session_id=? AND state NOT IN ('completed','cancelled') ORDER BY created_at,rowid LIMIT 100`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []QueuedMessage{}
	for rows.Next() {
		var item QueuedMessage
		if err = rows.Scan(&item.ID, &item.SessionID, &item.Content, &item.Mode, &item.State, &item.TurnID, &item.Error, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) CancelQueuedMessage(ctx context.Context, sessionID, id string) (QueuedMessage, error) {
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return QueuedMessage{}, err
	}
	_, err := s.store.DB.ExecContext(ctx, `UPDATE agent_message_queue SET state='cancelled',updated_at=? WHERE session_id=? AND id=? AND state IN ('queued','needs_review')`, formatTime(time.Now().UTC()), sessionID, id)
	if err != nil {
		return QueuedMessage{}, err
	}
	item, err := s.queuedMessage(ctx, sessionID, id)
	if err == nil && item.State != "cancelled" {
		return item, fmt.Errorf("message already started; stop its exact turn instead")
	}
	return item, err
}

// DrainOneQueued uses the normal RunTurn authority/qualification/lease path.
// Queue reservation is committed in acquireTurn's transaction, not in this read.
func (s *Service) DrainOneQueued(ctx context.Context) (bool, error) {
	var id, sessionID, owner, content, revision, provider, project string
	var epoch int
	err := s.store.DB.QueryRowContext(ctx, `SELECT q.id,q.session_id,q.owner_principal_id,q.content,q.contract_revision,q.provider_id,q.project_id,q.cache_epoch FROM agent_message_queue q JOIN agent_sessions s ON s.id=q.session_id WHERE q.state='queued' AND s.state='active' AND s.active_turn_id='' AND (q.mode='queue' OR EXISTS(SELECT 1 FROM agent_turn_controls c WHERE c.session_id=q.session_id AND c.turn_id=q.base_turn_id AND c.state='stopped')) ORDER BY q.created_at,q.rowid LIMIT 1`).Scan(&id, &sessionID, &owner, &content, &revision, &provider, &project, &epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	bound := identity.WithPrincipal(ctx, owner)
	session, err := s.GetSession(bound, sessionID)
	if err != nil || session.ContractRevision != revision || session.ProviderID != provider || session.ProjectID != project || session.CacheEpoch != epoch {
		_, updateErr := s.store.DB.ExecContext(ctx, `UPDATE agent_message_queue SET state='needs_review',error='Session scope or contract changed; review before sending',updated_at=? WHERE id=? AND state='queued'`, formatTime(time.Now().UTC()), id)
		return true, updateErr
	}
	result, runErr := s.RunTurn(bound, sessionID, TurnInput{Content: content, IdempotencyKey: "queue:" + id, QueueID: id}, nil)
	state := "completed"
	if result.FinishReason == "approval_required" {
		state = "awaiting_approval"
	}
	if runErr != nil {
		state = "failed"
	}
	// A concurrent direct turn can win the normal lease. Leave the item queued.
	var current string
	if err = s.store.DB.QueryRowContext(ctx, `SELECT state FROM agent_message_queue WHERE id=?`, id).Scan(&current); err != nil {
		return false, err
	}
	if current == "queued" {
		if runErr != nil {
			fresh, loadErr := s.GetSession(bound, sessionID)
			if loadErr == nil && fresh.State != "active" {
				return false, nil
			}
			state = "needs_review"
		} else {
			return false, fmt.Errorf("queued turn did not acquire its item")
		}
	}
	message := ""
	if runErr != nil {
		message = "Turn could not finish; inspect its receipts before sending again"
	}
	_, err = s.store.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE agent_message_queue SET state=?,error=?,updated_at=? WHERE id=? AND state IN ('queued','running')`, state, message, formatTime(time.Now().UTC()), id)
	return true, err
}

func (s *Service) RunMessageQueue(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = s.DrainOneQueued(ctx)
		}
	}
}

func (s *Service) RecoverMessageQueue(ctx context.Context) error {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := formatTime(time.Now().UTC())
	// Inspect leases before startup recovery clears them. A never-dispatched
	// follow-up still depends on the interrupted turn's outcome.
	if _, err = tx.ExecContext(ctx, `UPDATE agent_message_queue SET state='needs_review',error='Previous execution was interrupted; review this continuation before sending',updated_at=? WHERE state='queued' AND (EXISTS(SELECT 1 FROM agent_sessions s WHERE s.id=agent_message_queue.session_id AND s.state IN ('running','control_blocked')) OR EXISTS(SELECT 1 FROM tool_approvals a WHERE a.session_id=agent_message_queue.session_id AND a.state IN ('executing','uncertain')) OR EXISTS(SELECT 1 FROM agent_message_queue q WHERE q.session_id=agent_message_queue.session_id AND q.state='running'))`, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_message_queue SET state='needs_review',error='Execution interrupted; no message or effect was replayed',updated_at=? WHERE state='running'`, now); err != nil {
		return err
	}
	return tx.Commit()
}

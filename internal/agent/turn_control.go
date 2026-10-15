package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/providers"
)

var ErrTurnStopped = errors.New("turn stopped by owner")

// A restart is not evidence that an external command/effect terminated.
// Retain the turn's ownership and require inspection, never resume its queue.
func (s *Service) RecoverTurnControls(ctx context.Context) error {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := formatTime(time.Now().UTC())
	if _, err = tx.ExecContext(ctx, `UPDATE agent_turn_controls SET state='needs_review',reason='Restart interrupted stop confirmation; inspect command and effect receipts before continuing',updated_at=? WHERE state='stopping'`, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_sessions SET state='control_blocked',active_turn_id=(SELECT c.turn_id FROM agent_turn_controls c WHERE c.session_id=agent_sessions.id AND c.state='needs_review' ORDER BY requested_at DESC LIMIT 1),updated_at=? WHERE EXISTS(SELECT 1 FROM agent_turn_controls c WHERE c.session_id=agent_sessions.id AND c.state='needs_review')`, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tool_approvals SET state='cancelled',decided_at=?,decision_reason='stop persisted before restart' WHERE state='pending' AND EXISTS(SELECT 1 FROM agent_turn_controls c WHERE c.session_id=tool_approvals.session_id AND c.turn_id=tool_approvals.turn_id)`, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_message_queue SET state='needs_review',error='Previous stop requires inspection after restart',updated_at=? WHERE state='queued' AND mode='steer' AND EXISTS(SELECT 1 FROM agent_turn_controls c WHERE c.session_id=agent_message_queue.session_id AND c.turn_id=agent_message_queue.base_turn_id AND c.state='needs_review')`, now); err != nil {
		return err
	}
	return tx.Commit()
}

type TurnControl struct {
	TurnID      string `json:"turn_id"`
	State       string `json:"state"`
	RequestedAt string `json:"requested_at"`
	UpdatedAt   string `json:"updated_at"`
	Reason      string `json:"reason"`
}

type turnExecutions struct {
	mu     sync.Mutex
	active map[string]context.CancelFunc
}

// Registration and stop requests share a lock. A request arriving before
// registration remains durable and cancels the executor as it registers.
func (s *Service) controlExecution(ctx context.Context, sessionID, turnID string) (context.Context, func(), error) {
	s.executions.mu.Lock()
	defer s.executions.mu.Unlock()
	if s.executions.active == nil {
		s.executions.active = map[string]context.CancelFunc{}
	}
	if s.executions.active[turnID] != nil {
		return nil, nil, fmt.Errorf("turn execution is already active")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.executions.active[turnID] = cancel
	if err := s.checkTurnControl(ctx, sessionID, turnID); err != nil {
		cancel()
	}
	return runCtx, func() {
		cancel()
		s.executions.mu.Lock()
		delete(s.executions.active, turnID)
		s.executions.mu.Unlock()
		// Stop may race a successful return at an approval boundary.
		durable := context.WithoutCancel(ctx)
		control, err := s.turnControlByID(durable, sessionID, turnID)
		if err == nil && control.State == "stopping" {
			session, loadErr := s.GetSession(durable, sessionID)
			if loadErr == nil {
				provider, providerErr := s.providers.Get(durable, session.ProviderID)
				if providerErr == nil {
					_, _ = s.finishStoppedTurn(durable, session, provider, turnID)
				}
			}
		}
	}, nil
}

func (s *Service) checkTurnControl(ctx context.Context, sessionID, turnID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var requested bool
	if err := s.store.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_turn_controls WHERE session_id=? AND turn_id=?)`, sessionID, turnID).Scan(&requested); err != nil {
		return err
	}
	if requested {
		return ErrTurnStopped
	}
	return nil
}

func (s *Service) GetTurnControl(ctx context.Context, sessionID string) (*TurnControl, error) {
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	var item TurnControl
	err := s.store.DB.QueryRowContext(ctx, `SELECT turn_id,state,requested_at,updated_at,reason FROM agent_turn_controls WHERE session_id=? ORDER BY requested_at DESC LIMIT 1`, sessionID).Scan(&item.TurnID, &item.State, &item.RequestedAt, &item.UpdatedAt, &item.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}

// StopTurn never interprets a cancellation request as proof an executor ended.
// Pending approval has no dispatched effect and can be closed atomically.
func (s *Service) StopTurn(ctx context.Context, sessionID, turnID string) (*TurnControl, error) {
	if turnID == "" {
		return nil, fmt.Errorf("exact turn_id is required")
	}
	s.executions.mu.Lock()
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		s.executions.mu.Unlock()
		return nil, err
	}
	var existing bool
	err = s.store.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_turn_controls WHERE session_id=? AND turn_id=?)`, sessionID, turnID).Scan(&existing)
	if err != nil {
		s.executions.mu.Unlock()
		return nil, err
	}
	if existing {
		s.executions.mu.Unlock()
		return s.turnControlByID(ctx, sessionID, turnID)
	}
	if session.ActiveTurnID != turnID || (session.State != "running" && session.State != "awaiting_approval") {
		s.executions.mu.Unlock()
		return nil, fmt.Errorf("turn is no longer active; refresh its state")
	}
	now := formatTime(time.Now().UTC())
	jobs, _ := json.Marshal(s.runs.activeIDs(sessionID))
	result, err := s.store.DB.ExecContext(ctx, `INSERT INTO agent_turn_controls(session_id,turn_id,state,requested_at,updated_at,run_ids_json) SELECT id,?,'stopping',?,?,? FROM agent_sessions WHERE id=? AND active_turn_id=? AND state IN ('running','awaiting_approval')`, turnID, now, now, string(jobs), sessionID, turnID)
	if err == nil {
		if changed, _ := result.RowsAffected(); changed != 1 {
			err = fmt.Errorf("turn finished before the stop request")
		}
	}
	cancel := s.executions.active[turnID]
	if err == nil && cancel != nil {
		cancel()
	}
	s.executions.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if session.State == "awaiting_approval" && cancel == nil {
		provider, loadErr := s.providers.Get(ctx, session.ProviderID)
		if loadErr != nil {
			return nil, loadErr
		}
		if _, err = s.finishStoppedTurn(context.WithoutCancel(ctx), session, provider, turnID); err != nil {
			return nil, err
		}
	}
	return s.turnControlByID(ctx, sessionID, turnID)
}

func (s *Service) turnControlByID(ctx context.Context, sessionID, turnID string) (*TurnControl, error) {
	var item TurnControl
	err := s.store.DB.QueryRowContext(ctx, `SELECT turn_id,state,requested_at,updated_at,reason FROM agent_turn_controls WHERE session_id=? AND turn_id=?`, sessionID, turnID).Scan(&item.TurnID, &item.State, &item.RequestedAt, &item.UpdatedAt, &item.Reason)
	return &item, err
}

func (s *Service) ReconcileStoppedTurn(ctx context.Context, sessionID, turnID string) (*TurnControl, error) {
	s.executions.mu.Lock()
	defer s.executions.mu.Unlock()
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	control, err := s.turnControlByID(ctx, sessionID, turnID)
	if err != nil {
		return nil, err
	}
	if control.State == "stopped" {
		return control, nil
	}
	if control.State != "needs_review" || session.ActiveTurnID != turnID || s.executions.active[turnID] != nil {
		return nil, fmt.Errorf("stop confirmation is still running or the exact turn changed")
	}
	provider, err := s.providers.Get(ctx, session.ProviderID)
	if err != nil {
		return nil, err
	}
	if err = s.providers.ConfirmRuntimeIdle(ctx, provider); err != nil {
		return control, err
	}
	if _, err = s.finishStoppedTurn(ctx, session, provider, turnID); err != nil {
		return nil, err
	}
	return s.turnControlByID(ctx, sessionID, turnID)
}

func (s *Service) finishStoppedTurn(ctx context.Context, session Session, provider providers.Profile, turnID string) (Event, error) {
	// Command processes outlive a poll. Cancel only this session's owned jobs,
	// then retain the lease if any job still runs or cannot be observed.
	state, reason := "stopped", ""
	if err := s.providers.ConfirmStoppedRuntime(ctx, provider); err != nil {
		state, reason = "needs_review", "The local model has not confirmed it is idle; inference remains paused."
	}
	if s.runner != nil {
		var stored string
		if err := s.store.DB.QueryRowContext(ctx, `SELECT run_ids_json FROM agent_turn_controls WHERE session_id=? AND turn_id=?`, session.ID, turnID).Scan(&stored); err != nil {
			return Event{}, err
		}
		var saved []string
		if err := json.Unmarshal([]byte(stored), &saved); err != nil {
			return Event{}, err
		}
		ids := s.runs.activeIDs(session.ID)
		seen := map[string]bool{}
		for _, id := range ids {
			seen[id] = true
		}
		for _, id := range saved {
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
		encoded, _ := json.Marshal(ids)
		if _, err := s.store.DB.ExecContext(ctx, `UPDATE agent_turn_controls SET run_ids_json=? WHERE session_id=? AND turn_id=?`, string(encoded), session.ID, turnID); err != nil {
			return Event{}, err
		}
		for _, id := range ids {
			bound, cancel := context.WithTimeout(ctx, 2*time.Second)
			result, lookupErr := s.runner.LookupRun(bound, id)
			if lookupErr == nil && !result.Done() {
				_ = s.runner.CancelRun(bound, id)
				result, lookupErr = s.runner.LookupRun(bound, id)
			}
			cancel()
			if lookupErr != nil || !result.Done() {
				state, reason = "needs_review", "A command has not confirmed termination; inspect its result before continuing."
			} else {
				s.runs.finish(session.ID, id)
			}
		}
	} else {
		var saved string
		if err := s.store.DB.QueryRowContext(ctx, `SELECT run_ids_json FROM agent_turn_controls WHERE session_id=? AND turn_id=?`, session.ID, turnID).Scan(&saved); err != nil {
			return Event{}, err
		}
		if saved != "[]" && saved != "null" {
			state, reason = "needs_review", "Command receipts cannot be inspected without their runner."
		}
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM agent_turn_controls WHERE session_id=? AND turn_id=?`, session.ID, turnID).Scan(&current); err != nil {
		return Event{}, err
	}
	if current != "stopping" && current != "needs_review" {
		return Event{}, nil
	}
	if current == "needs_review" && state == "needs_review" {
		return Event{}, nil
	}
	var uncertain int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM tool_approvals WHERE session_id=? AND turn_id=? AND state IN ('executing','uncertain')`, session.ID, turnID).Scan(&uncertain); err != nil {
		return Event{}, err
	}
	if uncertain > 0 {
		state, reason = "needs_review", "An effect has an uncertain result; inspect its receipt before continuing."
	}
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `UPDATE tool_approvals SET state='cancelled',decided_at=?,decision_reason='owner stopped turn' WHERE session_id=? AND turn_id=? AND state='pending'`, formatTime(now), session.ID, turnID); err != nil {
		return Event{}, err
	}
	event := Event{ID: identity.New("event"), SessionID: session.ID, TurnID: turnID, EventKind: "turn_stopped", Role: "assistant", Content: "หยุดตามคำขอแล้ว ผลของงานก่อนหยุดยังอยู่ในประวัติ", ProviderID: provider.ID, Model: provider.Model, CreatedAt: now}
	if current == "needs_review" {
		event.EventKind = "turn_stop_reconciled"
		event.Content = "ตรวจยืนยันการหยุดแล้ว พร้อมรับข้อความถัดไป"
	}
	if state == "needs_review" {
		event.Content = "หยุดการสั่งงานเพิ่มแล้ว แต่ยังต้องตรวจผลของเครื่องมือก่อนทำต่อ"
	}
	event.Sequence, err = nextSequence(ctx, tx, session.ID)
	if err != nil {
		return Event{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_events(id,session_id,turn_id,sequence,event_kind,role,content,metadata_json,provider_id,model,created_at) VALUES(?,?,?,?,?,?,?,'{}',?,?,?)`, event.ID, event.SessionID, event.TurnID, event.Sequence, event.EventKind, event.Role, event.Content, provider.ID, provider.Model, formatTime(now)); err != nil {
		return Event{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_turn_controls SET state=?,reason=?,updated_at=? WHERE session_id=? AND turn_id=?`, state, reason, formatTime(now), session.ID, turnID); err != nil {
		return Event{}, err
	}
	if state == "stopped" {
		_, err = tx.ExecContext(ctx, `UPDATE agent_sessions SET state='active',active_turn_id='',lease_acquired_at=NULL,updated_at=? WHERE id=? AND active_turn_id=?`, formatTime(now), session.ID, turnID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE agent_sessions SET state='control_blocked',updated_at=? WHERE id=? AND active_turn_id=?`, formatTime(now), session.ID, turnID)
	}
	if err != nil {
		return Event{}, err
	}
	queueState := "cancelled"
	if state == "needs_review" {
		queueState = "needs_review"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_message_queue SET state=?,error=?,updated_at=? WHERE turn_id=? AND state IN ('running','awaiting_approval')`, queueState, reason, formatTime(now), turnID); err != nil {
		return Event{}, err
	}
	if s.skills != nil {
		if _, err = s.skills.CompleteTurnActivationsTx(ctx, tx, turnID, "failure"); err != nil {
			return Event{}, err
		}
	}
	return event, tx.Commit()
}

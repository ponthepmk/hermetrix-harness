package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hermetrix-harness/internal/identity"
	"time"
)

const AutoRetryInterval = 5 * time.Minute
const autoContinuation = "ทำต่อจากคำขอเดิมและเป้าหมายในเอกสารบริบท ใช้ context_search ดึงเฉพาะส่วนที่จำเป็น ไม่อ่านไฟล์เดิมซ้ำทั้งหมด ตรวจผลคำสั่งเดิมก่อน ห้ามทำการเปลี่ยนไฟล์หรือคำสั่งที่มีผลข้างเคียงซ้ำโดยไม่ตรวจหลักฐาน สรุปสิ่งที่ทำสำเร็จ งานค้าง และผลทดสอบจริง ถ้ายังไม่เสร็จให้ระบุงานค้างชัดเจน"

type RetryStatus struct {
	State           string    `json:"state"`
	NextAttemptAt   time.Time `json:"next_attempt_at"`
	Attempts        int       `json:"attempts"`
	IntervalSeconds int       `json:"interval_seconds"`
}

func scheduleRetryTx(ctx context.Context, tx *sql.Tx, event Event) error {
	pending := event.EventKind == "turn_failed" || event.Metadata["continuation_needed"] == true
	if !pending {
		_, err := tx.ExecContext(ctx, `UPDATE agent_retries SET state='completed',updated_at=? WHERE session_id=? AND state<>'paused'`, formatTime(event.CreatedAt), event.SessionID)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO agent_retries(session_id,source_event_id,state,next_attempt_at,updated_at) VALUES(?,?,'pending',?,?)
 ON CONFLICT(session_id) DO UPDATE SET source_event_id=excluded.source_event_id,state='pending',next_attempt_at=excluded.next_attempt_at,updated_at=excluded.updated_at WHERE agent_retries.state<>'paused'`,
		event.SessionID, event.ID, formatTime(event.CreatedAt.Add(AutoRetryInterval)), formatTime(event.CreatedAt))
	return err
}

func (s *Service) RetryStatus(ctx context.Context, sessionID string) (*RetryStatus, error) {
	var status RetryStatus
	var next string
	err := s.store.DB.QueryRowContext(ctx, `SELECT state,next_attempt_at,attempts FROM agent_retries WHERE session_id=?`, sessionID).Scan(&status.State, &next, &status.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	status.NextAttemptAt, err = time.Parse(time.RFC3339Nano, next)
	status.IntervalSeconds = int(AutoRetryInterval.Seconds())
	return &status, err
}

func (s *Service) PauseAutoRetry(ctx context.Context, sessionID string) error {
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return err
	}
	now := formatTime(time.Now().UTC())
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO agent_retries(session_id,state,next_attempt_at,updated_at) VALUES(?,'paused',?,?) ON CONFLICT(session_id) DO UPDATE SET state='paused',updated_at=excluded.updated_at`, sessionID, now, now)
	return err
}

// Recover schedules only the latest unfinished outcome. A paused schedule is
// retained across restarts; newer user requests and completed answers win.
func (s *Service) RecoverAutoRetries(ctx context.Context) error {
	now := time.Now().UTC()
	_, err := s.store.DB.ExecContext(ctx, `UPDATE agent_retries SET state='pending',next_attempt_at=?,updated_at=? WHERE state='running'`, formatTime(now.Add(AutoRetryInterval)), formatTime(now))
	if err != nil {
		return err
	}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO agent_retries(session_id,source_event_id,state,next_attempt_at,updated_at)
 SELECT e.session_id,e.id,'pending',?,? FROM agent_events e JOIN agent_sessions s ON s.id=e.session_id
 WHERE s.state='active' AND e.sequence=(SELECT MAX(sequence) FROM agent_events WHERE session_id=e.session_id AND (event_kind='message' OR event_kind='turn_failed'))
 AND (e.event_kind='turn_failed' OR (e.role='assistant' AND json_extract(e.metadata_json,'$.continuation_needed')=1))
 ON CONFLICT(session_id) DO UPDATE SET source_event_id=excluded.source_event_id,state='pending',next_attempt_at=excluded.next_attempt_at,updated_at=excluded.updated_at WHERE agent_retries.state<>'paused' AND agent_retries.source_event_id<>excluded.source_event_id`, formatTime(now.Add(AutoRetryInterval)), formatTime(now))
	return err
}

// One worker invocation claims one due session. RunTurn's normal lease and the
// expected source event are checked atomically, protecting against concurrent
// manual sends, pause requests and duplicate workers.
func (s *Service) RunDueAutoRetry(ctx context.Context, now time.Time) (bool, error) {
	var sessionID, source string
	err := s.store.DB.QueryRowContext(ctx, `SELECT r.session_id,r.source_event_id FROM agent_retries r JOIN agent_sessions s ON s.id=r.session_id
 WHERE r.state='pending' AND r.next_attempt_at<=? AND s.state='active' AND s.active_turn_id='' ORDER BY r.next_attempt_at LIMIT 1`, formatTime(now)).Scan(&sessionID, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE agent_retries SET state='running',attempts=attempts+1,updated_at=? WHERE session_id=? AND source_event_id=? AND state='pending' AND next_attempt_at<=?`, formatTime(now), sessionID, source, formatTime(now))
	if err != nil {
		return false, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return false, nil
	}
	retryCtx := ctx
	var retryActor string
	if err := s.store.DB.QueryRowContext(ctx, `SELECT actor FROM agent_approval_modes WHERE session_id=?`, sessionID).Scan(&retryActor); err == nil && retryActor != "" {
		retryCtx = identity.WithPrincipal(ctx, retryActor)
	}
	_, runErr := s.RunTurn(retryCtx, sessionID, TurnInput{Content: autoContinuation, IdempotencyKey: "auto-retry:" + source, ExpectedOutcomeID: source}, nil)
	if runErr == nil {
		var mode, actor string
		if err := s.store.DB.QueryRowContext(ctx, `SELECT mode,actor FROM agent_approval_modes WHERE session_id=?`, sessionID).Scan(&mode, &actor); err == nil && mode == "auto" {
			for attempts := 0; attempts < 24; attempts++ {
				if err := s.store.DB.QueryRowContext(ctx, `SELECT mode,actor FROM agent_approval_modes WHERE session_id=?`, sessionID).Scan(&mode, &actor); err != nil || mode != "auto" {
					break
				}
				status, _ := s.RetryStatus(ctx, sessionID)
				if status != nil && status.State == "paused" {
					break
				}
				approvals, err := s.ListApprovals(ctx, sessionID)
				if err != nil {
					runErr = err
					break
				}
				pending := ""
				for _, approval := range approvals {
					if approval.State == "pending" {
						pending = approval.ID
						break
					}
				}
				if pending == "" {
					break
				}
				if _, err = s.DecideApproval(identity.WithPrincipal(ctx, actor), pending, ApprovalDecisionInput{Automatic: true, Actor: actor, Decision: "approve", Reason: "auto approve mode selected by user; automatic continuation"}, nil); err != nil {
					runErr = err
					break
				}
			}
		}
	}
	// Validation can fail before a turn commits (e.g. an unavailable credential).
	// Leave the source scheduled; a committed outcome has already replaced it.
	_, err = s.store.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE agent_retries SET state='pending',next_attempt_at=?,updated_at=? WHERE session_id=? AND source_event_id=? AND state='running'`, formatTime(time.Now().UTC().Add(AutoRetryInterval)), formatTime(time.Now().UTC()), sessionID, source)
	if err != nil {
		return true, err
	}
	return true, runErr
}

func (s *Service) StartAutoRetries(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			_, err := s.RunDueAutoRetry(ctx, now.UTC())
			if err != nil && ctx.Err() == nil && onError != nil {
				onError(err)
			}
		}
	}
}

func (s *Service) SetApprovalMode(ctx context.Context, sessionID, mode, actor string) error {
	if mode != "auto" && mode != "ask_all" {
		return fmt.Errorf("approval mode must be auto or ask_all")
	}
	if actor == "" {
		return fmt.Errorf("approval actor is required")
	}
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return err
	}
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO agent_approval_modes(session_id,mode,actor) VALUES(?,?,?) ON CONFLICT(session_id) DO UPDATE SET mode=excluded.mode,actor=excluded.actor`, sessionID, mode, actor)
	return err
}

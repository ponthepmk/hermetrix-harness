package agentplatform

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type Transport interface {
	Send(context.Context, []byte) ([]byte, error)
}

type outboxRecord struct {
	PlatformRunID string
	Sequence      int64
	Envelope      []byte
	Attempts      int
}

// DeliverPending replays only immutable outbox bytes. It cannot reconstruct an
// update or invoke task execution because it owns neither dependency.
func (s *Service) DeliverPending(ctx context.Context, receiverTrust Trust, transport Transport, maxAttempts int) (DeliveryResult, error) {
	if transport == nil || maxAttempts < 1 {
		return DeliveryResult{}, ErrValidation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.store.DB.QueryContext(ctx, `SELECT o.platform_run_id,o.sequence,o.immutable_envelope,o.attempts
		FROM agent_platform_outbox o JOIN agent_platform_streams s
		ON s.platform_id=o.platform_id AND s.platform_run_id=o.platform_run_id
		WHERE o.platform_id=? AND o.sequence>s.acked_sequence AND o.attempts<?
		ORDER BY o.platform_run_id,o.sequence`, s.trust.PlatformID, maxAttempts)
	if err != nil {
		return DeliveryResult{}, err
	}
	items := []outboxRecord{}
	for rows.Next() {
		var item outboxRecord
		if err = rows.Scan(&item.PlatformRunID, &item.Sequence, &item.Envelope, &item.Attempts); err != nil {
			rows.Close()
			return DeliveryResult{}, err
		}
		items = append(items, item)
	}
	if err = rows.Close(); err != nil {
		return DeliveryResult{}, err
	}
	result := DeliveryResult{}
	for _, item := range items {
		result.Attempted++
		if err = s.recordDeliveryAttempt(ctx, item); err != nil {
			return result, err
		}
		ackRaw, sendErr := transport.Send(ctx, append([]byte(nil), item.Envelope...))
		if sendErr != nil {
			if err = s.recordDeliveryFailure(ctx, item, sendErr); err != nil {
				return result, err
			}
			continue
		}
		_, ack, validateErr := ValidateACK(ackRaw, receiverTrust)
		if validateErr != nil || ack.PlatformRunID != item.PlatformRunID {
			if validateErr == nil {
				validateErr = fmt.Errorf("ACK run mismatch")
			}
			if err = s.recordDeliveryFailure(ctx, item, validateErr); err != nil {
				return result, err
			}
			continue
		}
		if err = s.applyACK(ctx, ack); err != nil {
			return result, err
		}
		if ack.AckedSequence > result.Acked {
			result.Acked = ack.AckedSequence
		}
	}
	if err = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_platform_outbox o
		JOIN agent_platform_streams s ON s.platform_id=o.platform_id AND s.platform_run_id=o.platform_run_id
		WHERE o.platform_id=? AND o.sequence>s.acked_sequence`, s.trust.PlatformID).Scan(&result.Pending); err != nil {
		return result, err
	}
	if err = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_platform_outbox o
		JOIN agent_platform_streams s ON s.platform_id=o.platform_id AND s.platform_run_id=o.platform_run_id
		WHERE o.platform_id=? AND o.sequence>s.acked_sequence AND o.attempts>=?`, s.trust.PlatformID, maxAttempts).Scan(&result.Exhausted); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) recordDeliveryAttempt(ctx context.Context, item outboxRecord) error {
	result, err := s.store.DB.ExecContext(ctx, `UPDATE agent_platform_outbox SET attempts=attempts+1
		WHERE platform_id=? AND platform_run_id=? AND sequence=? AND attempts=?`, s.trust.PlatformID,
		item.PlatformRunID, item.Sequence, item.Attempts)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: concurrent delivery attempt", ErrDigestConflict)
	}
	return nil
}

func (s *Service) recordDeliveryFailure(ctx context.Context, item outboxRecord, cause error) error {
	_, err := s.store.DB.ExecContext(ctx, `UPDATE agent_platform_outbox SET delivery_status='failed',last_error=?
		WHERE platform_id=? AND platform_run_id=? AND sequence=? AND attempts=?`, cause.Error(), s.trust.PlatformID,
		item.PlatformRunID, item.Sequence, item.Attempts+1)
	return err
}

func (s *Service) applyACK(ctx context.Context, ack ACK) error {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var next, current int64
	if err = tx.QueryRowContext(ctx, `SELECT next_sequence,acked_sequence FROM agent_platform_streams
		WHERE platform_id=? AND platform_run_id=?`, s.trust.PlatformID, ack.PlatformRunID).Scan(&next, &current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: ACK run", ErrValidation)
		}
		return err
	}
	if ack.AckedSequence >= next {
		return fmt.Errorf("%w: ACK is ahead of committed stream", ErrValidation)
	}
	if ack.AckedSequence <= current {
		return tx.Commit()
	}
	now := formatContractTime(s.trust.now())
	if _, err = tx.ExecContext(ctx, `UPDATE agent_platform_streams SET acked_sequence=?,updated_at=?
		WHERE platform_id=? AND platform_run_id=?`, ack.AckedSequence, now, s.trust.PlatformID, ack.PlatformRunID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_platform_outbox SET delivery_status='acked',acked_at=?,last_error=''
		WHERE platform_id=? AND platform_run_id=? AND sequence<=?`, now, s.trust.PlatformID,
		ack.PlatformRunID, ack.AckedSequence); err != nil {
		return err
	}
	return tx.Commit()
}

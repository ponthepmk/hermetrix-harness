package fixture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"hermetrix-harness/internal/agentplatform"
)

type Receiver struct {
	db          *sql.DB
	senderTrust agentplatform.Trust
	trust       agentplatform.Trust
}

func OpenReceiver(ctx context.Context, root string, senderTrust, receiverTrust agentplatform.Trust) (*Receiver, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "fixture-receiver.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
		CREATE TABLE IF NOT EXISTS received_updates (
		 platform_run_id TEXT NOT NULL, sequence INTEGER NOT NULL, payload_digest TEXT NOT NULL,
		 immutable_envelope BLOB NOT NULL, received_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		 PRIMARY KEY(platform_run_id,sequence));
		CREATE TABLE IF NOT EXISTS receive_streams (
		 platform_run_id TEXT PRIMARY KEY, acked_sequence INTEGER NOT NULL DEFAULT 0 CHECK(acked_sequence>=0));
		CREATE TRIGGER IF NOT EXISTS received_update_immutable BEFORE UPDATE ON received_updates
		BEGIN SELECT RAISE(ABORT,'received update is immutable'); END;`); err != nil {
		db.Close()
		return nil, err
	}
	return &Receiver{db: db, senderTrust: senderTrust, trust: receiverTrust}, nil
}

func (r *Receiver) Close() error { return r.db.Close() }

func (r *Receiver) Send(ctx context.Context, raw []byte) ([]byte, error) {
	envelope, update, err := agentplatform.ValidateRunUpdate(raw, r.senderTrust)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_digest FROM received_updates WHERE platform_run_id=? AND sequence=?`,
		update.PlatformRunID, update.Sequence).Scan(&existing)
	if err == nil && existing != envelope.PayloadDigest {
		return nil, agentplatform.ErrDigestConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = tx.ExecContext(ctx, `INSERT INTO received_updates(platform_run_id,sequence,payload_digest,immutable_envelope)
			VALUES(?,?,?,?)`, update.PlatformRunID, update.Sequence, envelope.PayloadDigest, raw); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO receive_streams(platform_run_id,acked_sequence) VALUES(?,0)
		ON CONFLICT(platform_run_id) DO NOTHING`, update.PlatformRunID); err != nil {
		return nil, err
	}
	var contiguous int64
	if err = tx.QueryRowContext(ctx, `SELECT acked_sequence FROM receive_streams WHERE platform_run_id=?`, update.PlatformRunID).Scan(&contiguous); err != nil {
		return nil, err
	}
	for {
		var exists int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM received_updates WHERE platform_run_id=? AND sequence=?`,
			update.PlatformRunID, contiguous+1).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, err
		}
		contiguous++
	}
	if _, err = tx.ExecContext(ctx, `UPDATE receive_streams SET acked_sequence=? WHERE platform_run_id=?`, contiguous,
		update.PlatformRunID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	ackRaw, _, err := agentplatform.SignEnvelope("ACK", agentplatform.ACKRevision, r.trust.NodeID,
		fmt.Sprintf("ack:%s:%d", update.PlatformRunID, contiguous), agentplatform.ACK{PlatformRunID: update.PlatformRunID,
			AckedSequence: contiguous}, r.trust.KeyID, r.trust.PrivateKey)
	return ackRaw, err
}

func (r *Receiver) AckedSequence(ctx context.Context, runID string) (int64, error) {
	var value int64
	err := r.db.QueryRowContext(ctx, `SELECT acked_sequence FROM receive_streams WHERE platform_run_id=?`, runID).Scan(&value)
	return value, err
}

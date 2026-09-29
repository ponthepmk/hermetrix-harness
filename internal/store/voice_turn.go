package store

import (
	"context"
	"database/sql"
)

// A finalized voice utterance may be retried after reconnect. Its user turn
// is committed once; partial hypotheses never enter this table.
func migrateV66(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agent_turn_idempotency (
  session_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(session_id,idempotency_key),
  FOREIGN KEY(session_id) REFERENCES agent_sessions(id) ON DELETE CASCADE
);`)
	return err
}

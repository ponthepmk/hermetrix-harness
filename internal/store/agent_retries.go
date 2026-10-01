package store

import (
	"context"
	"database/sql"
)

func migrateV74(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agent_retries (
 session_id TEXT PRIMARY KEY REFERENCES agent_sessions(id) ON DELETE CASCADE,
 source_event_id TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('pending','running','paused','completed','cancelled')),
 next_attempt_at TEXT NOT NULL,
 attempts INTEGER NOT NULL DEFAULT 0,
 updated_at TEXT NOT NULL
 ); CREATE INDEX IF NOT EXISTS idx_agent_retries_due ON agent_retries(state,next_attempt_at);
 CREATE TABLE IF NOT EXISTS agent_approval_modes (
 session_id TEXT PRIMARY KEY REFERENCES agent_sessions(id) ON DELETE CASCADE,
 mode TEXT NOT NULL CHECK(mode IN ('ask_all','auto')),
 actor TEXT NOT NULL
 );`)
	return err
}

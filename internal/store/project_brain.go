package store

import (
	"context"
	"database/sql"
)

// A submitted knowledge candidate is an independent transport message. It is
// not an EffectIntent and must never be replayed through task execution.
func migrateV50(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS project_brain_candidate_outbox (
  candidate_id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  payload_digest TEXT NOT NULL,
  approval_json TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('pending','sending','submitted','blocked','conflict')),
  attempt_count INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  remote_commit TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  submitted_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_project_brain_candidate_outbox_due
  ON project_brain_candidate_outbox(state,next_attempt_at,created_at);
`)
	return err
}

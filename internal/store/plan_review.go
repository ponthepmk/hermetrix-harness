package store

import (
	"context"
	"database/sql"
)

// v51 records a human decision over an immutable plan specification. Rejected
// revisions remain inspectable; a later plan receives a new revision number.
func migrateV51(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS task_plan_reviews (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  plan_revision INTEGER NOT NULL,
  requirement_revision INTEGER NOT NULL,
  subject_hash TEXT NOT NULL,
  verdict TEXT NOT NULL CHECK(verdict IN ('approved','rejected')),
  actor TEXT NOT NULL,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_task_plan_reviews_latest ON task_plan_reviews(task_id,plan_revision,created_at DESC);
`)
	return err
}

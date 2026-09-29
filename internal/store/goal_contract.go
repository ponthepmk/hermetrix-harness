package store

import (
	"context"
	"database/sql"
)

// v50 keeps optional goal metadata with its immutable requirement revision and
// records user acceptance against the exact validated subject, not a task id.
func migrateV50(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(task_requirement_revisions)`)
	if err != nil {
		return err
	}
	hasContract := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == "contract_json" {
			hasContract = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !hasContract {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE task_requirement_revisions ADD COLUMN contract_json TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS task_user_acceptances (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  requirement_revision INTEGER NOT NULL,
  plan_revision INTEGER NOT NULL,
  subject_hash TEXT NOT NULL,
  actor TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(task_id,requirement_revision,plan_revision,subject_hash,actor),
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_task_user_acceptances_subject
  ON task_user_acceptances(task_id,requirement_revision,plan_revision,subject_hash);
`)
	return err
}

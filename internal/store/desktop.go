package store

import (
	"context"
	"database/sql"
)

// v52 persists an exact native desktop action before dispatch. Dispatched
// actions are never retried automatically after a process interruption.
func migrateV52(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS desktop_actions (
  id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL,
  window_handle INTEGER NOT NULL,
  process_id INTEGER NOT NULL,
  action TEXT NOT NULL CHECK(action IN ('invoke','set_value')),
  element_ref INTEGER NOT NULL,
  target_json TEXT NOT NULL,
  text_hash TEXT NOT NULL,
  observation_hash TEXT NOT NULL,
  subject_hash TEXT NOT NULL,
  actor TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK(state IN ('prepared','cancelled','dispatched','completed','uncertain')),
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  dispatched_at TEXT,
  completed_at TEXT,
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id)
);
CREATE INDEX IF NOT EXISTS idx_desktop_actions_owner_state ON desktop_actions(owner_principal_id,state,created_at DESC);
`)
	return err
}

// v60 lets a native action bind to a Task without changing the window-level
// approval contract. Task completion can then see unresolved local effects.
func migrateV60(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(desktop_actions)`)
	if err != nil {
		return err
	}
	hasTaskID := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "task_id" {
			hasTaskID = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !hasTaskID {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE desktop_actions ADD COLUMN task_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_desktop_actions_task ON desktop_actions(task_id,state,created_at DESC)`)
	return err
}

// v61 records a human reconciliation against a fresh screenshot when native
// dispatch may have happened but its result could not be observed. The action
// remains uncertain; the separate receipt preserves that history.
func migrateV61(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS desktop_action_reconciliations (
  action_id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL,
  resolution TEXT NOT NULL CHECK(resolution IN ('observed_applied','observed_not_applied')),
  observation_hash TEXT NOT NULL,
  screenshot_artifact_id TEXT NOT NULL,
  actor TEXT NOT NULL,
  note TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(action_id) REFERENCES desktop_actions(id),
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id)
);
`)
	return err
}

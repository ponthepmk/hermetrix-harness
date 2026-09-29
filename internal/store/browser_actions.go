package store

import (
	"context"
	"database/sql"
)

// v65 makes browser click/type/key a one-use, exact-subject action. Interrupted
// dispatch remains unresolved and is never replayed when the app restarts.
func migrateV65(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS browser_action_receipts (
  id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL,
  task_id TEXT NOT NULL DEFAULT '',
  tab_id TEXT NOT NULL,
  action TEXT NOT NULL CHECK(action IN ('click','type','key')),
  element_ref INTEGER NOT NULL,
  target_json TEXT NOT NULL,
  text_hash TEXT NOT NULL,
  observation_hash TEXT NOT NULL,
  subject_hash TEXT NOT NULL,
  screenshot_before_id TEXT NOT NULL DEFAULT '',
  screenshot_after_id TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK(state IN ('prepared','cancelled','dispatched','completed','uncertain')),
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  dispatched_at TEXT,
  completed_at TEXT,
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id),
  FOREIGN KEY(tab_id) REFERENCES browser_tabs(id)
);
CREATE INDEX IF NOT EXISTS idx_browser_actions_task ON browser_action_receipts(task_id,state,created_at DESC);
CREATE INDEX IF NOT EXISTS idx_browser_actions_tab ON browser_action_receipts(owner_principal_id,tab_id,state);
CREATE TABLE IF NOT EXISTS browser_action_reconciliations (
  action_id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL,
  resolution TEXT NOT NULL CHECK(resolution IN ('observed_applied','observed_not_applied')),
  observation_hash TEXT NOT NULL,
  screenshot_artifact_id TEXT NOT NULL,
  verification_tab_id TEXT NOT NULL,
  actor TEXT NOT NULL,
  note TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(action_id) REFERENCES browser_action_receipts(id),
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id),
  FOREIGN KEY(verification_tab_id) REFERENCES browser_tabs(id)
);
`)
	return err
}

package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// v72 widens the receipt action constraint while keeping old approvals and
// reconciliations intact. SQLite cannot alter a CHECK constraint in place.
func migrateV72(ctx context.Context, tx *sql.Tx) error {
	var schema string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='browser_action_receipts'`).Scan(&schema); err != nil {
		return err
	}
	if strings.Contains(schema, "'key'") {
		return nil
	}
	// The reconciliation table is the only child of receipts. Save it inside
	// this transaction, remove the child, then recreate it after the parent
	// swap so no foreign key points at a dropped table at commit.
	if _, err := tx.ExecContext(ctx, `CREATE TABLE browser_action_reconciliations_v72_backup AS SELECT * FROM browser_action_reconciliations;
DROP TABLE browser_action_reconciliations;
CREATE TABLE browser_action_receipts_v72 (
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
INSERT INTO browser_action_receipts_v72 SELECT * FROM browser_action_receipts;
DROP TABLE browser_action_receipts;
ALTER TABLE browser_action_receipts_v72 RENAME TO browser_action_receipts;`); err != nil {
		return fmt.Errorf("widen browser receipt actions: %w", err)
	}
	if err := migrateV65(ctx, tx); err != nil {
		return fmt.Errorf("restore browser receipt indexes and reconciliation table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO browser_action_reconciliations SELECT * FROM browser_action_reconciliations_v72_backup;
DROP TABLE browser_action_reconciliations_v72_backup;`); err != nil {
		return fmt.Errorf("restore browser action reconciliations: %w", err)
	}
	return nil
}

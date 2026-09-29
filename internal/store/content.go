package store

import (
	"context"
	"database/sql"
)

// v54 keeps editorial inputs, claims, drafts and approvals owned by one Task.
// Approval is a receipt for one exact draft subject hash, never a global grant.
func migrateV54(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS task_content_briefs (
  task_id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL,
  revision INTEGER NOT NULL DEFAULT 1,
  topic TEXT NOT NULL,
  audience TEXT NOT NULL,
  language TEXT NOT NULL,
  facebook_page TEXT NOT NULL DEFAULT '',
  tiktok_account TEXT NOT NULL DEFAULT '',
  freshness_days INTEGER NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE,
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id)
);
CREATE TABLE IF NOT EXISTS task_content_claims (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  owner_principal_id TEXT NOT NULL,
  source_id TEXT NOT NULL,
  source_digest TEXT NOT NULL,
  statement TEXT NOT NULL,
  evidence_excerpt TEXT NOT NULL,
  review_status TEXT NOT NULL DEFAULT 'pending',
  reviewer TEXT NOT NULL DEFAULT '',
  reviewed_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE,
  FOREIGN KEY(source_id) REFERENCES task_research_candidates(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_task_content_claims ON task_content_claims(task_id,created_at);
CREATE TABLE IF NOT EXISTS task_content_drafts (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  owner_principal_id TEXT NOT NULL,
  platform TEXT NOT NULL CHECK(platform IN ('facebook','tiktok')),
  destination TEXT NOT NULL,
  body TEXT NOT NULL,
  script TEXT NOT NULL DEFAULT '',
  storyboard TEXT NOT NULL DEFAULT '',
  media_artifact_id TEXT NOT NULL DEFAULT '',
  revision INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_task_content_drafts ON task_content_drafts(task_id,platform,updated_at);
CREATE TABLE IF NOT EXISTS task_content_approvals (
  id TEXT PRIMARY KEY,
  draft_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  owner_principal_id TEXT NOT NULL,
  subject_hash TEXT NOT NULL,
  actor TEXT NOT NULL,
  approved_at TEXT NOT NULL,
  FOREIGN KEY(draft_id) REFERENCES task_content_drafts(id) ON DELETE CASCADE,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE,
  UNIQUE(draft_id,subject_hash)
);
`)
	return err
}

// v64 records the second source required when an editorial claim is marked
// consequential. Existing claims remain ordinary until explicitly reviewed.
func migrateV64(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []struct{ name, definition string }{
		{"consequential", "INTEGER NOT NULL DEFAULT 0"},
		{"corroborating_source_id", "TEXT NOT NULL DEFAULT ''"},
		{"corroborating_source_digest", "TEXT NOT NULL DEFAULT ''"},
		{"corroborating_excerpt", "TEXT NOT NULL DEFAULT ''"},
	} {
		rows, err := tx.QueryContext(ctx, `PRAGMA table_info(task_content_claims)`)
		if err != nil {
			return err
		}
		found := false
		for rows.Next() {
			var cid, notNull, pk int
			var name, kind string
			var defaultValue sql.NullString
			if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
				rows.Close()
				return err
			}
			found = found || name == column.name
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if !found {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE task_content_claims ADD COLUMN `+column.name+` `+column.definition); err != nil {
				return err
			}
		}
	}
	return nil
}

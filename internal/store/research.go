package store

import (
	"context"
	"database/sql"
)

// v53 keeps research metadata and selection tied to one Task. Source metadata
// is untrusted evidence; selection does not assert that a claim is supported.
func migrateV53(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS task_research_candidates (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  owner_principal_id TEXT NOT NULL,
  query TEXT NOT NULL,
  source_kind TEXT NOT NULL,
  doi TEXT NOT NULL,
  title TEXT NOT NULL,
  publisher TEXT NOT NULL,
  source_url TEXT NOT NULL,
  published_date TEXT NOT NULL,
  published_precision TEXT NOT NULL,
  retrieved_at TEXT NOT NULL,
  source_digest TEXT NOT NULL,
  selected INTEGER NOT NULL DEFAULT 0 CHECK(selected IN (0,1)),
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE,
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id)
);
CREATE INDEX IF NOT EXISTS idx_task_research_candidates ON task_research_candidates(task_id,retrieved_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_research_doi ON task_research_candidates(task_id,doi);
`)
	return err
}

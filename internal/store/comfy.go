package store

import (
	"context"
	"database/sql"
)

// v55 records a submitted local Comfy workflow before its effectful MCP call.
// A process lost during submission leaves an uncertain record, never a retry.
func migrateV55(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS task_comfy_jobs (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  owner_principal_id TEXT NOT NULL,
  workflow_path TEXT NOT NULL,
  workflow_hash TEXT NOT NULL,
  staged_path TEXT NOT NULL,
  operation_id TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('submitting','queued','running','completed','failed','cancelled','uncertain')),
  prompt_id TEXT NOT NULL DEFAULT '',
  result_artifact_ids_json TEXT NOT NULL DEFAULT '[]',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE,
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id)
);
CREATE INDEX IF NOT EXISTS idx_task_comfy_jobs_task ON task_comfy_jobs(task_id,created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_comfy_jobs_prompt ON task_comfy_jobs(prompt_id) WHERE prompt_id<>'';
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_comfy_jobs_operation ON task_comfy_jobs(owner_principal_id,operation_id);
`)
	return err
}

package store

import (
	"context"
	"database/sql"
)

// migrateV49 adds the fixture-only managed-agent adapter records. The tables
// cross-reference existing projects, tasks and artifacts; they do not create a
// second execution engine or a Pi-side schema.
func migrateV49(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS agent_platform_bindings (
  binding_id TEXT PRIMARY KEY,
  platform_id TEXT NOT NULL,
  node_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  repository_id TEXT NOT NULL,
  worktree_id TEXT NOT NULL DEFAULT '',
  project_id TEXT NOT NULL,
  owner_principal_id TEXT NOT NULL,
  canonical_root TEXT NOT NULL,
  git_common_dir TEXT NOT NULL,
  git_worktree_dir TEXT NOT NULL,
  repository_fingerprint TEXT NOT NULL,
  worktree_fingerprint TEXT NOT NULL,
  binding_revision INTEGER NOT NULL CHECK(binding_revision >= 1),
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(platform_id,node_id,repository_id,worktree_id),
  FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE RESTRICT,
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id) ON DELETE RESTRICT
);
CREATE TABLE IF NOT EXISTS agent_platform_assignments (
  platform_id TEXT NOT NULL,
  assignment_id TEXT NOT NULL,
  assignment_revision INTEGER NOT NULL CHECK(assignment_revision >= 1),
  platform_task_id TEXT NOT NULL,
  platform_run_id TEXT NOT NULL,
  node_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  binding_id TEXT NOT NULL,
  binding_revision INTEGER NOT NULL CHECK(binding_revision >= 1),
  harness_task_id TEXT NOT NULL,
  payload_digest TEXT NOT NULL,
  immutable_envelope BLOB NOT NULL,
  accepted_claim BLOB NOT NULL,
  access TEXT NOT NULL CHECK(access IN ('none','read')),
  intake_state TEXT NOT NULL CHECK(intake_state IN ('projected','observed')),
  created_at TEXT NOT NULL,
  PRIMARY KEY(platform_id,assignment_id,assignment_revision),
  UNIQUE(platform_id,assignment_id),
  UNIQUE(platform_id,platform_task_id),
  UNIQUE(platform_id,platform_run_id),
  UNIQUE(harness_task_id),
  FOREIGN KEY(binding_id) REFERENCES agent_platform_bindings(binding_id) ON DELETE RESTRICT,
  FOREIGN KEY(harness_task_id) REFERENCES durable_tasks(id) ON DELETE RESTRICT
);
CREATE TABLE IF NOT EXISTS agent_platform_streams (
  platform_id TEXT NOT NULL,
  platform_run_id TEXT NOT NULL,
  assignment_id TEXT NOT NULL,
  assignment_revision INTEGER NOT NULL,
  next_sequence INTEGER NOT NULL DEFAULT 1 CHECK(next_sequence >= 1),
  acked_sequence INTEGER NOT NULL DEFAULT 0 CHECK(acked_sequence >= 0),
  updated_at TEXT NOT NULL,
  PRIMARY KEY(platform_id,platform_run_id),
  FOREIGN KEY(platform_id,assignment_id,assignment_revision)
    REFERENCES agent_platform_assignments(platform_id,assignment_id,assignment_revision) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS agent_platform_outbox (
  platform_id TEXT NOT NULL,
  platform_run_id TEXT NOT NULL,
  sequence INTEGER NOT NULL CHECK(sequence >= 1),
  observation_key TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  payload_digest TEXT NOT NULL,
  immutable_envelope BLOB NOT NULL,
  projection_artifact_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  delivery_status TEXT NOT NULL CHECK(delivery_status IN ('pending','failed','acked')),
  attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts >= 0),
  next_attempt_at TEXT,
  last_error TEXT,
  acked_at TEXT,
  PRIMARY KEY(platform_id,platform_run_id,sequence),
  UNIQUE(platform_id,idempotency_key),
  UNIQUE(platform_id,platform_run_id,observation_key),
  FOREIGN KEY(platform_id,platform_run_id) REFERENCES agent_platform_streams(platform_id,platform_run_id) ON DELETE CASCADE,
  FOREIGN KEY(projection_artifact_id) REFERENCES artifacts(id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS idx_agent_platform_outbox_delivery
  ON agent_platform_outbox(delivery_status,next_attempt_at,created_at);
CREATE TRIGGER IF NOT EXISTS agent_platform_assignment_immutable
BEFORE UPDATE OF platform_task_id,platform_run_id,node_id,agent_id,binding_id,binding_revision,
  harness_task_id,payload_digest,immutable_envelope,accepted_claim,access,created_at
ON agent_platform_assignments BEGIN SELECT RAISE(ABORT,'agent platform assignment is immutable'); END;
CREATE TRIGGER IF NOT EXISTS agent_platform_outbox_immutable
BEFORE UPDATE OF platform_run_id,sequence,observation_key,idempotency_key,payload_digest,
  immutable_envelope,projection_artifact_id,created_at
ON agent_platform_outbox BEGIN SELECT RAISE(ABORT,'agent platform outbox payload is immutable'); END;
`)
	return err
}

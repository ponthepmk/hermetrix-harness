package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"hermetrix-harness/internal/blob"
)

type Store struct {
	DB    *sql.DB
	Blobs *blob.Store
	Root  string
}

func Open(ctx context.Context, root string) (*Store, error) {
	if root == "" {
		return nil, fmt.Errorf("data root is empty")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create data root: %w", err)
	}
	blobs, err := blob.Open(filepath.Join(root, "blobs", "sha256"))
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(root, "hermetrix.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure sqlite: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db, Blobs: blobs, Root: root}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// SchemaVersion reports what the open database actually says, not what the
// build intended. Those are the same number when migration succeeded and
// different when it did not, which is the whole reason to ask.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := s.DB.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version < 1 {
		if _, err := tx.ExecContext(ctx, schemaV1); err != nil {
			return fmt.Errorf("apply schema v1: %w", err)
		}
	}
	if version < 2 {
		if _, err := tx.ExecContext(ctx, schemaV2); err != nil {
			return fmt.Errorf("apply schema v2: %w", err)
		}
	}
	if version < 3 {
		if _, err := tx.ExecContext(ctx, schemaV3); err != nil {
			return fmt.Errorf("apply schema v3: %w", err)
		}
	}
	if version < 4 {
		if _, err := tx.ExecContext(ctx, schemaV4); err != nil {
			return fmt.Errorf("apply schema v4: %w", err)
		}
	}
	if version < 5 {
		if _, err := tx.ExecContext(ctx, schemaV5); err != nil {
			return fmt.Errorf("apply schema v5: %w", err)
		}
	}
	if version < 6 {
		if _, err := tx.ExecContext(ctx, schemaV6); err != nil {
			return fmt.Errorf("apply schema v6: %w", err)
		}
	}
	if version < 7 {
		if _, err := tx.ExecContext(ctx, schemaV7); err != nil {
			return fmt.Errorf("apply schema v7: %w", err)
		}
	}
	if version < 8 {
		if _, err := tx.ExecContext(ctx, schemaV8); err != nil {
			return fmt.Errorf("apply schema v8: %w", err)
		}
	}
	if version < 9 {
		if _, err := tx.ExecContext(ctx, schemaV9); err != nil {
			return fmt.Errorf("apply schema v9: %w", err)
		}
	}
	if version < 10 {
		if _, err := tx.ExecContext(ctx, schemaV10); err != nil {
			return fmt.Errorf("apply schema v10: %w", err)
		}
	}
	if version < 11 {
		if _, err := tx.ExecContext(ctx, schemaV11); err != nil {
			return fmt.Errorf("apply schema v11: %w", err)
		}
	}
	if version < 12 {
		if err := migrateV12(ctx, tx); err != nil {
			return fmt.Errorf("apply schema v12: %w", err)
		}
	}
	if version < 13 {
		if _, err := tx.ExecContext(ctx, schemaV13); err != nil {
			return fmt.Errorf("apply schema v13: %w", err)
		}
	}
	if version < 14 {
		if _, err := tx.ExecContext(ctx, schemaV14); err != nil {
			return fmt.Errorf("apply schema v14: %w", err)
		}
	}
	if version < 15 {
		if _, err := tx.ExecContext(ctx, schemaV15); err != nil {
			return fmt.Errorf("apply schema v15: %w", err)
		}
	}
	if version < 16 {
		if _, err := tx.ExecContext(ctx, schemaV16); err != nil {
			return fmt.Errorf("apply schema v16: %w", err)
		}
	}
	if version < 17 {
		if _, err := tx.ExecContext(ctx, schemaV17); err != nil {
			return fmt.Errorf("apply schema v17: %w", err)
		}
	}
	if version < 18 {
		if _, err := tx.ExecContext(ctx, schemaV18); err != nil {
			return fmt.Errorf("apply schema v18: %w", err)
		}
	}
	if version < 19 {
		if _, err := tx.ExecContext(ctx, schemaV19); err != nil {
			return fmt.Errorf("apply schema v19: %w", err)
		}
	}
	if version < 20 {
		if _, err := tx.ExecContext(ctx, schemaV20); err != nil {
			return fmt.Errorf("apply schema v20: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, CurrentSchemaVersion)); err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}

// CurrentSchemaVersion is the version Open migrates to. Tests assert against
// this rather than a literal, so adding a migration does not break a test that
// was never about the number.
const CurrentSchemaVersion = 20

const schemaV1 = `
CREATE TABLE IF NOT EXISTS skills (
  id TEXT PRIMARY KEY,
  canonical_name TEXT NOT NULL,
  scope_kind TEXT NOT NULL,
  scope_ref TEXT NOT NULL DEFAULT '',
  origin TEXT NOT NULL,
  owner TEXT NOT NULL,
  state TEXT NOT NULL,
  current_version_id TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  pinned INTEGER NOT NULL DEFAULT 0,
  protected INTEGER NOT NULL DEFAULT 0,
  absorbed_into_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(scope_kind, scope_ref, canonical_name),
  FOREIGN KEY(absorbed_into_id) REFERENCES skills(id)
);

CREATE TABLE IF NOT EXISTS skill_versions (
  id TEXT PRIMARY KEY,
  skill_id TEXT NOT NULL,
  parent_version_id TEXT,
  content_hash TEXT NOT NULL,
  package_blob_ref TEXT NOT NULL,
  manifest_json TEXT NOT NULL,
  author_actor TEXT NOT NULL,
  source_event_id TEXT,
  change_message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY(skill_id) REFERENCES skills(id),
  FOREIGN KEY(parent_version_id) REFERENCES skill_versions(id)
);

CREATE TABLE IF NOT EXISTS skill_candidates (
  id TEXT PRIMARY KEY,
  canonical_name TEXT NOT NULL,
  scope_kind TEXT NOT NULL,
  scope_ref TEXT NOT NULL DEFAULT '',
  origin TEXT NOT NULL,
  owner TEXT NOT NULL,
  change_kind TEXT NOT NULL,
  target_skill_id TEXT,
  base_version_id TEXT,
  candidate_blob_ref TEXT NOT NULL,
  candidate_hash TEXT NOT NULL,
  created_by TEXT NOT NULL,
  trigger_kind TEXT NOT NULL,
  reason TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  state TEXT NOT NULL,
  checks_json TEXT NOT NULL,
  revision INTEGER NOT NULL DEFAULT 1,
  reviewed_by TEXT,
  review_reason TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(target_skill_id) REFERENCES skills(id),
  FOREIGN KEY(base_version_id) REFERENCES skill_versions(id)
);

CREATE TABLE IF NOT EXISTS skill_events (
  id TEXT PRIMARY KEY,
  skill_id TEXT,
  version_id TEXT,
  candidate_id TEXT,
  event_kind TEXT NOT NULL,
  actor_kind TEXT NOT NULL,
  actor_ref TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL DEFAULT '',
  job_id TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(skill_id) REFERENCES skills(id),
  FOREIGN KEY(version_id) REFERENCES skill_versions(id),
  FOREIGN KEY(candidate_id) REFERENCES skill_candidates(id)
);

CREATE TABLE IF NOT EXISTS skill_activations (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  job_id TEXT NOT NULL DEFAULT '',
  skill_id TEXT NOT NULL,
  version_id TEXT NOT NULL,
  selection_source TEXT NOT NULL,
  selection_reason TEXT NOT NULL DEFAULT '',
  metadata_exposed INTEGER NOT NULL,
  body_injected INTEGER NOT NULL,
  relevant_tool_calls_json TEXT NOT NULL,
  outcome TEXT NOT NULL,
  outcome_source TEXT NOT NULL,
  attribution_kind TEXT NOT NULL,
  attribution_score REAL,
  created_at TEXT NOT NULL,
  completed_at TEXT,
  FOREIGN KEY(skill_id) REFERENCES skills(id),
  FOREIGN KEY(version_id) REFERENCES skill_versions(id)
);

CREATE TABLE IF NOT EXISTS skill_archives (
  id TEXT PRIMARY KEY,
  skill_id TEXT NOT NULL,
  archived_version_id TEXT NOT NULL,
  package_blob_ref TEXT NOT NULL,
  previous_state TEXT NOT NULL,
  previous_enabled INTEGER NOT NULL,
  previous_pinned INTEGER NOT NULL,
  reason TEXT NOT NULL,
  actor_kind TEXT NOT NULL,
  absorbed_into_id TEXT,
  created_at TEXT NOT NULL,
  restored_candidate_id TEXT,
  restored_at TEXT,
  FOREIGN KEY(skill_id) REFERENCES skills(id),
  FOREIGN KEY(archived_version_id) REFERENCES skill_versions(id)
);

CREATE TABLE IF NOT EXISTS skill_relations (
  id TEXT PRIMARY KEY,
  left_skill_id TEXT NOT NULL,
  left_version_id TEXT NOT NULL,
  right_skill_id TEXT NOT NULL,
  right_version_id TEXT NOT NULL,
  relation_kind TEXT NOT NULL,
  score REAL NOT NULL,
  evidence_json TEXT NOT NULL,
  analyzer_kind TEXT NOT NULL,
  analyzer_version TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(left_version_id, right_version_id, analyzer_kind, analyzer_version)
);

CREATE INDEX IF NOT EXISTS idx_candidates_state ON skill_candidates(state, created_at);
CREATE INDEX IF NOT EXISTS idx_events_skill ON skill_events(skill_id, created_at);
CREATE INDEX IF NOT EXISTS idx_activations_skill ON skill_activations(skill_id, created_at);
CREATE INDEX IF NOT EXISTS idx_archives_skill ON skill_archives(skill_id, created_at);
`

const schemaV2 = `
ALTER TABLE skill_candidates ADD COLUMN source_review_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_candidates_source_review ON skill_candidates(source_review_id) WHERE source_review_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS learning_reviews (
  id TEXT PRIMARY KEY,
  idempotency_key TEXT NOT NULL UNIQUE,
  state TEXT NOT NULL,
  trigger_kind TEXT NOT NULL,
  session_id TEXT NOT NULL,
  job_id TEXT NOT NULL DEFAULT '',
  digest_json TEXT NOT NULL,
  reviewer_revision TEXT NOT NULL,
  decision_json TEXT NOT NULL DEFAULT '{}',
  candidate_id TEXT,
  attempts INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  FOREIGN KEY(candidate_id) REFERENCES skill_candidates(id)
);

CREATE TABLE IF NOT EXISTS curator_runs (
  id TEXT PRIMARY KEY,
  mode TEXT NOT NULL,
  state TEXT NOT NULL,
  analyzer_revision TEXT NOT NULL,
  input_snapshot_json TEXT NOT NULL,
  findings_count INTEGER NOT NULL DEFAULT 0,
  proposals_count INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_learning_reviews_state ON learning_reviews(state, created_at);
CREATE INDEX IF NOT EXISTS idx_curator_runs_started ON curator_runs(started_at);
`

const schemaV3 = `
CREATE TABLE IF NOT EXISTS provider_profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  adapter_kind TEXT NOT NULL,
  base_url TEXT NOT NULL,
  model TEXT NOT NULL,
  api_key_env TEXT NOT NULL DEFAULT '',
  context_window INTEGER NOT NULL,
  context_evidence TEXT NOT NULL DEFAULT 'declared',
  max_output_tokens INTEGER NOT NULL DEFAULT 4096,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_sessions (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  context_profile TEXT NOT NULL,
  state TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(provider_id) REFERENCES provider_profiles(id)
);

CREATE TABLE IF NOT EXISTS agent_events (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  event_kind TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  provider_id TEXT,
  model TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(session_id, sequence),
  FOREIGN KEY(session_id) REFERENCES agent_sessions(id),
  FOREIGN KEY(provider_id) REFERENCES provider_profiles(id)
);

CREATE TABLE IF NOT EXISTS context_snapshots (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  model TEXT NOT NULL,
  profile_name TEXT NOT NULL,
  compiled_json TEXT NOT NULL,
  report_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(session_id) REFERENCES agent_sessions(id),
  FOREIGN KEY(provider_id) REFERENCES provider_profiles(id)
);

CREATE TABLE IF NOT EXISTS step_bindings (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  step_number INTEGER NOT NULL,
  provider_id TEXT NOT NULL,
  model TEXT NOT NULL,
  context_snapshot_id TEXT NOT NULL,
  capability_revision TEXT NOT NULL,
  policy_revision TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(session_id, turn_id, step_number),
  FOREIGN KEY(session_id) REFERENCES agent_sessions(id),
  FOREIGN KEY(provider_id) REFERENCES provider_profiles(id),
  FOREIGN KEY(context_snapshot_id) REFERENCES context_snapshots(id)
);

CREATE INDEX IF NOT EXISTS idx_agent_sessions_updated ON agent_sessions(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_events_session ON agent_events(session_id, sequence);
CREATE INDEX IF NOT EXISTS idx_context_snapshots_turn ON context_snapshots(session_id, turn_id);
CREATE INDEX IF NOT EXISTS idx_step_bindings_turn ON step_bindings(session_id, turn_id, step_number);
`

const schemaV4 = `
ALTER TABLE step_bindings ADD COLUMN tool_bindings_json TEXT NOT NULL DEFAULT '[]';
`

const schemaV5 = `
CREATE TABLE IF NOT EXISTS tool_approvals (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  step_binding_id TEXT NOT NULL,
  step_number INTEGER NOT NULL,
  tool_call_id TEXT NOT NULL UNIQUE,
  tool_name TEXT NOT NULL,
  tool_revision TEXT NOT NULL,
  effect TEXT NOT NULL,
  arguments_json TEXT NOT NULL,
  arguments_hash TEXT NOT NULL,
  summary TEXT NOT NULL,
  preview TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  state TEXT NOT NULL,
  requested_at TEXT NOT NULL,
  decided_at TEXT,
  decided_by TEXT NOT NULL DEFAULT '',
  decision_reason TEXT NOT NULL DEFAULT '',
  receipt_event_id TEXT,
  FOREIGN KEY(session_id) REFERENCES agent_sessions(id),
  FOREIGN KEY(step_binding_id) REFERENCES step_bindings(id),
  FOREIGN KEY(receipt_event_id) REFERENCES agent_events(id)
);

CREATE INDEX IF NOT EXISTS idx_tool_approvals_session ON tool_approvals(session_id, requested_at);
CREATE INDEX IF NOT EXISTS idx_tool_approvals_state ON tool_approvals(state, requested_at);
`

const schemaV6 = `
CREATE TABLE IF NOT EXISTS mcp_servers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  transport_kind TEXT NOT NULL,
  endpoint TEXT NOT NULL,
  api_key_env TEXT NOT NULL DEFAULT '',
  protocol_mode TEXT NOT NULL DEFAULT 'auto',
  trust_annotations INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  request_timeout_ms INTEGER NOT NULL DEFAULT 15000,
  status TEXT NOT NULL DEFAULT 'not_discovered',
  last_error TEXT NOT NULL DEFAULT '',
  last_protocol TEXT NOT NULL DEFAULT '',
  tool_count INTEGER NOT NULL DEFAULT 0,
  last_discovered_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS mcp_tools (
  server_id TEXT NOT NULL,
  remote_name TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  input_schema_json TEXT NOT NULL,
  output_schema_json TEXT NOT NULL DEFAULT '',
  annotations_json TEXT NOT NULL DEFAULT '',
  revision TEXT NOT NULL,
  effect TEXT NOT NULL,
  requires_approval INTEGER NOT NULL,
  discovered_at TEXT NOT NULL,
  PRIMARY KEY(server_id, remote_name),
  FOREIGN KEY(server_id) REFERENCES mcp_servers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_mcp_tools_server ON mcp_tools(server_id, remote_name);
CREATE INDEX IF NOT EXISTS idx_mcp_servers_status ON mcp_servers(status, updated_at);
`

const schemaV7 = `
CREATE TABLE IF NOT EXISTS skill_replay_runs (
  id TEXT PRIMARY KEY,
  candidate_id TEXT NOT NULL,
  candidate_revision INTEGER NOT NULL,
  candidate_hash TEXT NOT NULL,
  base_version_id TEXT NOT NULL DEFAULT '',
  runner_revision TEXT NOT NULL,
  state TEXT NOT NULL,
  fixtures_total INTEGER NOT NULL DEFAULT 0,
  baseline_passed INTEGER NOT NULL DEFAULT 0,
  candidate_passed INTEGER NOT NULL DEFAULT 0,
  regressions INTEGER NOT NULL DEFAULT 0,
  improvements INTEGER NOT NULL DEFAULT 0,
  result_json TEXT NOT NULL DEFAULT '{}',
  diff_text TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  completed_at TEXT,
  FOREIGN KEY(candidate_id) REFERENCES skill_candidates(id)
);

CREATE TABLE IF NOT EXISTS skill_replay_cases (
  run_id TEXT NOT NULL,
  case_id TEXT NOT NULL,
  fixture_path TEXT NOT NULL,
  baseline_passed INTEGER NOT NULL,
  candidate_passed INTEGER NOT NULL,
  details_json TEXT NOT NULL,
  PRIMARY KEY(run_id, case_id),
  FOREIGN KEY(run_id) REFERENCES skill_replay_runs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS candidate_capability_reviews (
  candidate_id TEXT NOT NULL,
  candidate_revision INTEGER NOT NULL,
  actor TEXT NOT NULL,
  decision TEXT NOT NULL,
  added_tools_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(candidate_id, candidate_revision),
  FOREIGN KEY(candidate_id) REFERENCES skill_candidates(id)
);

CREATE INDEX IF NOT EXISTS idx_skill_replay_candidate ON skill_replay_runs(candidate_id, started_at DESC);
`

const schemaV8 = `
CREATE TABLE IF NOT EXISTS context_eval_cases (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  language TEXT NOT NULL,
  benchmark_class TEXT NOT NULL,
  fragments_json TEXT NOT NULL,
  expectations_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS context_eval_runs (
  id TEXT PRIMARY KEY,
  case_id TEXT NOT NULL,
  profile_name TEXT NOT NULL,
  compiler_revision TEXT NOT NULL,
  verifier_revision TEXT NOT NULL,
  state TEXT NOT NULL,
  metrics_json TEXT NOT NULL DEFAULT '{}',
  full_blob_ref TEXT NOT NULL DEFAULT '',
  compiled_blob_ref TEXT NOT NULL DEFAULT '',
  fallback_used INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  completed_at TEXT,
  FOREIGN KEY(case_id) REFERENCES context_eval_cases(id)
);

CREATE INDEX IF NOT EXISTS idx_context_eval_runs_case ON context_eval_runs(case_id, started_at DESC);
`

const schemaV9 = `
CREATE TABLE IF NOT EXISTS model_qualification_runs (
  id TEXT PRIMARY KEY,
  provider_id TEXT,
  runtime_kind TEXT NOT NULL DEFAULT '',
  runtime_endpoint TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL,
  suite_revision TEXT NOT NULL,
  state TEXT NOT NULL,
  declared_context INTEGER NOT NULL DEFAULT 0,
  allocated_context INTEGER NOT NULL DEFAULT 0,
  context_tier TEXT NOT NULL DEFAULT 'limited',
  capability_grade TEXT NOT NULL DEFAULT 'C',
  results_json TEXT NOT NULL DEFAULT '{}',
  remediation_json TEXT NOT NULL DEFAULT '[]',
  error TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  completed_at TEXT,
  FOREIGN KEY(provider_id) REFERENCES provider_profiles(id)
);

CREATE INDEX IF NOT EXISTS idx_model_qualification_provider ON model_qualification_runs(provider_id, started_at DESC);
`

const schemaV10 = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  root_path TEXT NOT NULL UNIQUE,
  state TEXT NOT NULL DEFAULT 'active',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

ALTER TABLE agent_sessions ADD COLUMN project_id TEXT;

CREATE TABLE IF NOT EXISTS artifacts (
  id TEXT PRIMARY KEY,
  project_id TEXT,
  session_id TEXT,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  blob_ref TEXT NOT NULL,
  byte_size INTEGER NOT NULL,
  checksum TEXT NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  FOREIGN KEY(project_id) REFERENCES projects(id),
  FOREIGN KEY(session_id) REFERENCES agent_sessions(id)
);

CREATE TABLE IF NOT EXISTS background_jobs (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  state TEXT NOT NULL,
  progress REAL NOT NULL DEFAULT 0,
  payload_json TEXT NOT NULL DEFAULT '{}',
  result_json TEXT NOT NULL DEFAULT '{}',
  error TEXT NOT NULL DEFAULT '',
  cancel_requested INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT
);

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS memories (
  id TEXT PRIMARY KEY,
  scope_kind TEXT NOT NULL,
  scope_ref TEXT NOT NULL DEFAULT '',
  memory_kind TEXT NOT NULL,
  content TEXT NOT NULL,
  source TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'active',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS backup_runs (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  state TEXT NOT NULL,
  format_version INTEGER NOT NULL,
  manifest_blob_ref TEXT NOT NULL DEFAULT '',
  checksum TEXT NOT NULL DEFAULT '',
  counts_json TEXT NOT NULL DEFAULT '{}',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_artifacts_project ON artifacts(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_background_jobs_state ON background_jobs(state, created_at);
CREATE INDEX IF NOT EXISTS idx_memories_scope ON memories(scope_kind, scope_ref, state);
`

const schemaV11 = `
CREATE TABLE IF NOT EXISTS curator_findings (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  finding_kind TEXT NOT NULL,
  severity TEXT NOT NULL,
  left_skill_id TEXT,
  right_skill_id TEXT,
  score REAL NOT NULL DEFAULT 0,
  evidence_json TEXT NOT NULL,
  proposal_json TEXT NOT NULL DEFAULT '{}',
  state TEXT NOT NULL DEFAULT 'open',
  created_at TEXT NOT NULL,
  FOREIGN KEY(run_id) REFERENCES curator_runs(id) ON DELETE CASCADE,
  FOREIGN KEY(left_skill_id) REFERENCES skills(id),
  FOREIGN KEY(right_skill_id) REFERENCES skills(id)
);

CREATE TABLE IF NOT EXISTS maintenance_schedules (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  task_kind TEXT NOT NULL,
  interval_seconds INTEGER NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 0,
  require_idle INTEGER NOT NULL DEFAULT 1,
  require_ac_power INTEGER NOT NULL DEFAULT 1,
  next_run_at TEXT NOT NULL,
  last_run_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS gc_runs (
  id TEXT PRIMARY KEY,
  state TEXT NOT NULL,
  mode TEXT NOT NULL,
  snapshot_revision TEXT NOT NULL,
  reachable_count INTEGER NOT NULL,
  unreachable_count INTEGER NOT NULL,
  reclaimable_bytes INTEGER NOT NULL,
  candidates_json TEXT NOT NULL,
  quarantine_path TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_curator_findings_run ON curator_findings(run_id, severity, score DESC);
CREATE INDEX IF NOT EXISTS idx_maintenance_due ON maintenance_schedules(enabled, next_run_at);
`

// V12 deliberately checks columns one-by-one. Some pre-release schema-11
// databases already carried these fields while others did not.
func migrateV12(ctx context.Context, tx *sql.Tx) error {
	columns := []struct {
		name       string
		definition string
	}{
		{"requested_profile", `TEXT NOT NULL DEFAULT ''`},
		{"eligible", `INTEGER NOT NULL DEFAULT 0`},
		{"requires_decision", `INTEGER NOT NULL DEFAULT 0`},
	}
	for _, column := range columns {
		found := false
		rows, err := tx.QueryContext(ctx, `PRAGMA table_info(model_qualification_runs)`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, kind string
			var defaultValue any
			if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
				rows.Close()
				return err
			}
			found = found || name == column.name
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if !found {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE model_qualification_runs ADD COLUMN `+column.name+` `+column.definition); err != nil {
				return err
			}
		}
	}
	return nil
}

const schemaV13 = `
ALTER TABLE agent_sessions ADD COLUMN active_turn_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_sessions ADD COLUMN lease_acquired_at TEXT;
ALTER TABLE agent_sessions ADD COLUMN contract_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agent_sessions ADD COLUMN contract_revision TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_sessions ADD COLUMN cache_epoch INTEGER NOT NULL DEFAULT 1;
ALTER TABLE agent_sessions ADD COLUMN qualification_run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE model_qualification_runs ADD COLUMN provider_revision TEXT NOT NULL DEFAULT '';
ALTER TABLE step_bindings ADD COLUMN session_contract_revision TEXT NOT NULL DEFAULT '';
ALTER TABLE step_bindings ADD COLUMN cache_epoch INTEGER NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS learning_trigger_outbox (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  milestone_id TEXT NOT NULL,
  job_id TEXT NOT NULL DEFAULT '',
  trigger_kind TEXT NOT NULL,
  digest_json TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  processed_at TEXT,
  UNIQUE(session_id, milestone_id, trigger_kind),
  FOREIGN KEY(session_id) REFERENCES agent_sessions(id)
);

CREATE INDEX IF NOT EXISTS idx_agent_sessions_active_turn ON agent_sessions(active_turn_id);
CREATE INDEX IF NOT EXISTS idx_learning_trigger_outbox_state ON learning_trigger_outbox(state, created_at);
`

const schemaV14 = `
CREATE TABLE IF NOT EXISTS skill_authority_policy (
  id TEXT PRIMARY KEY CHECK(id='local'),
  mode TEXT NOT NULL DEFAULT 'manual',
  auto_promote_agent_create INTEGER NOT NULL DEFAULT 0,
  auto_promote_agent_improve INTEGER NOT NULL DEFAULT 0,
  auto_archive_agent_skills INTEGER NOT NULL DEFAULT 0,
  allowed_scopes_json TEXT NOT NULL DEFAULT '["user"]',
  max_candidate_tokens INTEGER NOT NULL DEFAULT 4096,
  revision INTEGER NOT NULL DEFAULT 1,
  updated_by TEXT NOT NULL DEFAULT 'system',
  update_reason TEXT NOT NULL DEFAULT 'safe default',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS skill_authority_actions (
  id TEXT PRIMARY KEY,
  action_kind TEXT NOT NULL,
  candidate_id TEXT,
  skill_id TEXT,
  before_version_id TEXT NOT NULL DEFAULT '',
  after_version_id TEXT NOT NULL DEFAULT '',
  policy_revision INTEGER NOT NULL,
  actor TEXT NOT NULL,
  state TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  completed_at TEXT,
  rollback_candidate_id TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(candidate_id) REFERENCES skill_candidates(id),
  FOREIGN KEY(skill_id) REFERENCES skills(id)
);

INSERT OR IGNORE INTO skill_authority_policy(id,mode,allowed_scopes_json,max_candidate_tokens,revision,updated_by,update_reason,created_at,updated_at)
VALUES('local','manual','["user"]',4096,1,'system','safe default',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'));

CREATE INDEX IF NOT EXISTS idx_skill_authority_actions_created ON skill_authority_actions(created_at DESC);
`

const schemaV15 = `
ALTER TABLE skill_authority_actions ADD COLUMN archive_id TEXT NOT NULL DEFAULT '';
`

const schemaV16 = `
CREATE TABLE IF NOT EXISTS terminal_sessions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  shell TEXT NOT NULL,
  working_dir TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  output_tail TEXT NOT NULL DEFAULT '',
  cursor INTEGER NOT NULL DEFAULT 0,
  exit_code INTEGER,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT,
  FOREIGN KEY(project_id) REFERENCES projects(id)
);

CREATE TABLE IF NOT EXISTS browser_tabs (
  id TEXT PRIMARY KEY,
  project_id TEXT,
  url TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  allow_private INTEGER NOT NULL DEFAULT 0,
  text_snapshot TEXT NOT NULL DEFAULT '',
  links_json TEXT NOT NULL DEFAULT '[]',
  screenshot_artifact_id TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(project_id) REFERENCES projects(id)
);

CREATE TABLE IF NOT EXISTS agent_teams (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  provider_id TEXT NOT NULL,
  project_id TEXT NOT NULL DEFAULT '',
  context_profile TEXT NOT NULL,
  max_parallel INTEGER NOT NULL DEFAULT 2,
  state TEXT NOT NULL DEFAULT 'active',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_team_members (
  id TEXT PRIMARY KEY,
  team_id TEXT NOT NULL,
  name TEXT NOT NULL,
  role TEXT NOT NULL,
  instructions TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY(team_id) REFERENCES agent_teams(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_team_runs (
  id TEXT PRIMARY KEY,
  team_id TEXT NOT NULL,
  goal TEXT NOT NULL,
  state TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  completed_at TEXT,
  FOREIGN KEY(team_id) REFERENCES agent_teams(id)
);

CREATE TABLE IF NOT EXISTS agent_team_tasks (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  task_key TEXT NOT NULL,
  member_id TEXT NOT NULL,
  prompt TEXT NOT NULL,
  depends_on_json TEXT NOT NULL DEFAULT '[]',
  state TEXT NOT NULL,
  session_id TEXT NOT NULL DEFAULT '',
  output TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  UNIQUE(run_id, task_key),
  FOREIGN KEY(run_id) REFERENCES agent_team_runs(id),
  FOREIGN KEY(member_id) REFERENCES agent_team_members(id)
);

CREATE INDEX IF NOT EXISTS idx_terminal_sessions_project ON terminal_sessions(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_browser_tabs_updated ON browser_tabs(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_team_runs_team ON agent_team_runs(team_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_team_tasks_run ON agent_team_tasks(run_id, state);
`

// schemaV17 records how much of its output a model spends reasoning. Reasoning
// bills as completion tokens, so a profile's output reserve is not all answer,
// and on a small profile the answer can round down to nothing.
const schemaV17 = `
ALTER TABLE provider_profiles ADD COLUMN reasoning_ratio REAL NOT NULL DEFAULT 0;
ALTER TABLE provider_profiles ADD COLUMN reasoning_sample INTEGER NOT NULL DEFAULT 0;
`

// schemaV18 keeps every token prediction beside what the provider actually
// billed for the same request.
//
// The Phase 9 exit gate asks whether predicted input sits within ±10% of
// reported usage at p95. That could not be answered at all: Observe() folded
// each pair into an in-memory average and discarded it, and the only usage
// written anywhere was the whole turn's total against the last step's
// snapshot -- two different quantities, so of ninety snapshots exactly two
// were usable. A gate whose evidence is thrown away the moment it is produced
// is not a gate.
//
// One row per model step, so the volume is the volume of work already done.
const schemaV18 = `
CREATE TABLE IF NOT EXISTS token_observations (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  step_number INTEGER NOT NULL,
  provider_id TEXT NOT NULL,
  model TEXT NOT NULL,
  profile_name TEXT NOT NULL,
  context_snapshot_id TEXT NOT NULL,
  predicted_input INTEGER NOT NULL,
  actual_input INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(session_id, turn_id, step_number)
);
CREATE INDEX IF NOT EXISTS idx_token_observations_model ON token_observations(provider_id, model, created_at);
`

// schemaV19 keeps the token-scale calibration where it belongs: on the provider
// profile, beside the reasoning ratio that was already learned this way.
//
// It lived in a single in-memory float on one shared AdaptiveEstimator. Two
// things followed. It reset to 1.0 on every restart, so a server that had
// learned its model over-counts Thai by a quarter went back to over-counting
// on the next boot -- measured at 0.766 after eighteen turns, discarded. And it
// was one number for every provider and model at once, so concurrent sessions
// on different tokenizers pulled it in opposite directions and each corrupted
// the other's predictions.
const schemaV19 = `
ALTER TABLE provider_profiles ADD COLUMN token_multiplier REAL NOT NULL DEFAULT 1;
ALTER TABLE provider_profiles ADD COLUMN token_sample INTEGER NOT NULL DEFAULT 0;
`

// schemaV20 separates the prediction that can be compared against a bill from
// the budget that cannot.
//
// predicted_input includes the worst-case tool burst -- budget held back for a
// tool result that has not happened. Comparing it to reported prompt usage made
// the error band a function of context size: eighteen consecutive requests
// drifted from -51.7% to -27.9% as the fixed reserve was diluted by a growing
// prompt. Measured against the prompt alone the same requests are a flat -21.5%
// with a 2.0% spread. The first shape looks like an estimator that improves
// with use; the second is the truth, and it is a bias a calibration removes.
const schemaV20 = `
ALTER TABLE token_observations ADD COLUMN predicted_prompt INTEGER NOT NULL DEFAULT 0;
`

package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"hermetrix-harness/internal/blob"
	"hermetrix-harness/internal/identity"
)

type Store struct {
	DB    *sql.DB
	Blobs *blob.Store
	Root  string
	lock  *dataRootLock
}

func Open(ctx context.Context, root string) (*Store, error) {
	if root == "" {
		return nil, fmt.Errorf("data root is empty")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create data root: %w", err)
	}
	rootLock, err := acquireDataRootLock(filepath.Join(root, ".hermetrix.lock"))
	if err != nil {
		return nil, fmt.Errorf("lock data root: %w", err)
	}
	keepLock := false
	defer func() {
		if !keepLock {
			_ = rootLock.close()
		}
	}()
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
	if err := migrate(ctx, db, blobs); err != nil {
		db.Close()
		return nil, err
	}
	keepLock = true
	return &Store{DB: db, Blobs: blobs, Root: root, lock: rootLock}, nil
}

func (s *Store) Close() error {
	dbErr := s.DB.Close()
	lockErr := s.lock.close()
	if dbErr != nil {
		return dbErr
	}
	return lockErr
}

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

func (s *Store) LocalPrincipalID(ctx context.Context) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM local_principals WHERE kind='local' ORDER BY created_at LIMIT 1`).Scan(&id)
	return id, err
}

// OwnerPrincipalID returns the trusted principal bound by an ingress boundary,
// or the installation-local principal for internal/background work.
func (s *Store) OwnerPrincipalID(ctx context.Context) (string, error) {
	if id := identity.Principal(ctx); id != "" {
		var exists int
		if err := s.DB.QueryRowContext(ctx, `SELECT 1 FROM local_principals WHERE id=?`, id).Scan(&exists); err != nil {
			return "", fmt.Errorf("unknown principal: %w", err)
		}
		return id, nil
	}
	return s.LocalPrincipalID(ctx)
}

func migrate(ctx context.Context, db *sql.DB, blobs *blob.Store) error {
	// migrateV29TableSwap has to run before the shared transaction below opens
	// and commit on its own. See its comment for why.
	if err := migrateV29TableSwap(ctx, db); err != nil {
		return fmt.Errorf("apply schema v29 table swap: %w", err)
	}

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
	if version < 21 {
		if _, err := tx.ExecContext(ctx, schemaV21); err != nil {
			return fmt.Errorf("apply schema v21: %w", err)
		}
	}
	if version < 22 {
		if _, err := tx.ExecContext(ctx, schemaV22); err != nil {
			return fmt.Errorf("apply schema v22: %w", err)
		}
	}
	if version < 23 {
		if _, err := tx.ExecContext(ctx, schemaV23); err != nil {
			return fmt.Errorf("apply schema v23: %w", err)
		}
	}
	if version < 24 {
		if _, err := tx.ExecContext(ctx, schemaV24); err != nil {
			return fmt.Errorf("apply schema v24: %w", err)
		}
	}
	if version < 25 {
		if _, err := tx.ExecContext(ctx, schemaV25); err != nil {
			return fmt.Errorf("apply schema v25: %w", err)
		}
	}
	if version < 26 {
		if _, err := tx.ExecContext(ctx, schemaV26); err != nil {
			return fmt.Errorf("apply schema v26: %w", err)
		}
	}
	if version < 27 {
		if _, err := tx.ExecContext(ctx, schemaV27); err != nil {
			return fmt.Errorf("apply schema v27: %w", err)
		}
	}
	if version < 28 {
		if _, err := tx.ExecContext(ctx, schemaV28); err != nil {
			return fmt.Errorf("apply schema v28: %w", err)
		}
	}
	if version < 29 {
		if err := migrateV29(ctx, tx); err != nil {
			return fmt.Errorf("apply schema v29: %w", err)
		}
	}
	if version < 30 {
		if _, err := tx.ExecContext(ctx, schemaV30); err != nil {
			return fmt.Errorf("apply schema v30: %w", err)
		}
	}
	if version < 31 {
		if _, err := tx.ExecContext(ctx, schemaV31); err != nil {
			return fmt.Errorf("apply schema v31: %w", err)
		}
	}
	if version < 32 {
		if _, err := tx.ExecContext(ctx, schemaV32); err != nil {
			return fmt.Errorf("apply schema v32: %w", err)
		}
	}
	if version < 33 {
		if err := migrateV33(ctx, tx); err != nil {
			return fmt.Errorf("apply schema v33: %w", err)
		}
	}
	if version < 34 {
		if _, err := tx.ExecContext(ctx, schemaV34); err != nil {
			return fmt.Errorf("apply schema v34: %w", err)
		}
	}
	if version < 35 {
		if _, err := tx.ExecContext(ctx, schemaV35); err != nil {
			return fmt.Errorf("apply schema v35: %w", err)
		}
	}
	if version < 36 {
		if _, err := tx.ExecContext(ctx, schemaV36); err != nil {
			return fmt.Errorf("apply schema v36: %w", err)
		}
	}
	if version < 37 {
		if err := migrateV37(ctx, tx); err != nil {
			return fmt.Errorf("apply schema v37: %w", err)
		}
	}
	if version < 38 {
		if err := migrateV38(ctx, tx, blobs); err != nil {
			return fmt.Errorf("apply schema v38: %w", err)
		}
	}
	if version < 39 {
		if err := migrateV39(ctx, tx); err != nil {
			return fmt.Errorf("apply schema v39: %w", err)
		}
	}
	if version < 40 {
		if _, err := tx.ExecContext(ctx, schemaV40); err != nil {
			return fmt.Errorf("apply schema v40: %w", err)
		}
	}
	if version < 41 {
		if err := migrateV41(ctx, tx); err != nil {
			return fmt.Errorf("apply schema v41: %w", err)
		}
	}
	if version < 42 {
		if err := migrateV42(ctx, tx); err != nil {
			return fmt.Errorf("apply schema v42: %w", err)
		}
	}
	if version < 43 {
		if err := migrateV43(ctx, tx); err != nil {
			return fmt.Errorf("apply schema v43: %w", err)
		}
	}
	if version < 44 {
		if err := migrateV44(ctx, tx); err != nil {
			return fmt.Errorf("apply schema v44: %w", err)
		}
	}
	if version < 45 {
		if err := migrateV45(ctx, tx); err != nil {
			return fmt.Errorf("apply schema v45: %w", err)
		}
	}
	if version < 46 {
		if err := migrateV46(ctx, tx); err != nil {
			return fmt.Errorf("apply schema v46: %w", err)
		}
	}
	if version < 47 {
		if err := migrateV47(ctx, tx); err != nil {
			return fmt.Errorf("apply schema v47: %w", err)
		}
	}
	if version < 48 {
		if err := migrateV48(ctx, tx); err != nil {
			return fmt.Errorf("apply schema v48: %w", err)
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
const CurrentSchemaVersion = 48

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
CREATE TABLE IF NOT EXISTS candidate_behavioral_evals (
  id TEXT PRIMARY KEY,
  candidate_id TEXT NOT NULL,
  candidate_revision INTEGER NOT NULL,
  candidate_hash TEXT NOT NULL,
  base_version_id TEXT NOT NULL DEFAULT '',
  runner_revision TEXT NOT NULL,
  state TEXT NOT NULL,
  tasks INTEGER NOT NULL DEFAULT 0,
  baseline_passed INTEGER NOT NULL DEFAULT 0,
  candidate_passed INTEGER NOT NULL DEFAULT 0,
  regressions INTEGER NOT NULL DEFAULT 0,
  false_success_delta INTEGER NOT NULL DEFAULT 0,
  result_json TEXT NOT NULL DEFAULT '{}',
  error TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  completed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_behavioral_evals_candidate
  ON candidate_behavioral_evals(candidate_id, started_at);

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

-- Removed: terminal_sessions, browser_tabs, agent_teams, agent_team_members,
-- agent_team_runs, agent_team_tasks.
--
-- They were created here, listed in the backup manifest, and referenced nowhere
-- else: no reader, no writer, no route. A restore reported them as restored,
-- which reads as coverage of a terminal, a managed browser and an agent team
-- that do not exist. Schema is a claim the product makes on its own behalf and
-- these were not true. Recorded as O-42.
--
-- The features are planned -- workbench and PTY in Phase 11, parent-child task
-- graphs in Phase 12 -- and their tables belong in the commit that builds them,
-- with the columns those implementations need rather than the ones guessed
-- here. scripts/doc-truth.sh reports any table that drifts back into this state.

-- Vectors for semantic retrieval. Optional: a workspace with no embedder
-- configured never writes here and every reader falls back to lexical search.
--
-- Keyed by revision because vectors from different models are not comparable --
-- their coordinates mean different things -- so a model change invalidates
-- rather than silently mixing two geometries into one similarity score.
CREATE TABLE IF NOT EXISTS event_embeddings (
  event_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  revision TEXT NOT NULL,
  -- One row per chunk. A bi-encoder averages whatever it is given, so a fact
  -- inside a long message is diluted out of its own vector: measured with
  -- bge-m3, a fact scoring 0.567 alone scored 0.338 inside 5,600 runes of
  -- padding, below the 0.354 of padding with no fact in it at all. A document
  -- is scored by its most relevant chunk rather than by its average.
  chunk INTEGER NOT NULL DEFAULT 0,
  dimensions INTEGER NOT NULL,
  vector BLOB NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(event_id, revision, chunk)
);
CREATE INDEX IF NOT EXISTS idx_event_embeddings_session
  ON event_embeddings(session_id, revision);

-- Vectors for Skill catalog entries, so a goal in one language can reach a
-- summary written in another (R-14). Keyed by the hash of the embedded text
-- rather than by the Skill: an edited summary is a different text and earns a
-- different vector with nothing to invalidate, and a version that changed
-- nothing about how it describes itself is not embedded twice.
CREATE TABLE IF NOT EXISTS skill_embeddings (
  text_hash TEXT NOT NULL,
  revision TEXT NOT NULL,
  chunk INTEGER NOT NULL DEFAULT 0,
  dimensions INTEGER NOT NULL,
  vector BLOB NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(text_hash, revision, chunk)
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
  -- The agent may promote what it writes, and the user reviews it afterwards:
  -- every automatic promotion is recorded as a reversible authority action and
  -- shown in Skill Studio. Automatic archiving stays off, because deciding a
  -- Skill is dead is not the same kind of judgement as writing a new one.
  mode TEXT NOT NULL DEFAULT 'gated_automation',
  auto_promote_agent_create INTEGER NOT NULL DEFAULT 1,
  auto_promote_agent_improve INTEGER NOT NULL DEFAULT 1,
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

INSERT OR IGNORE INTO skill_authority_policy(id,mode,auto_promote_agent_create,auto_promote_agent_improve,allowed_scopes_json,max_candidate_tokens,revision,updated_by,update_reason,created_at,updated_at)
VALUES('local','gated_automation',1,1,'["user"]',4096,1,'system','agent may promote what it writes; every promotion is reversible and reviewable',strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now'));

CREATE INDEX IF NOT EXISTS idx_skill_authority_actions_created ON skill_authority_actions(created_at DESC);
`

const schemaV15 = `
ALTER TABLE skill_authority_actions ADD COLUMN archive_id TEXT NOT NULL DEFAULT '';
`

const schemaV16 = `






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

// schemaV21 learns how many tokens a character outside ASCII actually costs.
//
// The estimator charged one token per such character. No tokenizer does that;
// measured against a live gateway, Thai runs about 0.51. On a context that is
// ninety percent Thai the estimate came in thirty percent high, and a single
// overall multiplier could not correct it, because the right correction depends
// on how much of the text is non-ASCII and that changes every turn.
//
// Refitting twenty-three live requests with the rate as the only free parameter
// took p95 error from 41.5% to 2.8% and within-band from 1 of 23 to 23 of 23.
// The rate is learned per provider rather than written down, because 0.51 is
// one model and one language, and a constant fitted to a single observation is
// how the duplicate analyzer ended up unable to retrieve anything.
const schemaV21 = `
ALTER TABLE provider_profiles ADD COLUMN nonascii_rate REAL NOT NULL DEFAULT 1;
ALTER TABLE provider_profiles ADD COLUMN nonascii_sample INTEGER NOT NULL DEFAULT 0;
ALTER TABLE token_observations ADD COLUMN ascii_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE token_observations ADD COLUMN nonascii_chars INTEGER NOT NULL DEFAULT 0;
`

// schemaV22 stores the part of a request the estimator was never counting: the
// chat template.
//
// Every message is wrapped by the model's template, and a request carries a
// preamble besides. Both are billed and neither is context. Measured against a
// live gateway the relationship is exactly linear in message count: sending the
// same content split across 1, 3, 5, 9, 17 and 33 messages cost 9 more tokens
// each time, over a constant of 43. The probe, which uses empty messages
// instead, reads the same line as 7 and 45; both describe the corpus equally
// well (p95 7.42% and 7.54%).
//
// These are measured rather than learned. Two requests with known message
// counts determine both exactly, which is a better answer than any amount of
// regression on production traffic -- where message count, content size and
// language mix all grow together and no fit can separate them.
const schemaV22 = `
ALTER TABLE provider_profiles ADD COLUMN token_message_overhead INTEGER NOT NULL DEFAULT 0;
ALTER TABLE provider_profiles ADD COLUMN token_request_overhead INTEGER NOT NULL DEFAULT 0;
ALTER TABLE provider_profiles ADD COLUMN token_overhead_measured_at TEXT NOT NULL DEFAULT '';
`

// schemaV23 is the persistence half of the real interactive PTY runtime. The
// process itself is intentionally not recovered or replayed after restart;
// its bounded output tail and interrupted state remain inspectable.
const schemaV23 = `
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
CREATE INDEX IF NOT EXISTS idx_terminal_sessions_project ON terminal_sessions(project_id, created_at DESC);
`

// schemaV24 persists managed Chrome tabs and their bounded, untrusted DOM
// snapshots. Live DevTools connections are never recovered after restart.
const schemaV24 = `
CREATE TABLE IF NOT EXISTS browser_tabs (
  id TEXT PRIMARY KEY,
  project_id TEXT,
  url TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  allow_private INTEGER NOT NULL DEFAULT 0,
  text_snapshot TEXT NOT NULL DEFAULT '',
  links_json TEXT NOT NULL DEFAULT '[]',
  elements_json TEXT NOT NULL DEFAULT '[]',
  screenshot_artifact_id TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(project_id) REFERENCES projects(id)
);
CREATE INDEX IF NOT EXISTS idx_browser_tabs_updated ON browser_tabs(updated_at DESC);
`

// schemaV25 keeps reusable team definitions separate from ephemeral runs. A
// run freezes its task graph and never retries an interrupted model/tool effect.
const schemaV25 = `
CREATE TABLE IF NOT EXISTS agent_teams (
  id TEXT PRIMARY KEY,
  project_id TEXT,
  name TEXT NOT NULL,
  instructions TEXT NOT NULL,
  state TEXT NOT NULL,
  revision INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(project_id) REFERENCES projects(id)
);
CREATE TABLE IF NOT EXISTS agent_team_members (
  id TEXT PRIMARY KEY,
  team_id TEXT NOT NULL,
  name TEXT NOT NULL,
  role TEXT NOT NULL,
  instructions TEXT NOT NULL,
  is_lead INTEGER NOT NULL DEFAULT 0,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  FOREIGN KEY(team_id) REFERENCES agent_teams(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS agent_team_runs (
  id TEXT PRIMARY KEY,
  team_id TEXT NOT NULL,
  project_id TEXT,
  objective TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  context_profile TEXT NOT NULL,
  state TEXT NOT NULL,
  max_parallel INTEGER NOT NULL,
  actor TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  FOREIGN KEY(team_id) REFERENCES agent_teams(id),
  FOREIGN KEY(project_id) REFERENCES projects(id)
);
CREATE TABLE IF NOT EXISTS agent_team_tasks (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  member_id TEXT NOT NULL,
  title TEXT NOT NULL,
  prompt TEXT NOT NULL,
  depends_json TEXT NOT NULL DEFAULT '[]',
  state TEXT NOT NULL,
  session_id TEXT NOT NULL DEFAULT '',
  result TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  FOREIGN KEY(run_id) REFERENCES agent_team_runs(id) ON DELETE CASCADE,
  FOREIGN KEY(member_id) REFERENCES agent_team_members(id)
);
CREATE INDEX IF NOT EXISTS idx_agent_teams_project ON agent_teams(project_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_team_runs_created ON agent_team_runs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_team_tasks_run ON agent_team_tasks(run_id, created_at);
`

// schemaV26 snapshots team/member instructions into every run. Team roster
// edits retire member rows rather than deleting identities referenced by
// historical tasks, so a run never changes meaning after it starts.
const schemaV26 = `
ALTER TABLE agent_team_members ADD COLUMN state TEXT NOT NULL DEFAULT 'active';
ALTER TABLE agent_team_runs ADD COLUMN team_name TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_team_runs ADD COLUMN team_instructions TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_team_tasks ADD COLUMN member_name TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_team_tasks ADD COLUMN member_role TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_team_tasks ADD COLUMN member_instructions TEXT NOT NULL DEFAULT '';
UPDATE agent_team_runs
SET team_name=COALESCE((SELECT name FROM agent_teams WHERE id=agent_team_runs.team_id),''),
    team_instructions=COALESCE((SELECT instructions FROM agent_teams WHERE id=agent_team_runs.team_id),'')
WHERE team_name='';
UPDATE agent_team_tasks
SET member_name=COALESCE((SELECT name FROM agent_team_members WHERE id=agent_team_tasks.member_id),''),
    member_role=COALESCE((SELECT role FROM agent_team_members WHERE id=agent_team_tasks.member_id),''),
    member_instructions=COALESCE((SELECT instructions FROM agent_team_members WHERE id=agent_team_tasks.member_id),'')
WHERE member_name='';
CREATE INDEX IF NOT EXISTS idx_agent_team_members_active ON agent_team_members(team_id,state,sort_order);
`

// schemaV27 lets a team task pause at an exact persisted agent approval and
// resume the same turn after a human decision. The run also keeps the reviewed
// qualification reason needed by tasks scheduled after the pause.
// schemaV28 stores the two catalog kinds an MCP server publishes beside its
// tools. Until now discovery indexed tools and silently dropped resources and
// prompts, so a server whose whole point was the data it exposes appeared to
// have nothing in it.
const schemaV28 = `-- An MCP server publishes three kinds of thing, not one. Resources are data it
-- can hand over (a file, a row, a page); prompts are templates it wants used
-- verbatim. Both are stored beside the tools and replaced by the same atomic
-- discovery, so a server's catalog is always one consistent snapshot.
CREATE TABLE IF NOT EXISTS mcp_resources (
  server_id TEXT NOT NULL,
  uri TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  mime_type TEXT NOT NULL DEFAULT '',
  annotations_json TEXT NOT NULL DEFAULT '',
  revision TEXT NOT NULL,
  requires_approval INTEGER NOT NULL,
  discovered_at TEXT NOT NULL,
  PRIMARY KEY(server_id, uri),
  FOREIGN KEY(server_id) REFERENCES mcp_servers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_mcp_resources_server ON mcp_resources(server_id, uri);

CREATE TABLE IF NOT EXISTS mcp_prompts (
  server_id TEXT NOT NULL,
  name TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  arguments_json TEXT NOT NULL DEFAULT '[]',
  revision TEXT NOT NULL,
  requires_approval INTEGER NOT NULL,
  discovered_at TEXT NOT NULL,
  PRIMARY KEY(server_id, name),
  FOREIGN KEY(server_id) REFERENCES mcp_servers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_mcp_prompts_server ON mcp_prompts(server_id, name);
`

const schemaV27 = `
ALTER TABLE agent_team_runs ADD COLUMN qualification_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_team_tasks ADD COLUMN approval_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_team_tasks ADD COLUMN approval_summary TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_team_tasks ADD COLUMN approval_preview TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_team_tasks ADD COLUMN approval_effect TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_agent_team_tasks_approval ON agent_team_tasks(approval_id,state);
`

// schemaV30 is the durable task spine. It deliberately stores immutable
// requirement/plan revisions separately from mutable execution state: a
// resumed run can point at the exact contract it was executing instead of a
// summary that changed while it was offline.
const schemaV30 = `
CREATE TABLE IF NOT EXISTS durable_tasks (
  id TEXT PRIMARY KEY,
  project_id TEXT,
  title TEXT NOT NULL,
  objective TEXT NOT NULL,
  original_request TEXT NOT NULL,
  state TEXT NOT NULL,
  active_requirement_revision INTEGER NOT NULL DEFAULT 1,
  active_plan_revision INTEGER NOT NULL DEFAULT 0,
  revision INTEGER NOT NULL DEFAULT 1,
  pause_reason TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS task_requirement_revisions (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  revision INTEGER NOT NULL,
  constraints_json TEXT NOT NULL DEFAULT '[]',
  unknowns_json TEXT NOT NULL DEFAULT '[]',
  criteria_json TEXT NOT NULL DEFAULT '[]',
  supersedes_id TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(task_id, revision),
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS task_plan_revisions (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  revision INTEGER NOT NULL,
  requirement_revision INTEGER NOT NULL,
  reason TEXT NOT NULL,
  actor TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(task_id, revision),
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS task_steps (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  plan_revision INTEGER NOT NULL,
  step_key TEXT NOT NULL,
  title TEXT NOT NULL,
  instructions TEXT NOT NULL,
  dependencies_json TEXT NOT NULL DEFAULT '[]',
  checks_json TEXT NOT NULL DEFAULT '[]',
  effect_scope_json TEXT NOT NULL DEFAULT '[]',
  state TEXT NOT NULL,
  revision INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  UNIQUE(task_id, plan_revision, step_key),
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS task_checkpoints (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  plan_revision INTEGER NOT NULL,
  task_revision INTEGER NOT NULL,
  completed_steps_json TEXT NOT NULL,
  pending_steps_json TEXT NOT NULL,
  evidence_refs_json TEXT NOT NULL DEFAULT '[]',
  unresolved_effects_json TEXT NOT NULL DEFAULT '[]',
  next_action TEXT NOT NULL,
  resume_prerequisites_json TEXT NOT NULL DEFAULT '[]',
  reason TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS task_validations (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  step_id TEXT NOT NULL DEFAULT '',
  requirement_id TEXT NOT NULL DEFAULT '',
  check_id TEXT NOT NULL,
  subject_revision TEXT NOT NULL,
  status TEXT NOT NULL,
  expected TEXT NOT NULL DEFAULT '',
  actual TEXT NOT NULL DEFAULT '',
  evidence_refs_json TEXT NOT NULL DEFAULT '[]',
  severity TEXT NOT NULL DEFAULT '',
  confidence REAL NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_durable_tasks_state ON durable_tasks(state, updated_at);
CREATE INDEX IF NOT EXISTS idx_task_steps_active ON task_steps(task_id, plan_revision, state, sort_order);
CREATE INDEX IF NOT EXISTS idx_task_checkpoints_latest ON task_checkpoints(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_task_validations_subject ON task_validations(task_id, step_id, check_id, subject_revision, created_at DESC);
`

// schemaV31 records the boundary around every durable execution and external
// effect. An operation that crossed dispatch without an observed receipt is
// uncertain after restart, never silently replayed.
const schemaV31 = `
CREATE TABLE IF NOT EXISTS task_runs (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  plan_revision INTEGER NOT NULL,
  requirement_revision INTEGER NOT NULL,
  state TEXT NOT NULL,
  owner TEXT NOT NULL,
  lease_token TEXT NOT NULL,
  lease_expires_at TEXT NOT NULL,
  stop_reason TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS task_step_attempts (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  step_id TEXT NOT NULL,
  step_revision INTEGER NOT NULL,
  state TEXT NOT NULL,
  input_hash TEXT NOT NULL,
  output TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  completed_at TEXT,
  FOREIGN KEY(run_id) REFERENCES task_runs(id) ON DELETE CASCADE,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE,
  FOREIGN KEY(step_id) REFERENCES task_steps(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS task_effect_intents (
  id TEXT PRIMARY KEY,
  attempt_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  operation_id TEXT NOT NULL UNIQUE,
  action TEXT NOT NULL,
  target TEXT NOT NULL,
  authority TEXT NOT NULL,
  state TEXT NOT NULL,
  receipt_json TEXT NOT NULL DEFAULT '{}',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(attempt_id) REFERENCES task_step_attempts(id) ON DELETE CASCADE,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_task_one_live_run ON task_runs(task_id) WHERE state='running';
CREATE INDEX IF NOT EXISTS idx_task_runs_lease ON task_runs(state, lease_expires_at);
CREATE INDEX IF NOT EXISTS idx_task_attempts_run ON task_step_attempts(run_id, state, started_at);
CREATE INDEX IF NOT EXISTS idx_task_effects_reconcile ON task_effect_intents(task_id, state, updated_at);
`

// migrateV37 binds every durable effect to the exact run generation that
// planned it. Observation and reconciliation remain possible after expiry,
// while planning and dispatch can now prove a live owner transactionally.
func migrateV37(ctx context.Context, tx *sql.Tx) error {
	var orphaned int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_effect_intents e
		LEFT JOIN task_step_attempts a ON a.id=e.attempt_id
		LEFT JOIN task_runs r ON r.id=a.run_id
		WHERE a.id IS NULL OR r.id IS NULL`).Scan(&orphaned); err != nil {
		return fmt.Errorf("inspect effect authority backfill: %w", err)
	}
	if orphaned != 0 {
		return fmt.Errorf("cannot bind %d effect intents to a run", orphaned)
	}
	const migration = `
ALTER TABLE task_runs ADD COLUMN lease_generation INTEGER NOT NULL DEFAULT 1;

ALTER TABLE task_effect_intents RENAME TO task_effect_intents_v36;
CREATE TABLE task_effect_intents (
  id TEXT PRIMARY KEY,
  attempt_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  lease_generation INTEGER NOT NULL,
  operation_id TEXT NOT NULL UNIQUE,
  action TEXT NOT NULL,
  target TEXT NOT NULL,
  authority TEXT NOT NULL,
  state TEXT NOT NULL,
  receipt_json TEXT NOT NULL DEFAULT '{}',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(attempt_id) REFERENCES task_step_attempts(id) ON DELETE CASCADE,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE,
  FOREIGN KEY(run_id) REFERENCES task_runs(id) ON DELETE CASCADE
);
INSERT INTO task_effect_intents(
  id,attempt_id,task_id,run_id,lease_generation,operation_id,action,target,authority,state,receipt_json,error,created_at,updated_at
)
SELECT e.id,e.attempt_id,e.task_id,a.run_id,1,e.operation_id,e.action,e.target,e.authority,e.state,e.receipt_json,e.error,e.created_at,e.updated_at
FROM task_effect_intents_v36 e JOIN task_step_attempts a ON a.id=e.attempt_id;
DROP TABLE task_effect_intents_v36;
CREATE INDEX idx_task_effects_reconcile ON task_effect_intents(task_id,state,updated_at);
CREATE INDEX idx_task_effects_run_state ON task_effect_intents(run_id,state,updated_at);
`
	if _, err := tx.ExecContext(ctx, migration); err != nil {
		return err
	}
	var table, rowID, parent string
	var foreignKeyID int
	err := tx.QueryRowContext(ctx, `SELECT "table",rowid,parent,fkid FROM pragma_foreign_key_check LIMIT 1`).
		Scan(&table, &rowID, &parent, &foreignKeyID)
	if err == nil {
		return fmt.Errorf("foreign key check failed: table=%s row=%s parent=%s key=%d", table, rowID, parent, foreignKeyID)
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("run foreign key check: %w", err)
	}
	return nil
}

func migrateV38(ctx context.Context, tx *sql.Tx, blobs *blob.Store) error {
	const mutationSchema = `
CREATE TABLE file_mutation_intents (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL UNIQUE,
  project_id TEXT NOT NULL,
  path TEXT NOT NULL,
  actor TEXT NOT NULL,
  before_sha256 TEXT NOT NULL,
  after_sha256 TEXT NOT NULL,
  state TEXT NOT NULL,
  receipt_artifact_id TEXT,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
  FOREIGN KEY(receipt_artifact_id) REFERENCES artifacts(id) ON DELETE RESTRICT
);
CREATE INDEX idx_file_mutation_recovery ON file_mutation_intents(state,updated_at);
`
	if _, err := tx.ExecContext(ctx, mutationSchema); err != nil {
		return err
	}
	var proposalsTable, artifactsTable int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='task_code_proposals'`).Scan(&proposalsTable); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='artifacts'`).Scan(&artifactsTable); err != nil {
		return err
	}
	if proposalsTable == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE task_code_proposals ADD COLUMN rollback_artifact_id TEXT REFERENCES artifacts(id) ON DELETE RESTRICT;
		ALTER TABLE task_code_proposals ADD COLUMN verification_artifact_id TEXT REFERENCES artifacts(id) ON DELETE RESTRICT;`); err != nil {
		return err
	}
	// Some aged migration fixtures intentionally contain only the subsystem
	// tables that existed at their version. There is no artifact authority to
	// bind in such a fixture, so the new nullable columns are the complete and
	// honest migration result.
	if artifactsTable == 0 {
		return nil
	}
	// Bind every artifact that has one unambiguous proposal relationship. JSON
	// metadata remains descriptive; after this migration workflows use only the
	// relational columns below as authority evidence.
	if _, err := tx.ExecContext(ctx, `UPDATE task_code_proposals AS p SET rollback_artifact_id=(
		SELECT MIN(a.id) FROM artifacts a WHERE a.kind='code_proposal_rollback'
		AND json_extract(a.metadata_json,'$.proposal_id')=p.id)
		WHERE (SELECT COUNT(*) FROM artifacts a WHERE a.kind='code_proposal_rollback'
		AND json_extract(a.metadata_json,'$.proposal_id')=p.id)=1`); err != nil {
		return fmt.Errorf("backfill rollback artifact bindings: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE task_code_proposals AS p SET verification_artifact_id=(
		SELECT MIN(a.id) FROM artifacts a WHERE a.kind='code_verification_bundle'
		AND json_extract(a.metadata_json,'$.proposal_id')=p.id)
		WHERE (SELECT COUNT(*) FROM artifacts a WHERE a.kind='code_verification_bundle'
		AND json_extract(a.metadata_json,'$.proposal_id')=p.id)=1`); err != nil {
		return fmt.Errorf("backfill verification artifact bindings: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,project_id,state,
		CASE WHEN rollback_artifact_id IS NULL AND state IN ('applying','applied','awaiting_post_review') THEN 1 ELSE 0 END,
		CASE WHEN verification_artifact_id IS NULL AND state='awaiting_post_review' THEN 1 ELSE 0 END
		FROM task_code_proposals
		WHERE (rollback_artifact_id IS NULL AND state IN ('applying','applied','awaiting_post_review'))
		OR (verification_artifact_id IS NULL AND state='awaiting_post_review')`)
	if err != nil {
		return fmt.Errorf("find proposals requiring recovery: %w", err)
	}
	type recoveryProposal struct {
		id, projectID, state string
		missingRollback      int
		missingVerification  int
	}
	var recovery []recoveryProposal
	for rows.Next() {
		var item recoveryProposal
		if err = rows.Scan(&item.id, &item.projectID, &item.state, &item.missingRollback, &item.missingVerification); err != nil {
			rows.Close()
			return err
		}
		recovery = append(recovery, item)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, item := range recovery {
		now := time.Now().UTC()
		record := map[string]any{"proposal_id": item.id, "former_state": item.state,
			"missing_rollback_artifact":     item.missingRollback != 0,
			"missing_verification_artifact": item.missingVerification != 0,
			"migration":                     38, "recorded_at": now.Format(time.RFC3339Nano)}
		body, _ := json.Marshal(record)
		ref, putErr := blobs.Put(body)
		if putErr != nil {
			return fmt.Errorf("persist proposal recovery audit: %w", putErr)
		}
		artifactID := identity.New("artifact")
		metadata, _ := json.Marshal(map[string]any{"proposal_id": item.id, "former_state": item.state, "migration": 38})
		if _, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,project_id,name,kind,mime_type,blob_ref,byte_size,checksum,metadata_json,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, artifactID, item.projectID, item.id+".recovery.json", "proposal_recovery_audit",
			"application/vnd.hermetrix.proposal-recovery+json", ref, len(body), ref, string(metadata), now.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("record proposal recovery audit: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE task_code_proposals SET state='recovery_required',updated_at=? WHERE id=? AND state=?`,
			now.Format(time.RFC3339Nano), item.id, item.state); err != nil {
			return fmt.Errorf("hold proposal for recovery: %w", err)
		}
	}
	var table, rowID, parent string
	var foreignKeyID int
	err = tx.QueryRowContext(ctx, `SELECT "table",rowid,parent,fkid FROM pragma_foreign_key_check LIMIT 1`).
		Scan(&table, &rowID, &parent, &foreignKeyID)
	if err == nil {
		return fmt.Errorf("foreign key check failed: table=%s row=%s parent=%s key=%d", table, rowID, parent, foreignKeyID)
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("run foreign key check: %w", err)
	}
	return nil
}

// migrateV39 moves newly compiled context payloads to the content-addressed
// blob store while preserving historical inline snapshots. Checkpoints are
// derived state and can always be rebuilt from canonical agent_events.
func migrateV39(ctx context.Context, tx *sql.Tx) error {
	var snapshots int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='context_snapshots'`).Scan(&snapshots); err != nil {
		return err
	}
	if snapshots != 0 {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE context_snapshots ADD COLUMN compiled_blob_ref TEXT NOT NULL DEFAULT '';
			ALTER TABLE context_snapshots ADD COLUMN manifest_hash TEXT NOT NULL DEFAULT '';`); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS session_context_checkpoints (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		source_through_sequence INTEGER NOT NULL,
		contract_revision TEXT NOT NULL,
		compiler_revision TEXT NOT NULL,
		estimator_revision TEXT NOT NULL,
		checkpoint_blob_ref TEXT NOT NULL,
		manifest_hash TEXT NOT NULL,
		created_at TEXT NOT NULL,
		UNIQUE(session_id,source_through_sequence,contract_revision,compiler_revision,estimator_revision),
		FOREIGN KEY(session_id) REFERENCES agent_sessions(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_context_checkpoints_session ON session_context_checkpoints(session_id,source_through_sequence DESC);`)
	return err
}

const schemaV40 = `
CREATE TABLE IF NOT EXISTS inference_usage_ledger (
  id TEXT PRIMARY KEY,
  request_id TEXT NOT NULL UNIQUE,
  owner_kind TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  session_id TEXT,
  turn_id TEXT,
  usage_source TEXT NOT NULL,
  resource_key_hash TEXT NOT NULL,
  reserved_prompt_tokens INTEGER NOT NULL,
  reserved_output_tokens INTEGER NOT NULL,
  charged_prompt_tokens INTEGER NOT NULL DEFAULT 0,
  charged_output_tokens INTEGER NOT NULL DEFAULT 0,
  usage_quality TEXT NOT NULL,
  state TEXT NOT NULL,
  active_started_at TEXT,
  active_elapsed_ms INTEGER NOT NULL DEFAULT 0,
  deadline_at TEXT NOT NULL,
  terminal_reason TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_inference_usage_owner
  ON inference_usage_ledger(owner_kind,owner_id,created_at);

CREATE TABLE IF NOT EXISTS event_lexical_features (
  event_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  revision TEXT NOT NULL,
  features_blob BLOB NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(event_id) REFERENCES agent_events(id) ON DELETE CASCADE,
  FOREIGN KEY(session_id) REFERENCES agent_sessions(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_event_lexical_session ON event_lexical_features(session_id,revision);
`

func migrateV41(ctx context.Context, tx *sql.Tx) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	principalID := identity.New("principal")
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS local_principals (
		id TEXT PRIMARY KEY,
		kind TEXT NOT NULL UNIQUE CHECK(kind='local'),
		label TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS share_policy_audits (
		id TEXT PRIMARY KEY,
		principal_id TEXT NOT NULL,
		object_kind TEXT NOT NULL,
		object_id TEXT NOT NULL,
		from_visibility TEXT NOT NULL,
		to_visibility TEXT NOT NULL,
		expected_revision INTEGER NOT NULL,
		actor TEXT NOT NULL,
		reason TEXT NOT NULL,
		created_at TEXT NOT NULL,
		FOREIGN KEY(principal_id) REFERENCES local_principals(id) ON DELETE RESTRICT
	);`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO local_principals(id,kind,label,created_at)
		SELECT ?,'local','Local installation',? WHERE NOT EXISTS(SELECT 1 FROM local_principals WHERE kind='local')`, principalID, now); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT id FROM local_principals WHERE kind='local'`).Scan(&principalID); err != nil {
		return err
	}
	type ownershipTable struct {
		name  string
		extra []string
	}
	tables := []ownershipTable{
		{"projects", nil},
		{"agent_sessions", []string{"egress_policy TEXT NOT NULL DEFAULT 'local_only'"}},
		{"durable_tasks", []string{"egress_policy TEXT NOT NULL DEFAULT 'local_only'"}},
		{"artifacts", []string{"source_lineage_json TEXT NOT NULL DEFAULT '[]'"}},
		{"memories", nil},
		{"skills", nil},
		{"skill_candidates", nil},
	}
	for _, table := range tables {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table.name).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		statements := []string{
			"ALTER TABLE " + table.name + " ADD COLUMN owner_principal_id TEXT NOT NULL DEFAULT ''",
			"ALTER TABLE " + table.name + " ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private'",
			"ALTER TABLE " + table.name + " ADD COLUMN export_policy TEXT NOT NULL DEFAULT 'deny'",
			"ALTER TABLE " + table.name + " ADD COLUMN sharing_revision INTEGER NOT NULL DEFAULT 1",
		}
		for _, column := range table.extra {
			statements = append(statements, "ALTER TABLE "+table.name+" ADD COLUMN "+column)
		}
		for _, statement := range statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("add ownership to %s: %w", table.name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE "+table.name+" SET owner_principal_id=? WHERE owner_principal_id=''", principalID); err != nil {
			return err
		}
		triggerPrefix := "ownership_" + table.name
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`CREATE TRIGGER %s_insert AFTER INSERT ON %s
			WHEN NEW.owner_principal_id='' BEGIN
			UPDATE %s SET owner_principal_id=(SELECT id FROM local_principals WHERE kind='local') WHERE rowid=NEW.rowid;
			END;
			CREATE TRIGGER %s_policy_insert BEFORE INSERT ON %s
			WHEN NEW.visibility NOT IN ('private','project_shared') OR NEW.export_policy NOT IN ('deny','explicit_selection')
			BEGIN SELECT RAISE(ABORT,'invalid visibility or export policy'); END;
			CREATE TRIGGER %s_policy_update BEFORE UPDATE OF visibility,export_policy ON %s
			WHEN NEW.visibility NOT IN ('private','project_shared') OR NEW.export_policy NOT IN ('deny','explicit_selection')
			BEGIN SELECT RAISE(ABORT,'invalid visibility or export policy'); END;`,
			triggerPrefix, table.name, table.name, triggerPrefix, table.name, triggerPrefix, table.name)); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_share_policy_object ON share_policy_audits(object_kind,object_id,created_at)`)
	return err
}

func migrateV42(ctx context.Context, tx *sql.Tx) error {
	const schema = `
CREATE TABLE runtime_fingerprints (
  id TEXT PRIMARY KEY,
  digest TEXT NOT NULL UNIQUE,
  runtime_kind TEXT NOT NULL,
  endpoint_identity TEXT NOT NULL,
  build_revision TEXT NOT NULL DEFAULT '',
  model_id TEXT NOT NULL,
  model_checksums_json TEXT NOT NULL DEFAULT '[]',
  projector_checksum TEXT NOT NULL DEFAULT '',
  chat_template_hash TEXT NOT NULL DEFAULT '',
  tokenizer_revision TEXT NOT NULL DEFAULT '',
  kv_settings_json TEXT NOT NULL DEFAULT '{}',
  context_capacity INTEGER NOT NULL DEFAULT 0,
  device_mapping_json TEXT NOT NULL DEFAULT '[]',
  config_digest TEXT NOT NULL,
  evidence_quality TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE inference_presets (
  id TEXT NOT NULL,
  revision INTEGER NOT NULL,
  role TEXT NOT NULL,
  context_target INTEGER NOT NULL,
  context_max INTEGER NOT NULL,
  reasoning_mode TEXT NOT NULL,
  reasoning_token_cap INTEGER NOT NULL,
  answer_reserve INTEGER NOT NULL,
  generation_cap INTEGER NOT NULL,
  temperature REAL NOT NULL,
  schema_version INTEGER NOT NULL,
  content_digest TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  PRIMARY KEY(id,revision)
);
CREATE TRIGGER inference_presets_immutable_update BEFORE UPDATE ON inference_presets
BEGIN SELECT RAISE(ABORT,'inference presets are immutable'); END;
CREATE TRIGGER inference_presets_immutable_delete BEFORE DELETE ON inference_presets
BEGIN SELECT RAISE(ABORT,'inference presets are immutable'); END;
`
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	alters := map[string][]string{
		"provider_profiles":        {"resource_group TEXT NOT NULL DEFAULT ''", "runtime_fingerprint_id TEXT NOT NULL DEFAULT ''"},
		"model_qualification_runs": {"runtime_fingerprint_id TEXT REFERENCES runtime_fingerprints(id) ON DELETE RESTRICT", "modalities_json TEXT NOT NULL DEFAULT '[\"text\"]'", "controls_json TEXT NOT NULL DEFAULT '{}'"},
		"step_bindings":            {"preset_id TEXT NOT NULL DEFAULT 'legacy-chat'", "preset_revision INTEGER NOT NULL DEFAULT 0", "runtime_fingerprint_id TEXT NOT NULL DEFAULT ''", "effective_parameter_digest TEXT NOT NULL DEFAULT ''"},
		"task_step_attempts":       {"inference_role TEXT NOT NULL DEFAULT 'worker'", "preset_id TEXT NOT NULL DEFAULT 'legacy-worker'", "preset_revision INTEGER NOT NULL DEFAULT 0", "runtime_fingerprint_id TEXT NOT NULL DEFAULT ''"},
		"inference_usage_ledger":   {"preset_id TEXT NOT NULL DEFAULT ''", "preset_revision INTEGER NOT NULL DEFAULT 0", "runtime_fingerprint_id TEXT NOT NULL DEFAULT ''", "effective_parameter_digest TEXT NOT NULL DEFAULT ''"},
	}
	for table, columns := range alters {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		for _, column := range columns {
			if _, err := tx.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column); err != nil {
				return err
			}
		}
	}
	type preset struct {
		ID                string  `json:"id"`
		Revision          int     `json:"revision"`
		Role              string  `json:"role"`
		ContextTarget     int     `json:"context_target"`
		ContextMax        int     `json:"context_max"`
		ReasoningMode     string  `json:"reasoning_mode"`
		ReasoningTokenCap int     `json:"reasoning_token_cap"`
		AnswerReserve     int     `json:"answer_reserve"`
		GenerationCap     int     `json:"generation_cap"`
		Temperature       float64 `json:"temperature"`
		SchemaVersion     int     `json:"schema_version"`
	}
	presets := []preset{
		{"planner", 1, "planner", 40000, 60000, "bounded", 3072, 2048, 5120, 0.2, 1},
		{"worker", 1, "worker", 16000, 30000, "disabled", 0, 1500, 1500, 0.1, 1},
		{"worker-debug", 1, "worker-debug", 20000, 30000, "bounded", 1024, 1500, 2524, 0.1, 1},
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range presets {
		encoded, _ := json.Marshal(item)
		sum := sha256.Sum256(encoded)
		digest := hex.EncodeToString(sum[:])
		if _, err := tx.ExecContext(ctx, `INSERT INTO inference_presets(id,revision,role,context_target,context_max,reasoning_mode,
			reasoning_token_cap,answer_reserve,generation_cap,temperature,schema_version,content_digest,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.Revision, item.Role, item.ContextTarget, item.ContextMax, item.ReasoningMode,
			item.ReasoningTokenCap, item.AnswerReserve, item.GenerationCap, item.Temperature, item.SchemaVersion, digest, now); err != nil {
			return err
		}
	}
	return nil
}

func migrateV43(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE agent_event_parts (
  id TEXT PRIMARY KEY,
  event_id TEXT NOT NULL,
  ordinal INTEGER NOT NULL CHECK(ordinal >= 0 AND ordinal < 32),
  kind TEXT NOT NULL CHECK(kind IN ('text','image')),
  text_content TEXT,
  artifact_id TEXT,
  metadata_json TEXT NOT NULL DEFAULT '{}' CHECK(length(metadata_json) <= 16384),
  created_at TEXT NOT NULL,
  UNIQUE(event_id,ordinal),
  CHECK((kind='text' AND text_content IS NOT NULL AND length(text_content)>0 AND artifact_id IS NULL) OR
        (kind='image' AND text_content IS NULL AND artifact_id IS NOT NULL)),
  FOREIGN KEY(event_id) REFERENCES agent_events(id) ON DELETE CASCADE,
  FOREIGN KEY(artifact_id) REFERENCES artifacts(id) ON DELETE RESTRICT
);
CREATE INDEX idx_agent_event_parts_event ON agent_event_parts(event_id,ordinal);
CREATE INDEX idx_agent_event_parts_artifact ON agent_event_parts(artifact_id) WHERE artifact_id IS NOT NULL;
CREATE TABLE artifact_derivations (
  id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL,
  source_artifact_id TEXT NOT NULL,
  result_artifact_id TEXT NOT NULL,
  relation_kind TEXT NOT NULL,
  source_hash TEXT NOT NULL,
  result_hash TEXT NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE(source_artifact_id,result_artifact_id,relation_kind),
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id) ON DELETE RESTRICT,
  FOREIGN KEY(source_artifact_id) REFERENCES artifacts(id) ON DELETE RESTRICT,
  FOREIGN KEY(result_artifact_id) REFERENCES artifacts(id) ON DELETE RESTRICT
);
CREATE INDEX idx_artifact_derivations_source ON artifact_derivations(source_artifact_id,created_at);
CREATE INDEX idx_artifact_derivations_result ON artifact_derivations(result_artifact_id,created_at);
`)
	return err
}

func migrateV44(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE media_jobs (
  id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL,
  source_artifact_id TEXT NOT NULL,
  source_hash TEXT NOT NULL,
  processor_kind TEXT NOT NULL CHECK(processor_kind IN ('image_inspect','audio_transcribe','video_extract')),
  processor_revision TEXT NOT NULL,
  model_revision TEXT NOT NULL DEFAULT '',
  settings_digest TEXT NOT NULL,
  operation_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('queued','running','completed','failed','cancelled','interrupted')),
  progress REAL NOT NULL DEFAULT 0 CHECK(progress >= 0 AND progress <= 1),
  attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count >= 0),
  result_artifact_ids_json TEXT NOT NULL DEFAULT '[]',
  error_code TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK(cancel_requested IN (0,1)),
  created_at TEXT NOT NULL,
  started_at TEXT,
  updated_at TEXT NOT NULL,
  completed_at TEXT,
  UNIQUE(owner_principal_id,operation_id,idempotency_key),
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id) ON DELETE RESTRICT,
  FOREIGN KEY(source_artifact_id) REFERENCES artifacts(id) ON DELETE RESTRICT
);
CREATE INDEX idx_media_jobs_owner_state ON media_jobs(owner_principal_id,state,created_at);
CREATE INDEX idx_media_jobs_source ON media_jobs(source_artifact_id,created_at);

CREATE TABLE task_planning_decisions (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  task_revision INTEGER NOT NULL,
  classifier_revision INTEGER NOT NULL,
  decision TEXT NOT NULL CHECK(decision IN ('existing_plan','deterministic_plan','planner_required')),
  evidence_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(task_id,task_revision,classifier_revision),
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE
);
CREATE TABLE task_step_failures (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  step_id TEXT NOT NULL,
  attempt_id TEXT NOT NULL,
  normalized_signature TEXT NOT NULL,
  failure_kind TEXT NOT NULL CHECK(failure_kind IN ('execution','test','transport','configuration','uncertain_effect')),
  diff_artifact_id TEXT,
  evidence_refs_json TEXT NOT NULL DEFAULT '[]',
  prior_change_refs_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  UNIQUE(attempt_id),
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE,
  FOREIGN KEY(step_id) REFERENCES task_steps(id) ON DELETE CASCADE,
  FOREIGN KEY(attempt_id) REFERENCES task_step_attempts(id) ON DELETE CASCADE,
  FOREIGN KEY(diff_artifact_id) REFERENCES artifacts(id) ON DELETE RESTRICT
);
CREATE INDEX idx_task_step_failures_signature ON task_step_failures(step_id,normalized_signature,created_at);
CREATE TABLE task_step_escalations (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  step_id TEXT NOT NULL,
  normalized_signature TEXT NOT NULL,
  ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 1 AND 2),
  state TEXT NOT NULL CHECK(state IN ('awaiting_plan','resolved','limit_exceeded')),
  evidence_json TEXT NOT NULL,
  planner_run_id TEXT,
  plan_revision INTEGER,
  created_at TEXT NOT NULL,
  resolved_at TEXT,
  UNIQUE(step_id,ordinal),
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE,
  FOREIGN KEY(step_id) REFERENCES task_steps(id) ON DELETE CASCADE,
  FOREIGN KEY(planner_run_id) REFERENCES task_planner_runs(id) ON DELETE SET NULL
);
CREATE INDEX idx_task_step_escalations_state ON task_step_escalations(task_id,state,created_at);
`)
	return err
}

func migrateV45(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE share_previews (
  id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  actor TEXT NOT NULL,
  manifest_blob_ref TEXT NOT NULL,
  manifest_digest TEXT NOT NULL,
  source_revision_digest TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('ready','expired','consumed','invalidated')),
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id) ON DELETE RESTRICT,
  FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);
CREATE INDEX idx_share_previews_owner_project ON share_previews(owner_principal_id,project_id,state,expires_at);
CREATE TABLE share_export_jobs (
  id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  preview_id TEXT NOT NULL,
  manifest_digest TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('queued','running','completed','failed','cancelled','interrupted')),
  package_blob_ref TEXT,
  package_checksum TEXT,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  completed_at TEXT,
  UNIQUE(owner_principal_id,idempotency_key),
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id) ON DELETE RESTRICT,
  FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
  FOREIGN KEY(preview_id) REFERENCES share_previews(id) ON DELETE RESTRICT
);
CREATE INDEX idx_share_exports_owner_state ON share_export_jobs(owner_principal_id,state,created_at);
CREATE TABLE share_import_staging (
  id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL,
  actor TEXT NOT NULL,
  package_blob_ref TEXT NOT NULL,
  package_checksum TEXT NOT NULL,
  manifest_digest TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('awaiting_apply','applying','completed','failed','expired')),
  summary_json TEXT NOT NULL,
  destination_project_id TEXT,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  completed_at TEXT,
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id) ON DELETE RESTRICT,
  FOREIGN KEY(destination_project_id) REFERENCES projects(id) ON DELETE SET NULL
);
CREATE INDEX idx_share_imports_owner_state ON share_import_staging(owner_principal_id,state,created_at);
`)
	return err
}

func migrateV46(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE workspace_migration_jobs (
  id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('export','import_preview','import_apply')),
  state TEXT NOT NULL CHECK(state IN ('running','awaiting_apply','completed','failed','interrupted')),
  format_version INTEGER NOT NULL,
  package_blob_ref TEXT,
  package_checksum TEXT,
  source_principal_id TEXT,
  destination_principal_id TEXT,
  summary_json TEXT NOT NULL DEFAULT '{}',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  completed_at TEXT,
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id) ON DELETE RESTRICT
);
CREATE INDEX idx_workspace_migrations_owner_state ON workspace_migration_jobs(owner_principal_id,state,created_at);
CREATE TABLE workspace_migration_maps (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL,
  object_kind TEXT NOT NULL,
  source_id TEXT NOT NULL,
  destination_id TEXT NOT NULL,
  source_root TEXT NOT NULL DEFAULT '',
  destination_root TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(job_id,object_kind,source_id),
  FOREIGN KEY(job_id) REFERENCES workspace_migration_jobs(id) ON DELETE CASCADE
);
`)
	return err
}

func migrateV47(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS task_decision_shadow_runs (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  task_revision INTEGER NOT NULL,
  provider_id TEXT NOT NULL,
  provider_revision TEXT NOT NULL,
  model_name TEXT NOT NULL,
  state_json TEXT NOT NULL,
  candidates_json TEXT NOT NULL,
  baseline_json TEXT NOT NULL,
  model_json TEXT,
  agreement INTEGER NOT NULL CHECK(agreement IN (0,1)),
  valid INTEGER NOT NULL CHECK(valid IN (0,1)),
  latency_ms INTEGER NOT NULL,
  prompt_tokens INTEGER NOT NULL,
  completion_tokens INTEGER NOT NULL,
  model_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_task_decision_shadow_task ON task_decision_shadow_runs(task_id,created_at DESC);
CREATE INDEX IF NOT EXISTS idx_task_decision_shadow_provider ON task_decision_shadow_runs(provider_id,created_at DESC);
`)
	return err
}

func migrateV48(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS decision_benchmark_runs (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL,
  provider_revision TEXT NOT NULL,
  model_name TEXT NOT NULL,
  decision_revision TEXT NOT NULL,
  results_json TEXT NOT NULL,
  total_cases INTEGER NOT NULL,
  valid_cases INTEGER NOT NULL,
  correct_cases INTEGER NOT NULL,
  accuracy REAL NOT NULL,
  invalid_rate REAL NOT NULL,
  average_latency_ms REAL NOT NULL,
  passed INTEGER NOT NULL CHECK(passed IN (0,1)),
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_decision_benchmark_provider ON decision_benchmark_runs(provider_id,created_at DESC);
CREATE TABLE IF NOT EXISTS decision_admission_policies (
  provider_id TEXT PRIMARY KEY,
  provider_revision TEXT NOT NULL,
  benchmark_run_id TEXT NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  allowed_risks_json TEXT NOT NULL DEFAULT '["read"]',
  actor TEXT NOT NULL,
  reason TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(benchmark_run_id) REFERENCES decision_benchmark_runs(id) ON DELETE RESTRICT
);
`)
	return err
}

// schemaV32 makes code proposals and their review decisions durable. Model
// output cannot become an applied change merely because an artifact exists.
const schemaV32 = `
CREATE TABLE IF NOT EXISTS task_code_proposals (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  step_id TEXT NOT NULL,
  attempt_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  packet_hash TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  provider_revision TEXT NOT NULL,
  artifact_id TEXT NOT NULL,
  result_hash TEXT NOT NULL,
  state TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE,
  FOREIGN KEY(step_id) REFERENCES task_steps(id) ON DELETE CASCADE,
  FOREIGN KEY(attempt_id) REFERENCES task_step_attempts(id) ON DELETE CASCADE,
  FOREIGN KEY(artifact_id) REFERENCES artifacts(id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS task_code_reviews (
  id TEXT PRIMARY KEY,
  proposal_id TEXT NOT NULL,
  reviewer TEXT NOT NULL,
  verdict TEXT NOT NULL,
  rationale TEXT NOT NULL,
  findings_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  FOREIGN KEY(proposal_id) REFERENCES task_code_proposals(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_code_proposals_task ON task_code_proposals(task_id,state,created_at);
CREATE INDEX IF NOT EXISTS idx_task_code_reviews_proposal ON task_code_reviews(proposal_id,created_at);
`

// schemaV33 makes managed command result lookup deterministic after a crash.
// The operation id is persisted in the job payload before the process starts;
// one effect can therefore be reconciled without replaying the command.
const schemaV33 = `
CREATE UNIQUE INDEX IF NOT EXISTS idx_background_jobs_operation
ON background_jobs(json_extract(payload_json,'$.operation_id'))
WHERE json_extract(payload_json,'$.operation_id') IS NOT NULL;
`

func migrateV33(ctx context.Context, tx *sql.Tx) error {
	var tables int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='background_jobs'`).Scan(&tables); err != nil {
		return err
	}
	if tables == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, schemaV33)
	return err
}

const schemaV34 = `
CREATE TABLE IF NOT EXISTS task_planner_runs (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  requirement_revision INTEGER NOT NULL,
  expected_task_revision INTEGER NOT NULL,
  provider_id TEXT NOT NULL,
  provider_revision TEXT NOT NULL,
  input_hash TEXT NOT NULL,
  state TEXT NOT NULL,
  artifact_id TEXT,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE,
  FOREIGN KEY(artifact_id) REFERENCES artifacts(id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS idx_task_planner_runs_task ON task_planner_runs(task_id,created_at DESC);
`

// schemaV35 persists the exact authoritative packet beside a step attempt.
// The hash alone proves identity but cannot resume a proposal after the UI or
// coordinator restarts; storing the bounded packet makes that handoff durable
// without reconstructing authority from newer task state.
const schemaV35 = `
ALTER TABLE task_step_attempts ADD COLUMN packet_json TEXT NOT NULL DEFAULT '';
`

// schemaV36 keeps the planner's acceptance-criterion mapping on the immutable
// step revision. Verification can then bind observed command evidence to the
// criterion the plan named instead of letting a UI guess after execution.
const schemaV36 = `
ALTER TABLE task_steps ADD COLUMN requirement_ids_json TEXT NOT NULL DEFAULT '[]';
`

// migrateV29TableSwap rebuilds the projects table when it already exists in
// its pre-v29 shape, and does nothing otherwise. It has to run before
// migrateV29 -- and outside any surrounding transaction -- because of a
// SQLite limitation this project's own test suite never exercised.
//
// Five tables (agent_sessions, artifacts, terminal_sessions, browser_tabs,
// agent_teams, agent_team_runs) carry FOREIGN KEY(project_id) REFERENCES
// projects(id). Rebuilding projects means dropping it and recreating it under
// the same name, and with foreign_keys=ON -- the pragma this store always
// sets -- SQLite refuses that drop while other rows still reference it.
//
// The obvious fix, PRAGMA defer_foreign_keys=ON, postpones the *check* but
// not the underlying bookkeeping: SQLite tracks a deferred violation against
// the dropped table's b-tree object specifically, and creating a new object
// under the old name does not retroactively satisfy it. Verified directly
// against a real, aged database with real rows in five referencing tables and
// zero actual constraint violations (`PRAGMA foreign_key_check` confirms this
// both before the rebuild and after, run standalone): the deferred approach
// still fails at COMMIT. This is not theoretical -- it is what a real user's
// upgrade hit first, because every migration test before this one started
// from a bare schema with no FK-referencing rows to trip it.
//
// SQLite's own documented answer for rebuilding a referenced table is to
// disable enforcement outright, and PRAGMA foreign_keys can only be toggled
// outside an open transaction -- inside one, it is a documented no-op. That
// is why this cannot be folded into the shared migration transaction the rest
// of this file uses: it needs its own transaction, bracketed by the pragma
// toggle in autocommit mode on either side.
//
// Idempotent and safe to re-run: if the process dies after this commits but
// before the shared transaction records user_version, the next startup finds
// projects already in its new shape (via the pinned column check below) and
// skips straight past this step.
func migrateV29TableSwap(ctx context.Context, db *sql.DB) (err error) {
	var name string
	lookupErr := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='projects'`).Scan(&name)
	if lookupErr != nil {
		if lookupErr == sql.ErrNoRows {
			return nil // nothing to rebuild; migrateV29 creates it fresh
		}
		return lookupErr
	}
	var alreadyMigrated int
	if scanErr := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('projects') WHERE name='pinned'`).Scan(&alreadyMigrated); scanErr != nil {
		return scanErr
	}
	if alreadyMigrated > 0 {
		return nil // already in the v29 shape -- a prior run got here first
	}

	if _, execErr := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); execErr != nil {
		return fmt.Errorf("disable foreign keys for table swap: %w", execErr)
	}
	// However this function returns, enforcement must be back on before
	// anything else touches this connection -- the shared migration
	// transaction that follows relies on it, and so does every other query
	// this store ever runs. A failure here cannot be logged and swallowed:
	// running the rest of this process with foreign_keys silently OFF is
	// exactly the class of bug the store's own doc-truth check (the
	// ignored-exec-error claim) exists to refuse, so it is folded into this
	// function's named return instead -- it wins over a nil success, and rides
	// alongside a real one, rather than disappearing into `_, _ =`.
	defer func() {
		if _, reenableErr := db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); reenableErr != nil {
			if err != nil {
				err = fmt.Errorf("%w (also failed to re-enable foreign keys: %v)", err, reenableErr)
			} else {
				err = fmt.Errorf("re-enable foreign keys after table swap: %w", reenableErr)
			}
		}
	}()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin table swap: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schemaV29Rebuild); err != nil {
		return err
	}
	// Defence in depth: foreign_keys=OFF means the rebuild cannot fail loudly
	// on its own, so ask directly whether it left anything inconsistent rather
	// than assuming the absence of an error means the absence of a problem.
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("check table swap: %w", err)
	}
	var violation string
	hasViolation := rows.Next()
	if hasViolation {
		_ = rows.Scan(&violation)
	}
	if closeErr := rows.Close(); closeErr != nil {
		return closeErr
	}
	if hasViolation {
		return fmt.Errorf("table swap left a foreign key violation: %s", violation)
	}
	return tx.Commit()
}

// schemaV29 makes a project a bounded scope rather than a code folder. A
// project without code is ordinary -- planning a trip and planning a refactor
// have the same shape -- so root_path becomes optional. SQLite cannot drop a
// column's UNIQUE constraint, so the table is rebuilt and the uniqueness moves
// to a partial index that only covers roots that actually exist.
//
// migrateV29 branches on whether projects already exists rather than always
// rebuilding it. A database that never ran the schemaV10 step that created
// projects (a hand-built fixture pinned to a later version, for instance)
// has nothing to copy from, and rebuilding a table that was never there
// would fail on the SELECT instead of just creating the new shape directly.
func migrateV29(ctx context.Context, tx *sql.Tx) error {
	// migrateV29TableSwap, above, already rebuilt projects if it existed
	// BEFORE migrate() was called -- the case where real rows in the five
	// FK-referencing tables might already be committed on disk, which is why
	// that rebuild has to run outside this transaction with foreign_keys=OFF.
	//
	// This function still has to handle the other way projects can reach this
	// point in the old shape: a brand-new database, migrated through every
	// version in one call to migrate(). There, an earlier version's schema
	// (long before v29) creates projects for the first time inside THIS SAME
	// open transaction, so nothing outside it can possibly reference it yet --
	// the plain rebuild below is safe precisely because there is no
	// previously-committed data to protect.
	var name string
	err := tx.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='projects'`).Scan(&name)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == sql.ErrNoRows {
		if _, execErr := tx.ExecContext(ctx, schemaV29Create); execErr != nil {
			return execErr
		}
	} else {
		var alreadyMigrated int
		if scanErr := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_table_info('projects') WHERE name='pinned'`).Scan(&alreadyMigrated); scanErr != nil {
			return scanErr
		}
		if alreadyMigrated == 0 {
			if _, execErr := tx.ExecContext(ctx, schemaV29Rebuild); execErr != nil {
				return execErr
			}
		}
	}

	// A project became mandatory for every session at this version, so a
	// session with none is a backlog left over from before that rule existed,
	// not an event that can keep happening. This is therefore a one-time
	// backfill and belongs inside the version gate like the rest of v29: once
	// this has run, nothing in the schema lets project_id go missing again, so
	// an orphan appearing afterward would be a bug worth seeing rather than a
	// steady-state condition worth a full table scan on every open to heal.
	// The table-existence check mirrors the one above projects -- a fixture
	// pinned straight to a schema version without ever running schemaV3 has no
	// agent_sessions to sweep, and the INSERT would fail on the missing table
	// rather than finding nothing to do.
	err = tx.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='agent_sessions'`).Scan(&name)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == sql.ErrNoRows {
		return nil
	}
	_, execErr := tx.ExecContext(ctx, schemaV29Inbox)
	return execErr
}

const schemaV29Create = `
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  root_path TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'active',
  pinned INTEGER NOT NULL DEFAULT 0,
  last_opened_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_root ON projects(root_path) WHERE root_path <> '';
`

// schemaV29Rebuild is run by migrateV29TableSwap, outside any transaction and
// with PRAGMA foreign_keys=OFF -- see that function for why DROP TABLE
// projects cannot happen any other way when five other tables reference it.
const schemaV29Rebuild = `
CREATE TABLE projects_v29 (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  root_path TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'active',
  pinned INTEGER NOT NULL DEFAULT 0,
  last_opened_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
INSERT INTO projects_v29(id,name,root_path,state,created_at,updated_at)
  SELECT id,name,root_path,state,created_at,updated_at FROM projects;
DROP TABLE projects;
ALTER TABLE projects_v29 RENAME TO projects;
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_root ON projects(root_path) WHERE root_path <> '';
`

// schemaV29Inbox is the other half of making a project the root of everything:
// a session could always exist with no project at all ("chat only"), and once
// the picker is organized by project that session would have nowhere to show
// up. It must not be hidden or silently dropped, so it moves into an ordinary
// project named Inbox that the user can rename, pin or delete once it is
// empty. The WHERE EXISTS guard means Inbox is never created in a database
// that has no orphan session to receive it -- an empty category must not be
// drawn, and that includes not being created.
const schemaV29Inbox = `
INSERT OR IGNORE INTO projects(id,name,root_path,state,created_at,updated_at)
  SELECT 'project_inbox','Inbox','','active',
         strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
  WHERE EXISTS(SELECT 1 FROM agent_sessions WHERE project_id IS NULL OR project_id='');
UPDATE agent_sessions SET project_id='project_inbox'
  WHERE project_id IS NULL OR project_id='';
`

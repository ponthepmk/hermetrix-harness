package store

const schemaV79 = `
CREATE TABLE IF NOT EXISTS behavioral_eval_runs (
  id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL REFERENCES local_principals(id),
  candidate_id TEXT NOT NULL REFERENCES skill_candidates(id),
  candidate_revision INTEGER NOT NULL CHECK(candidate_revision > 0),
  candidate_hash TEXT NOT NULL,
  manifest_json TEXT NOT NULL,
  manifest_sha256 TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'prepared' CHECK(state IN ('prepared','dispatched','completed','uncertain','stale','aborted')),
  evaluation_id TEXT UNIQUE REFERENCES candidate_behavioral_evals(id),
  result_sha256 TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  completed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_behavioral_runs_candidate ON behavioral_eval_runs(owner_principal_id, candidate_id, started_at);
CREATE INDEX IF NOT EXISTS idx_behavioral_runs_state ON behavioral_eval_runs(owner_principal_id, state, started_at);
CREATE TRIGGER IF NOT EXISTS behavioral_runs_manifest_immutable
  BEFORE UPDATE OF id, owner_principal_id, candidate_id, candidate_revision, candidate_hash, manifest_json, manifest_sha256, started_at
  ON behavioral_eval_runs
  BEGIN
    SELECT RAISE(ABORT, 'behavioral_eval_runs immutable fields cannot be updated');
  END;
`

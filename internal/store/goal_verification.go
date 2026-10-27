package store

const schemaV78 = `
CREATE TABLE IF NOT EXISTS task_goal_verifications (
  proposal_id TEXT PRIMARY KEY REFERENCES task_code_proposals(id),
  goal_id TEXT NOT NULL REFERENCES task_goal_runs(id),
  revision INTEGER NOT NULL,
  state TEXT NOT NULL,
  progress_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_goal_verification_owner ON task_goal_verifications(goal_id,state);
`

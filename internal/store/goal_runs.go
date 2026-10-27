package store

const schemaV77 = `
CREATE TABLE IF NOT EXISTS task_goal_runs (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES durable_tasks(id),
  requirement_revision INTEGER NOT NULL,
  expected_task_revision INTEGER NOT NULL,
  actor TEXT NOT NULL,
  config_json TEXT NOT NULL,
  state TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  actions_used INTEGER NOT NULL DEFAULT 0,
  attempts_used INTEGER NOT NULL DEFAULT 0,
  plans_used INTEGER NOT NULL DEFAULT 0,
  claim_token TEXT NOT NULL DEFAULT '',
  deadline TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_goal_task_active ON task_goal_runs(task_id)
  WHERE state IN ('active','waiting','stopping');
CREATE INDEX IF NOT EXISTS idx_goal_dispatch ON task_goal_runs(state,updated_at);
CREATE TABLE IF NOT EXISTS task_goal_actions (
  id TEXT PRIMARY KEY,
  goal_id TEXT NOT NULL REFERENCES task_goal_runs(id),
  ordinal INTEGER NOT NULL,
  action TEXT NOT NULL,
  task_revision INTEGER NOT NULL,
  state TEXT NOT NULL,
  receipt_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  completed_at TEXT,
  UNIQUE(goal_id,ordinal)
);
`

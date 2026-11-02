package store

const schemaV83 = `
CREATE TABLE IF NOT EXISTS automations (
 id TEXT PRIMARY KEY,
 owner_principal_id TEXT NOT NULL REFERENCES local_principals(id),
 name TEXT NOT NULL,
 template_json TEXT NOT NULL,
 interval_seconds INTEGER NOT NULL CHECK(interval_seconds=0 OR interval_seconds BETWEEN 300 AND 2592000),
 timezone TEXT NOT NULL,
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 next_run_at TEXT NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS automation_runs (
 id TEXT PRIMARY KEY,
 automation_id TEXT NOT NULL REFERENCES automations(id),
 dedup_key TEXT NOT NULL,
 task_id TEXT REFERENCES durable_tasks(id),
 state TEXT NOT NULL CHECK(state IN ('awaiting_review','skipped_overlap')),
 created_at TEXT NOT NULL,
 UNIQUE(automation_id,dedup_key)
);
CREATE INDEX IF NOT EXISTS automation_owner ON automations(owner_principal_id);
`

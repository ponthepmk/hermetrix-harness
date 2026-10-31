package store

// Task subjects have a real Task foreign key. They must not be inserted into
// the conversation outbox, whose session_id references agent_sessions.
const schemaV82 = `
CREATE TABLE IF NOT EXISTS task_learning_outbox (
 id TEXT PRIMARY KEY,
 task_id TEXT NOT NULL,
 session_id TEXT NOT NULL CHECK(session_id = 'task:' || task_id),
 turn_id TEXT NOT NULL,
 milestone_id TEXT NOT NULL,
 job_id TEXT NOT NULL DEFAULT '',
 trigger_kind TEXT NOT NULL,
 digest_json TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending',
 error TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 processed_at TEXT,
 UNIQUE(task_id,milestone_id,trigger_kind),
 FOREIGN KEY(task_id) REFERENCES durable_tasks(id)
);
CREATE INDEX IF NOT EXISTS idx_task_learning_outbox_state ON task_learning_outbox(state,created_at);
`

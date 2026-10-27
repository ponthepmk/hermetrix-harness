package store

const schemaV75 = `
CREATE TABLE IF NOT EXISTS agent_turn_controls (
 session_id TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
 turn_id TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('stopping','stopped','needs_review')),
 requested_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 reason TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(session_id,turn_id)
);
CREATE TABLE IF NOT EXISTS agent_message_queue (
 id TEXT PRIMARY KEY,
 session_id TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
 owner_principal_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 provider_id TEXT NOT NULL,
 contract_revision TEXT NOT NULL,
 cache_epoch INTEGER NOT NULL,
 content TEXT NOT NULL,
 mode TEXT NOT NULL CHECK(mode IN ('queue','steer')),
 base_turn_id TEXT NOT NULL DEFAULT '',
 idempotency_key TEXT NOT NULL,
 payload_hash TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('queued','running','awaiting_approval','completed','failed','cancelled','needs_review')),
 turn_id TEXT NOT NULL DEFAULT '',
 error TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(session_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_agent_message_queue_pending ON agent_message_queue(state,created_at);
`

const schemaV76 = `ALTER TABLE agent_turn_controls ADD COLUMN run_ids_json TEXT NOT NULL DEFAULT '[]';`

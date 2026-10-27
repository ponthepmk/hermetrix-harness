package store

const schemaV80 = `
CREATE TABLE IF NOT EXISTS coding_goal_operations (
  id TEXT NOT NULL,
  owner_principal_id TEXT NOT NULL REFERENCES local_principals(id),
  request_sha256 TEXT NOT NULL CHECK(length(request_sha256) = 64),
  request_json TEXT NOT NULL CHECK(json_valid(request_json)),
  task_id TEXT NOT NULL UNIQUE REFERENCES durable_tasks(id),
  goal_id TEXT NOT NULL UNIQUE,
  initial_task_revision INTEGER NOT NULL CHECK(initial_task_revision > 0),
  goal_config_json TEXT NOT NULL CHECK(json_valid(goal_config_json)),
  created_at TEXT NOT NULL,
  PRIMARY KEY (owner_principal_id, id)
);
CREATE INDEX IF NOT EXISTS idx_coding_goal_operations_owner ON coding_goal_operations(owner_principal_id, created_at);
CREATE TRIGGER IF NOT EXISTS coding_goal_operations_immutable_update
  BEFORE UPDATE
  ON coding_goal_operations
  BEGIN
    SELECT RAISE(ABORT, 'coding_goal_operations rows are immutable');
  END;
CREATE TRIGGER IF NOT EXISTS coding_goal_operations_immutable_delete
  BEFORE DELETE
  ON coding_goal_operations
  BEGIN
    SELECT RAISE(ABORT, 'coding_goal_operations rows cannot be deleted');
  END;
`

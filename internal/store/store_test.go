package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrationFromV1AddsLearningAgentAndBindingTables(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(root, "hermetrix.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	dataStore, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	var schemaVersion int
	if err := dataStore.DB.QueryRow(`PRAGMA user_version`).Scan(&schemaVersion); err != nil || schemaVersion != CurrentSchemaVersion {
		t.Fatalf("schema version=%d err=%v", schemaVersion, err)
	}
	// A version number proves nothing on its own. Check that the newest
	// migration actually added its columns.
	for _, column := range []string{"reasoning_ratio", "reasoning_sample"} {
		var found int
		if err := dataStore.DB.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('provider_profiles') WHERE name=?`, column).Scan(&found); err != nil || found != 1 {
			t.Fatalf("provider_profiles is missing column %s: found=%d err=%v", column, found, err)
		}
	}
	for _, table := range []string{"learning_reviews", "learning_trigger_outbox", "curator_runs", "provider_profiles", "agent_sessions", "agent_events", "context_snapshots", "event_embeddings", "step_bindings", "tool_approvals", "mcp_servers", "mcp_tools", "skill_replay_runs", "skill_replay_cases", "candidate_capability_reviews", "context_eval_cases", "context_eval_runs", "model_qualification_runs", "projects", "artifacts", "background_jobs", "settings", "memories", "backup_runs", "curator_findings", "maintenance_schedules", "gc_runs", "skill_authority_policy", "skill_authority_actions"} {
		var found string
		if err := dataStore.DB.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&found); err != nil {
			t.Fatalf("missing %s: %v", table, err)
		}
	}
	var sourceColumn int
	rows, err := dataStore.DB.Query(`PRAGMA table_info(skill_candidates)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "source_review_id" {
			sourceColumn++
		}
	}
	if sourceColumn != 1 {
		t.Fatalf("source_review_id columns = %d", sourceColumn)
	}
	var toolBindingsColumn int
	stepRows, err := dataStore.DB.Query(`PRAGMA table_info(step_bindings)`)
	if err != nil {
		t.Fatal(err)
	}
	defer stepRows.Close()
	for stepRows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue any
		if err := stepRows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "tool_bindings_json" {
			toolBindingsColumn++
		}
	}
	if toolBindingsColumn != 1 {
		t.Fatalf("tool_bindings_json columns = %d", toolBindingsColumn)
	}
	var version int
	if err := dataStore.DB.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("schema version = %d, err=%v", version, err)
	}
}

func TestMigrationFromPreReleaseV11AcceptsQualificationColumnsAlreadyPresent(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(root, "hermetrix.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{schemaV1, schemaV2, schemaV3, schemaV4, schemaV5, schemaV6, schemaV7, schemaV8, schemaV9,
		schemaV10, schemaV11} {
		if _, err := db.Exec(schema); err != nil {
			t.Fatal(err)
		}
	}
	// Reproduce the interim schema-11 shape that already had the V12 fields.
	for _, statement := range []string{
		`ALTER TABLE model_qualification_runs ADD COLUMN requested_profile TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE model_qualification_runs ADD COLUMN eligible INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE model_qualification_runs ADD COLUMN requires_decision INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`PRAGMA user_version=11`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	dataStore, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	var version int
	if err := dataStore.DB.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
}

package store

import (
	"context"
	"database/sql"
	"testing"
)

func TestMergedRetryMigrationLegacyDesktop(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, "CREATE TABLE agent_sessions (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatalf("create agent_sessions: %v", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE desktop_actions (id TEXT PRIMARY KEY, label TEXT)"); err != nil {
		t.Fatalf("create desktop_actions: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO desktop_actions (id, label) VALUES ('sentinel', 'keep')"); err != nil {
		t.Fatalf("insert sentinel: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := migrateV81(ctx, tx); err != nil {
		t.Fatalf("migrate first: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit first: %v", err)
	}

	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin second: %v", err)
	}
	if err := migrateV81(ctx, tx); err != nil {
		t.Fatalf("migrate second: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit second: %v", err)
	}

	var label, before, after, obs string
	row := db.QueryRowContext(ctx, "SELECT label, screenshot_before_id, screenshot_after_id, observation_json FROM desktop_actions WHERE id='sentinel'")
	if err := row.Scan(&label, &before, &after, &obs); err != nil {
		t.Fatalf("select sentinel: %v", err)
	}
	if label != "keep" {
		t.Fatalf("label = %q", label)
	}
	if before != "" || after != "" || obs != "" {
		t.Fatalf("unexpected nonempty: %q %q %q", before, after, obs)
	}

	var count int
	row = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('agent_retries','agent_approval_modes')")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if count != 2 {
		t.Fatalf("table count = %d", count)
	}
}

func TestMergedRetryMigrationUpgrade80(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	data, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer data.Close()

	db := data.DB
	if _, err := db.ExecContext(ctx, "CREATE TABLE merge_sentinel (value TEXT)"); err != nil {
		t.Fatalf("create merge_sentinel: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO merge_sentinel (value) VALUES ('keep')"); err != nil {
		t.Fatalf("insert merge_sentinel: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS agent_retries"); err != nil {
		t.Fatalf("drop agent_retries: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS agent_approval_modes"); err != nil {
		t.Fatalf("drop agent_approval_modes: %v", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA user_version=80"); err != nil {
		t.Fatalf("set user_version: %v", err)
	}

	if err := data.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	data, err = Open(ctx, dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer data.Close()

	ver, err := data.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if ver != CurrentSchemaVersion {
		t.Fatalf("version = %d", ver)
	}
	if CurrentSchemaVersion != 81 {
		t.Fatalf("current = %d", CurrentSchemaVersion)
	}

	var count int
	row := data.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('agent_retries','agent_approval_modes')")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if count != 2 {
		t.Fatalf("table count = %d", count)
	}

	var value string
	row = data.DB.QueryRowContext(ctx, "SELECT value FROM merge_sentinel")
	if err := row.Scan(&value); err != nil {
		t.Fatalf("select merge_sentinel: %v", err)
	}
	if value != "keep" {
		t.Fatalf("value = %q", value)
	}
}

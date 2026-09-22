package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"hermetrix-harness/internal/store"
)

func TestMigrationV48ToV49IsAdditiveAndIdempotent(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "data")
	dataStore, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dataStore.DB.ExecContext(ctx, `INSERT INTO settings(key,value_json,updated_at)
		VALUES('h1.migration.sentinel','{"preserved":true}','2026-09-22T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	if err = dataStore.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "hermetrix.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `DROP TABLE agent_platform_outbox;
		DROP TABLE agent_platform_streams;
		DROP TABLE agent_platform_assignments;
		DROP TABLE agent_platform_bindings;
		PRAGMA user_version=48;`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	var sentinel string
	if err = reopened.DB.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key='h1.migration.sentinel'`).Scan(&sentinel); err != nil {
		t.Fatal(err)
	}
	if sentinel != `{"preserved":true}` {
		t.Fatalf("pre-v49 data changed: %s", sentinel)
	}
	for _, table := range []string{"agent_platform_bindings", "agent_platform_assignments", "agent_platform_streams", "agent_platform_outbox"} {
		var exists int
		if err = reopened.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil || exists != 1 {
			t.Fatalf("table %s exists=%d err=%v", table, exists, err)
		}
	}
	rows, err := reopened.DB.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		rows.Close()
		t.Fatal("foreign_key_check reported a violation")
	}
	rows.Close()
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	idempotent, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer idempotent.Close()
	version, err := idempotent.SchemaVersion(ctx)
	if err != nil || version != store.CurrentSchemaVersion {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
}

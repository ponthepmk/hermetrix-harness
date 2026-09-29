package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestBrowserKeyMigrationKeepsExistingReceiptAndReconciliation(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "browser-v71.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(ctx, `PRAGMA foreign_keys=ON;
CREATE TABLE local_principals(id TEXT PRIMARY KEY);
CREATE TABLE browser_tabs(id TEXT PRIMARY KEY);
CREATE TABLE browser_action_receipts (
  id TEXT PRIMARY KEY, owner_principal_id TEXT NOT NULL, task_id TEXT NOT NULL DEFAULT '',
  tab_id TEXT NOT NULL, action TEXT NOT NULL CHECK(action IN ('click','type')),
  element_ref INTEGER NOT NULL, target_json TEXT NOT NULL, text_hash TEXT NOT NULL,
  observation_hash TEXT NOT NULL, subject_hash TEXT NOT NULL,
  screenshot_before_id TEXT NOT NULL DEFAULT '', screenshot_after_id TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL DEFAULT '', state TEXT NOT NULL CHECK(state IN ('prepared','cancelled','dispatched','completed','uncertain')),
  error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, expires_at TEXT NOT NULL,
  dispatched_at TEXT, completed_at TEXT,
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id),
  FOREIGN KEY(tab_id) REFERENCES browser_tabs(id));
CREATE TABLE browser_action_reconciliations (
  action_id TEXT PRIMARY KEY, owner_principal_id TEXT NOT NULL, resolution TEXT NOT NULL,
  observation_hash TEXT NOT NULL, screenshot_artifact_id TEXT NOT NULL,
  verification_tab_id TEXT NOT NULL, actor TEXT NOT NULL, note TEXT NOT NULL, created_at TEXT NOT NULL,
  FOREIGN KEY(action_id) REFERENCES browser_action_receipts(id),
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id),
  FOREIGN KEY(verification_tab_id) REFERENCES browser_tabs(id));
INSERT INTO local_principals VALUES('owner');
INSERT INTO browser_tabs VALUES('tab');
INSERT INTO browser_action_receipts(id,owner_principal_id,tab_id,action,element_ref,target_json,text_hash,observation_hash,subject_hash,state,created_at,expires_at)
 VALUES('old','owner','tab','click',1,'{}','hash','observation','subject','uncertain','2026-09-29T00:00:00Z','2026-09-29T00:02:00Z');
INSERT INTO browser_action_reconciliations VALUES('old','owner','observed_applied','observation','shot','tab','tester','observed','2026-09-29T00:01:00Z');`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateV72(ctx, tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var action, resolution string
	if err := db.QueryRowContext(ctx, `SELECT action FROM browser_action_receipts WHERE id='old'`).Scan(&action); err != nil || action != "click" {
		t.Fatalf("old receipt=%q err=%v", action, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT resolution FROM browser_action_reconciliations WHERE action_id='old'`).Scan(&resolution); err != nil || resolution != "observed_applied" {
		t.Fatalf("old reconciliation=%q err=%v", resolution, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO browser_action_receipts(id,owner_principal_id,tab_id,action,element_ref,target_json,text_hash,observation_hash,subject_hash,state,created_at,expires_at)
 VALUES('new','owner','tab','key',1,'{}','hash','observation','subject','prepared','2026-09-29T00:00:00Z','2026-09-29T00:02:00Z')`); err != nil {
		t.Fatal(err)
	}
	var violations int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("foreign key violations=%d err=%v", violations, err)
	}
}

func TestBrowserActionV65MigrationPreservesExistingTabs(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "browser-migration")
	dataStore, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO browser_tabs(id,url,title,state,created_at,updated_at)
		VALUES('existing-tab','https://example.test/','Earlier page','closed','2026-09-26T00:00:00Z','2026-09-26T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	// A v64 directory had tabs but no mutation receipts. Keep the tab while
	// recreating only the new tables during upgrade.
	if _, err := dataStore.DB.ExecContext(ctx, `DROP TABLE browser_action_reconciliations;
		DROP TABLE browser_action_receipts; PRAGMA user_version=64;`); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.Close(); err != nil {
		t.Fatal(err)
	}
	dataStore, err = Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	var title string
	if err := dataStore.DB.QueryRowContext(ctx, `SELECT title FROM browser_tabs WHERE id='existing-tab'`).Scan(&title); err != nil || title != "Earlier page" {
		t.Fatalf("existing browser tab changed: %q %v", title, err)
	}
	for _, name := range []string{"browser_action_receipts", "browser_action_reconciliations"} {
		var count int
		if err := dataStore.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&count); err != nil || count != 1 {
			t.Fatalf("table %s missing after migration: %d %v", name, count, err)
		}
	}
	if version, err := dataStore.SchemaVersion(ctx); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("schema=%d err=%v", version, err)
	}
	var violations int
	if err := dataStore.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("foreign key violations=%d err=%v", violations, err)
	}
}

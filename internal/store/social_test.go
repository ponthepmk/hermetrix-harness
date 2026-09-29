package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSocialV59MigrationPreservesExistingAccountMetadata(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "social-migration")
	dataStore, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := dataStore.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO social_connections(id,owner_principal_id,platform,account_id,display_name,capability,status,checked_at,created_at) VALUES('connection-1',?,'facebook','page-1','Page One','page_identity','ready','2026-09-26T00:00:00Z','2026-09-26T00:00:00Z')`, owner)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a v57 data directory: identity records exist, export records do
	// not yet exist. Reopen must add both export and retry state.
	if _, err := dataStore.DB.ExecContext(ctx, `DROP TABLE social_posts`); err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.DB.ExecContext(ctx, `PRAGMA user_version=57`); err != nil {
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
	var accountID, name string
	if err := dataStore.DB.QueryRowContext(ctx, `SELECT account_id,display_name FROM social_connections WHERE id='connection-1'`).Scan(&accountID, &name); err != nil {
		t.Fatal(err)
	}
	if accountID != "page-1" || name != "Page One" {
		t.Fatalf("existing identity changed: %q %q", accountID, name)
	}
	var table string
	if err := dataStore.DB.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='social_posts'`).Scan(&table); err != nil || table != "social_posts" {
		t.Fatalf("social_posts missing: %q %v", table, err)
	}
	if version, err := dataStore.SchemaVersion(ctx); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("schema=%d err=%v", version, err)
	}
}

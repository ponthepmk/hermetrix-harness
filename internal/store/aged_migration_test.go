package store

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// This release smoke test reads a copy of an installed database only when an
// operator supplies its path. It never opens the source for writing or copies
// its credential vault into the temporary migration target.
func TestAgedInstalledDatabaseMigratesOnCopy(t *testing.T) {
	sourcePath := os.Getenv("HERMETRIX_AGED_DB")
	if sourcePath == "" {
		t.Skip("set HERMETRIX_AGED_DB to test a copy of installed data")
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	root := t.TempDir()
	target, err := os.OpenFile(filepath.Join(root, "hermetrix.db"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dataStore, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	var projects, sessions, violations int
	if err := dataStore.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&projects); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		t.Fatal(err)
	}
	if version, err := dataStore.SchemaVersion(ctx); err != nil || version != CurrentSchemaVersion || violations != 0 {
		t.Fatalf("copy migration schema=%d foreign-key-errors=%d err=%v", version, violations, err)
	}
	if err := dataStore.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var reopenedProjects, reopenedSessions int
	if err := reopened.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&reopenedProjects); err != nil {
		t.Fatal(err)
	}
	if err := reopened.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_sessions`).Scan(&reopenedSessions); err != nil {
		t.Fatal(err)
	}
	if reopenedProjects != projects || reopenedSessions != sessions {
		t.Fatalf("reopen changed counts: projects %d→%d sessions %d→%d", projects, reopenedProjects, sessions, reopenedSessions)
	}
	t.Logf("copied database migrated to schema %d with %d projects and %d sessions", CurrentSchemaVersion, projects, sessions)
}

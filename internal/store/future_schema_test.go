package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRejectsFutureSchemaWithoutChangingDatabase(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "hermetrix.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, fmt.Sprintf(`PRAGMA journal_mode=DELETE; CREATE TABLE future_data(value TEXT); INSERT INTO future_data VALUES('preserve'); PRAGMA user_version=%d;`, CurrentSchemaVersion+1)); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		opened, openErr := Open(ctx, root)
		if opened != nil {
			opened.Close()
			t.Fatal("older binary opened future database")
		}
		if openErr == nil || !strings.Contains(openErr.Error(), "newer than supported") {
			t.Fatalf("attempt%d: %v", attempt, openErr)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("failed open modified future database bytes")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	var mode, value string
	if err = db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM future_data`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if version != CurrentSchemaVersion+1 || mode != "delete" || value != "preserve" {
		t.Fatalf("future state changed: %d %s %s", version, mode, value)
	}
}

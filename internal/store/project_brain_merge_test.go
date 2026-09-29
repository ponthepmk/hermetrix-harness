package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

// Both branches shipped a different v50 schema. Exercise upgrades from each
// lineage so a version number alone cannot hide the missing tables or column.
func TestMigrationJoinsProjectBrainAndGoalContractLineages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int
		remove  []string
	}{
		{name: "project-brain-v50", version: 50, remove: []string{
			`DROP TABLE task_user_acceptances`,
			`ALTER TABLE task_requirement_revisions DROP COLUMN contract_json`,
		}},
		{name: "goal-contract-v72", version: 72, remove: []string{
			`DROP TABLE project_brain_candidate_outbox`,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			current, err := Open(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			if err := current.Close(); err != nil {
				t.Fatal(err)
			}

			db, err := sql.Open("sqlite", filepath.Join(root, "hermetrix.db"))
			if err != nil {
				t.Fatal(err)
			}
			for _, query := range tc.remove {
				if _, err := db.Exec(query); err != nil {
					t.Fatalf("prepare %s: %v", tc.name, err)
				}
			}
			if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, tc.version)); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			upgraded, err := Open(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			defer upgraded.Close()
			version, err := upgraded.SchemaVersion(context.Background())
			if err != nil || version != CurrentSchemaVersion {
				t.Fatalf("schema=%d err=%v", version, err)
			}
			for _, table := range []string{"task_user_acceptances", "project_brain_candidate_outbox"} {
				var count int
				if err := upgraded.DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
					t.Fatalf("table %s count=%d err=%v", table, count, err)
				}
			}
			var count int
			if err := upgraded.DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('task_requirement_revisions') WHERE name='contract_json'`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("contract_json count=%d err=%v", count, err)
			}
		})
	}
}

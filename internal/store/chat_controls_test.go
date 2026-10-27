package store

import (
	"context"
	"testing"
)

func TestChatControlMigrationFromBothV75PreviewLayouts(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "without job receipts", true: "with job receipts"}[existing], func(t *testing.T) {
			root := t.TempDir()
			ctx := context.Background()
			s, err := Open(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if !existing {
				if _, err = s.DB.Exec(`ALTER TABLE agent_turn_controls DROP COLUMN run_ids_json`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = s.DB.Exec(`PRAGMA user_version=75`); err != nil {
				t.Fatal(err)
			}
			s.Close()
			s, err = Open(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			var n int
			var version int
			if err = s.DB.QueryRow(`SELECT count(*) FROM pragma_table_info('agent_turn_controls') WHERE name='run_ids_json'`).Scan(&n); err != nil || n != 1 {
				t.Fatalf("job receipt columns=%d err=%v", n, err)
			}
			if err = s.DB.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != CurrentSchemaVersion {
				t.Fatal(version, err)
			}
			var integrity string
			if err = s.DB.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
				t.Fatal(integrity, err)
			}
		})
	}
}

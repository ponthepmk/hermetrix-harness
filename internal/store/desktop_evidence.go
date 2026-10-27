package store

import (
	"context"
	"database/sql"
)

// Additive migration preserves all existing actions and reconciliations.
func migrateV74(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(desktop_actions)`)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var cid, nn, pk int
		var name, kind string
		var def sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &nn, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, column := range []string{"screenshot_before_id", "screenshot_after_id", "observation_json"} {
		if existing[column] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE desktop_actions ADD COLUMN `+column+` TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

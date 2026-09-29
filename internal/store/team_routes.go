package store

import (
	"context"
	"database/sql"
	"fmt"
)

// A Team task freezes its model route so Main and Worker can use distinct
// providers without changing the roster or a running child contract.
func migrateV71(ctx context.Context, tx *sql.Tx) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='agent_team_tasks'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	for _, column := range []string{"provider_id", "context_profile"} {
		var present bool
		rows, err := tx.QueryContext(ctx, `PRAGMA table_info(agent_team_tasks)`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, kind string
			var defaultValue sql.NullString
			if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
				rows.Close()
				return err
			}
			if name == column {
				present = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if !present {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE agent_team_tasks ADD COLUMN %s TEXT NOT NULL DEFAULT ''`, column)); err != nil {
				return err
			}
		}
	}
	return nil
}

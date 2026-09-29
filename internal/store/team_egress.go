package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Team runs persist remote egress authority so resumed children use the same
// reviewed decision as the original run rather than a process-local default.
func migrateV70(ctx context.Context, tx *sql.Tx) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='agent_team_runs'`).Scan(&exists); err != nil {
		return err
	}
	// Some older, partial fixtures carry a later user_version without the Team
	// feature table. Preserve those databases; Team operations already require
	// the table and will report a missing feature instead of failing all startup.
	if exists == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(agent_team_runs)`)
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, column := range []struct{ name, definition string }{
		{"egress_policy", `TEXT NOT NULL DEFAULT 'local_only'`},
		{"remote_egress_actor", `TEXT NOT NULL DEFAULT ''`},
		{"remote_egress_reason", `TEXT NOT NULL DEFAULT ''`},
	} {
		if columns[column.name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE agent_team_runs ADD COLUMN %s %s`, column.name, column.definition)); err != nil {
			return err
		}
	}
	return nil
}

package store

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateV79OutputPaths adds the output_paths_json column to task_steps if it
// does not already exist. It must be called within the caller's transaction
// and does not commit or roll back on its own. The function is idempotent:
// if the column is already present it returns nil without issuing any DDL.
//
// No schema_version mutation is performed here; the caller (store.go) is
// responsible for bumping the version after all migrations have run.
func migrateV79OutputPaths(ctx context.Context, tx *sql.Tx) error {
	var count int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('task_steps') WHERE name='output_paths_json'`,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("check output_paths_json column: %w", err)
	}
	if count > 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx,
		`ALTER TABLE task_steps ADD COLUMN output_paths_json TEXT NOT NULL DEFAULT '[]'`,
	)
	if err != nil {
		return fmt.Errorf("add output_paths_json column: %w", err)
	}
	return nil
}

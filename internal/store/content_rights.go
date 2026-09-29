package store

import (
	"context"
	"database/sql"
)

// v56 makes media provenance review part of the exact content approval.
// Old drafts remain readable; a draft using video needs a rights note before
// it can receive a new approval.
func migrateV56(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(task_content_drafts)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == "media_rights_note" {
			rows.Close()
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE task_content_drafts ADD COLUMN media_rights_note TEXT NOT NULL DEFAULT '';`)
	return err
}

package store

import (
	"context"
	"database/sql"
)

// v57 stores verified destination identities and grant observations. Tokens
// live only in the encrypted credential vault and never in SQLite or backups.
func migrateV57(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS social_connections (
  id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL,
  platform TEXT NOT NULL CHECK(platform IN ('facebook','tiktok')),
  account_id TEXT NOT NULL,
  display_name TEXT NOT NULL,
  capability TEXT NOT NULL,
  status TEXT NOT NULL,
  checked_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(owner_principal_id,platform,account_id),
  FOREIGN KEY(owner_principal_id) REFERENCES local_principals(id)
);
CREATE INDEX IF NOT EXISTS idx_social_connections_owner ON social_connections(owner_principal_id,platform);
`)
	return err
}

// v58 records exact, separately approved export intents before any remote
// effect. Remote upload URLs can contain credentials and stay in the vault.
func migrateV58(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS social_posts (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  draft_id TEXT NOT NULL,
  connection_id TEXT NOT NULL,
  owner_principal_id TEXT NOT NULL,
  subject_hash TEXT NOT NULL,
  intent_hash TEXT NOT NULL,
  route TEXT NOT NULL,
  privacy_level TEXT NOT NULL DEFAULT '',
  is_aigc INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL,
  remote_id TEXT NOT NULL DEFAULT '',
  remote_status TEXT NOT NULL DEFAULT '',
  remote_url TEXT NOT NULL DEFAULT '',
  uploaded_bytes INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  approved_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(owner_principal_id,draft_id,connection_id,intent_hash),
  FOREIGN KEY(task_id) REFERENCES durable_tasks(id) ON DELETE CASCADE,
  FOREIGN KEY(draft_id) REFERENCES task_content_drafts(id) ON DELETE CASCADE,
  FOREIGN KEY(connection_id) REFERENCES social_connections(id)
);
CREATE INDEX IF NOT EXISTS idx_social_posts_task ON social_posts(task_id,created_at);
`)
	return err
}

// v59 makes a definite failure retryable through a new, separately approved
// export record while retaining the prior remote receipt and attempt history.
func migrateV59(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(social_posts)`)
	if err != nil {
		return err
	}
	hasRetryOf := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "retry_of" {
			hasRetryOf = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !hasRetryOf {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE social_posts ADD COLUMN retry_of TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_social_post_single_retry ON social_posts(retry_of) WHERE retry_of<>''`)
	return err
}

// v62 binds commercial-content declarations to the exact TikTok export intent.
// Older export records retain false defaults and their existing approval hash.
func migrateV62(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []string{"brand_content", "brand_organic"} {
		rows, err := tx.QueryContext(ctx, `PRAGMA table_info(social_posts)`)
		if err != nil {
			return err
		}
		found := false
		for rows.Next() {
			var cid, notNull, pk int
			var name, kind string
			var defaultValue sql.NullString
			if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
				rows.Close()
				return err
			}
			found = found || name == column
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if !found {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE social_posts ADD COLUMN `+column+` INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}
		}
	}
	return nil
}

// v63 binds creator interaction settings and the posting declaration to the
// reviewed TikTok Direct Post intent. Old records require a new reviewed intent.
func migrateV63(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []string{"allow_comment", "allow_duet", "allow_stitch", "music_consent"} {
		rows, err := tx.QueryContext(ctx, `PRAGMA table_info(social_posts)`)
		if err != nil {
			return err
		}
		found := false
		for rows.Next() {
			var cid, notNull, pk int
			var name, kind string
			var defaultValue sql.NullString
			if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
				rows.Close()
				return err
			}
			found = found || name == column
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if !found {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE social_posts ADD COLUMN `+column+` INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}
		}
	}
	return nil
}

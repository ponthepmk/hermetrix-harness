package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/store"
)

// Only the pristine, fully migrated bytes are shared. Each caller still opens
// its own real database, root lock, blob directory and installation principal.
// Production Open and the migration suite do not use this fixture.
var agentStoreImage struct {
	sync.Once
	data []byte
	err  error
}

func openAgentFixtureStore(t testing.TB) (*store.Store, error) {
	t.Helper()
	ctx := context.Background()
	agentStoreImage.Do(func() {
		root := filepath.Join(t.TempDir(), "pristine-schema")
		s, err := store.Open(ctx, root)
		if err != nil {
			agentStoreImage.err = err
			return
		}
		// This fixture currently has no blob seeds. Fail explicitly if a future
		// migration adds any, instead of creating dangling database references.
		err = filepath.WalkDir(filepath.Join(root, "blobs"), func(_ string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() {
				return fmt.Errorf("agent fixture needs migration blob seeds copied")
			}
			return nil
		})
		closeErr := s.Close()
		if err != nil {
			agentStoreImage.err = err
			return
		}
		if closeErr != nil {
			agentStoreImage.err = closeErr
			return
		}
		// Closing the final SQLite connection checkpoints its WAL before the
		// image is read. No live database or file handle is shared with tests.
		agentStoreImage.data, agentStoreImage.err = os.ReadFile(filepath.Join(root, "hermetrix.db"))
	})
	if agentStoreImage.err != nil {
		return nil, agentStoreImage.err
	}
	root := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(root, "hermetrix.db"), agentStoreImage.data, 0600); err != nil {
		return nil, err
	}
	s, err := store.Open(ctx, root)
	if err != nil {
		return nil, err
	}
	// Seed tables currently do not reference this installation principal.
	// Foreign keys remain enabled: a new referenced seed makes this fail rather
	// than silently sharing an installation identity across fresh test roots.
	result, err := s.DB.ExecContext(ctx, `UPDATE local_principals SET id=?,created_at=? WHERE kind='local'`,
		identity.New("principal"), time.Now().UTC().Format(time.RFC3339Nano))
	if err == nil {
		var n int64
		n, err = result.RowsAffected()
		if err == nil && n != 1 {
			err = fmt.Errorf("expected one local fixture principal, got %d", n)
		}
	}
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func TestAgentFixtureStoreRetainsFreshStateAndRealStoreGuards(t *testing.T) {
	ctx := context.Background()
	cold, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cold.Close() })
	first, err := openAgentFixtureStore(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := openAgentFixtureStore(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	firstOwner, err := first.LocalPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondOwner, err := second.LocalPrincipalID(ctx)
	if err != nil || firstOwner == secondOwner || first.Root == second.Root {
		t.Fatalf("fixture installations were shared: first=%s second=%s err=%v", firstOwner, secondOwner, err)
	}
	wantSchema, wantSeeds := agentFixtureShape(t, cold)
	for _, s := range []*store.Store{first, second} {
		schema, seeds := agentFixtureShape(t, s)
		if !reflect.DeepEqual(schema, wantSchema) || !reflect.DeepEqual(seeds, wantSeeds) {
			t.Fatal("fixture schema or seeded row counts differ from fresh production migrations")
		}
		version, err := s.SchemaVersion(ctx)
		if err != nil || version != store.CurrentSchemaVersion {
			t.Fatalf("schema=%d err=%v", version, err)
		}
		var foreignKeys int
		if err = s.DB.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
			t.Fatalf("foreign keys=%d err=%v", foreignKeys, err)
		}
		var journal, integrity string
		if err = s.DB.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journal); err != nil || journal != "wal" {
			t.Fatalf("journal=%s err=%v", journal, err)
		}
		if err = s.DB.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
			t.Fatalf("integrity=%s err=%v", integrity, err)
		}
	}
	if _, err = first.DB.ExecContext(ctx, `INSERT INTO settings(key,value_json,updated_at) VALUES('fixture-only',?,?)`, `"first"`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = second.DB.QueryRowContext(ctx, `SELECT count(*) FROM settings WHERE key='fixture-only'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("database data leaked: count=%d err=%v", count, err)
	}
	ref, err := first.Blobs.Put([]byte("first fixture only"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = second.Blobs.Get(ref); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blob store was shared: %v", err)
	}
	duplicate, err := store.Open(ctx, first.Root)
	if err == nil {
		_ = duplicate.Close()
		t.Fatal("fixture bypassed the real data-root lock")
	}
}

func agentFixtureShape(t *testing.T, s *store.Store) ([]string, map[string]int) {
	t.Helper()
	ctx := context.Background()
	owner, err := s.LocalPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT type,name,COALESCE(sql,'') FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	var schema, tables []string
	for rows.Next() {
		var kind, name, ddl string
		if err = rows.Scan(&kind, &name, &ddl); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		schema = append(schema, kind+"\x00"+name+"\x00"+ddl)
		if kind == "table" {
			tables = append(tables, name)
		}
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	seeds := map[string]int{}
	for _, table := range tables {
		// Names come only from SQLite's own schema; quote identifier characters.
		quoted := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		var count, owned int
		if err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoted).Scan(&count); err != nil {
			t.Fatal(err)
		}
		seeds[table] = count
		if err = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name='owner_principal_id'`, table).Scan(&owned); err != nil {
			t.Fatal(err)
		}
		if owned != 0 {
			if err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoted+" WHERE owner_principal_id<>'' AND owner_principal_id<>?", owner).Scan(&count); err != nil || count != 0 {
				t.Fatalf("fixture has stale seeded ownership in %s: count=%d err=%v", table, count, err)
			}
		}
	}
	return schema, seeds
}

func BenchmarkAgentStoreBootstrap(b *testing.B) {
	for _, mode := range []string{"cold_migrations", "pristine_image"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var s *store.Store
				var err error
				if mode == "cold_migrations" {
					s, err = store.Open(context.Background(), b.TempDir())
				} else {
					s, err = openAgentFixtureStore(b)
				}
				if err != nil {
					b.Fatal(err)
				}
				if err = s.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Package releaseinfo inspects release and schema metadata without opening the
// application store, acquiring its lock, creating files or running migrations.
package releaseinfo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"hermetrix-harness/internal/store"
)

// Set by the release build script. Unknown values are never inferred as clean.
var SourceCommit = "unknown"
var SourceTreeSHA256 = "unknown"
var SourceDirty = "unknown"

type Manifest struct {
	Format           string `json:"format"`
	Commit           string `json:"source_commit"`
	TreeSHA256       string `json:"source_tree_sha256"`
	Dirty            string `json:"source_dirty"`
	ExecutableSHA256 string `json:"executable_sha256"`
	GoVersion        string `json:"go_version"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	SchemaTarget     int    `json:"schema_target"`
}

func Current() (Manifest, error) {
	m := Manifest{Format: "hermetrix-release/v1", Commit: SourceCommit, TreeSHA256: SourceTreeSHA256, Dirty: SourceDirty, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, SchemaTarget: store.CurrentSchemaVersion}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && m.Commit == "unknown" {
				m.Commit = setting.Value
			}
			if setting.Key == "vcs.modified" && m.Dirty == "unknown" {
				m.Dirty = setting.Value
			}
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return m, err
	}
	f, err := os.Open(executable)
	if err != nil {
		return m, err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return m, err
	}
	m.ExecutableSHA256 = hex.EncodeToString(hash.Sum(nil))
	return m, nil
}

type Inspection struct {
	Exists bool   `json:"exists"`
	Schema int    `json:"schema_version"`
	State  string `json:"state"`
	Ready  bool   `json:"ready_without_migration"`
}

func Inspect(ctx context.Context, root string) (Inspection, error) {
	var result Inspection
	path, err := filepath.Abs(filepath.Join(root, "hermetrix.db"))
	if err != nil {
		return result, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		result.State = "not_initialized"
		return result, nil
	} else if err != nil {
		return result, err
	}
	// Immutable reads must never silently ignore pending WAL transactions. Refuse
	// a nonempty WAL instead of checkpointing or opening a writable shared index.
	if wal, err := os.Stat(path + "-wal"); err == nil && wal.Size() > 0 {
		return result, fmt.Errorf("nonempty WAL: inspect a consistent offline snapshot; no checkpoint performed")
	} else if err != nil && !os.IsNotExist(err) {
		return result, err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if runtime.GOOS == "windows" {
		u.Path = "/" + u.Path
	}
	q := u.Query()
	q.Set("mode", "ro")
	q.Set("immutable", "1")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return result, err
	}
	defer db.Close()
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&result.Schema); err != nil {
		return result, err
	}
	result.Exists = true
	switch {
	case result.Schema > store.CurrentSchemaVersion:
		result.State = "newer_than_binary"
	case result.Schema < store.CurrentSchemaVersion:
		result.State = "migration_required"
	default:
		result.State = "schema_current"
		result.Ready = true
	}
	return result, nil
}

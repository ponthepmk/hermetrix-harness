package product

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/secrets"
	"hermetrix-harness/internal/store"
)

const recoveryFormat = "hermetrix.full-recovery.v1"
const maxRecoveryBytes = 512 << 20
const maxRecoveryEntries = 100_000

func (s *Service) CreateFullRecovery(ctx context.Context, actor string) (BackupRun, []byte, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return BackupRun{}, nil, fmt.Errorf("recovery actor is required")
	}
	now := time.Now().UTC()
	run := BackupRun{ID: identity.New("recovery"), Kind: "full_recovery", State: "running", FormatVersion: 1, Counts: map[string]int{}, CreatedAt: now}
	runtimeRoot := filepath.Join(s.store.Root, "runtime", "recovery", run.ID)
	if err := os.MkdirAll(runtimeRoot, 0700); err != nil {
		s.failBackup(run.ID, err)
		return run, nil, err
	}
	defer os.RemoveAll(runtimeRoot)
	snapshotPath := filepath.Join(runtimeRoot, "hermetrix.db")
	_, _ = s.store.DB.ExecContext(ctx, `PRAGMA wal_checkpoint(FULL)`)
	literal := strings.ReplaceAll(snapshotPath, "'", "''")
	if _, err := s.store.DB.ExecContext(ctx, `VACUUM INTO '`+literal+`'`); err != nil {
		s.failBackup(run.ID, err)
		return run, nil, fmt.Errorf("create consistent SQLite snapshot: %w", err)
	}
	if err := prepareRecoverySnapshot(snapshotPath); err != nil {
		return run, nil, fmt.Errorf("prepare recovery snapshot: %w", err)
	}
	databaseBody, err := os.ReadFile(snapshotPath)
	if err != nil {
		s.failBackup(run.ID, err)
		return run, nil, err
	}
	required, err := requiredRecoveryBlobRefs(snapshotPath)
	if err != nil {
		s.failBackup(run.ID, err)
		return run, nil, err
	}
	refs := make([]string, 0, len(required))
	for ref := range required {
		if !s.store.Blobs.Exists(ref) {
			err = fmt.Errorf("database references missing CAS blob %s", ref)
			s.failBackup(run.ID, err)
			return run, nil, err
		}
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	schemaVersion, err := snapshotSchemaVersion(snapshotPath)
	if err != nil {
		return run, nil, fmt.Errorf("read recovery snapshot schema: %w", err)
	}
	manifest := FullRecoveryManifest{Format: recoveryFormat, SchemaVersion: schemaVersion, DatabaseSHA256: checksum(databaseBody), DatabaseBytes: int64(len(databaseBody)), CredentialProtection: secrets.Protection(), VaultIncluded: false, CreatedAt: now}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	writeEntry := func(name string, body []byte) error {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0600)
		writer, createErr := zw.CreateHeader(header)
		if createErr != nil {
			return createErr
		}
		_, writeErr := writer.Write(body)
		return writeErr
	}
	if err = writeEntry("database/hermetrix.db", databaseBody); err != nil {
		return run, nil, err
	}
	for _, ref := range refs {
		body, getErr := s.store.Blobs.Get(ref)
		if getErr != nil {
			s.failBackup(run.ID, getErr)
			return run, nil, getErr
		}
		if checksum(body) != ref {
			return run, nil, fmt.Errorf("CAS blob %s failed checksum", ref)
		}
		manifest.Blobs = append(manifest.Blobs, RecoveryBlob{Ref: ref, Bytes: int64(len(body))})
		if archive.Len()+len(body) > maxRecoveryBytes {
			return run, nil, fmt.Errorf("full recovery package exceeds 512 MiB")
		}
		if err = writeEntry("blobs/sha256/"+ref[:2]+"/"+ref[2:], body); err != nil {
			return run, nil, err
		}
	}
	manifestBody, _ := json.Marshal(manifest)
	if err = writeEntry("manifest.json", manifestBody); err == nil {
		err = zw.Close()
	}
	if err != nil {
		s.failBackup(run.ID, err)
		return run, nil, err
	}
	if archive.Len() > maxRecoveryBytes {
		err = fmt.Errorf("full recovery package exceeds 512 MiB")
		s.failBackup(run.ID, err)
		return run, nil, err
	}
	packageBody := archive.Bytes()
	ref, err := s.store.Blobs.Put(packageBody)
	if err != nil {
		return run, nil, err
	}
	completed := time.Now().UTC()
	counts := map[string]int{"blobs": len(manifest.Blobs), "database_bytes": len(databaseBody), "package_bytes": len(packageBody)}
	countsJSON, _ := json.Marshal(counts)
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO backup_runs(id,kind,state,format_version,manifest_blob_ref,checksum,counts_json,created_at,completed_at)
		VALUES(?,?,'completed',1,?,?,?,?,?)`, run.ID, run.Kind, ref, checksum(packageBody), string(countsJSON), formatTime(now), formatTime(completed))
	if err != nil {
		return run, nil, err
	}
	run.State, run.ManifestBlobRef, run.Checksum, run.Counts, run.CompletedAt = "completed", ref, checksum(packageBody), counts, &completed
	return run, packageBody, nil
}

func prepareRecoverySnapshot(snapshotPath string) error {
	db, err := sql.Open("sqlite", snapshotPath)
	if err != nil {
		return err
	}
	defer db.Close()
	// Recovery packages are outputs, not canonical application content. Keeping
	// their rows in a later snapshot would require recursively embedding every
	// prior package through backup_runs.manifest_blob_ref.
	if _, err = db.Exec(`DELETE FROM backup_runs WHERE kind='full_recovery'`); err != nil {
		return err
	}
	_, err = db.Exec(`VACUUM`)
	return err
}

func snapshotSchemaVersion(snapshotPath string) (int, error) {
	db, err := sql.Open("sqlite", snapshotPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func requiredRecoveryBlobRefs(snapshotPath string) (map[string]bool, error) {
	db, err := sql.Open("sqlite", snapshotPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tables, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for tables.Next() {
		var name string
		if err = tables.Scan(&name); err != nil {
			tables.Close()
			return nil, err
		}
		names = append(names, name)
	}
	tables.Close()
	refs := map[string]bool{}
	for _, table := range names {
		columns, queryErr := db.Query(`PRAGMA table_info("` + strings.ReplaceAll(table, `"`, `""`) + `")`)
		if queryErr != nil {
			return nil, queryErr
		}
		blobColumns := []string{}
		for columns.Next() {
			var cid, notnull, pk int
			var name, kind string
			var defaultValue any
			if scanErr := columns.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); scanErr != nil {
				columns.Close()
				return nil, scanErr
			}
			if strings.HasSuffix(name, "blob_ref") {
				blobColumns = append(blobColumns, name)
			}
		}
		columns.Close()
		for _, column := range blobColumns {
			rows, rowErr := db.Query(`SELECT "` + strings.ReplaceAll(column, `"`, `""`) + `" FROM "` + strings.ReplaceAll(table, `"`, `""`) + `" WHERE "` + strings.ReplaceAll(column, `"`, `""`) + `" IS NOT NULL`)
			if rowErr != nil {
				return nil, rowErr
			}
			for rows.Next() {
				var ref string
				if scanErr := rows.Scan(&ref); scanErr != nil {
					rows.Close()
					return nil, scanErr
				}
				if ref != "" {
					refs[ref] = true
				}
			}
			rows.Close()
		}
	}
	return refs, nil
}

func recoveryEntries(data []byte) (FullRecoveryManifest, map[string][]byte, error) {
	if len(data) == 0 || len(data) > maxRecoveryBytes {
		return FullRecoveryManifest{}, nil, fmt.Errorf("full recovery package is empty or exceeds 512 MiB")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return FullRecoveryManifest{}, nil, err
	}
	if len(zr.File) < 2 || len(zr.File) > maxRecoveryEntries {
		return FullRecoveryManifest{}, nil, fmt.Errorf("recovery archive entry count is invalid")
	}
	entries := map[string][]byte{}
	seen := map[string]bool{}
	total := int64(0)
	for _, file := range zr.File {
		name := file.Name
		clean := path.Clean(name)
		lower := strings.ToLower(clean)
		if name != clean || path.IsAbs(name) || strings.HasPrefix(clean, "../") || strings.Contains(name, "\\") || seen[lower] || file.Mode()&os.ModeSymlink != 0 || file.FileInfo().IsDir() {
			return FullRecoveryManifest{}, nil, fmt.Errorf("recovery archive contains unsafe entry %q", name)
		}
		seen[lower] = true
		total += int64(file.UncompressedSize64)
		if total > maxRecoveryBytes {
			return FullRecoveryManifest{}, nil, fmt.Errorf("recovery archive expands beyond 512 MiB")
		}
		rc, openErr := file.Open()
		if openErr != nil {
			return FullRecoveryManifest{}, nil, openErr
		}
		body, readErr := io.ReadAll(io.LimitReader(rc, int64(file.UncompressedSize64)+1))
		rc.Close()
		if readErr != nil || uint64(len(body)) != file.UncompressedSize64 {
			return FullRecoveryManifest{}, nil, fmt.Errorf("read recovery entry %q", name)
		}
		entries[name] = body
	}
	for name := range entries {
		if name == secrets.FileName || strings.HasSuffix(strings.ToLower(name), "/"+secrets.FileName) {
			return FullRecoveryManifest{}, nil, fmt.Errorf("credential vault must not be inside the database/CAS recovery package")
		}
	}
	manifestBody, ok := entries["manifest.json"]
	if !ok {
		return FullRecoveryManifest{}, nil, fmt.Errorf("recovery manifest is missing")
	}
	var manifest FullRecoveryManifest
	if json.Unmarshal(manifestBody, &manifest) != nil || manifest.Format != recoveryFormat || manifest.VaultIncluded {
		return FullRecoveryManifest{}, nil, fmt.Errorf("recovery manifest is invalid")
	}
	dbBody, ok := entries["database/hermetrix.db"]
	if !ok || checksum(dbBody) != manifest.DatabaseSHA256 || int64(len(dbBody)) != manifest.DatabaseBytes {
		return FullRecoveryManifest{}, nil, fmt.Errorf("recovery database failed integrity binding")
	}
	expected := map[string]bool{"manifest.json": true, "database/hermetrix.db": true}
	for _, blob := range manifest.Blobs {
		if !sha256Pattern.MatchString(blob.Ref) {
			return FullRecoveryManifest{}, nil, fmt.Errorf("recovery manifest contains an invalid blob reference")
		}
		name := "blobs/sha256/" + blob.Ref[:2] + "/" + blob.Ref[2:]
		body, exists := entries[name]
		if !exists || int64(len(body)) != blob.Bytes || checksum(body) != blob.Ref {
			return FullRecoveryManifest{}, nil, fmt.Errorf("recovery blob %s failed integrity binding", blob.Ref)
		}
		expected[name] = true
	}
	if len(expected) != len(entries) {
		return FullRecoveryManifest{}, nil, fmt.Errorf("recovery archive contains undeclared entries")
	}
	return manifest, entries, nil
}

func VerifyFullRecovery(data []byte) (FullRecoveryReport, error) {
	manifest, entries, err := recoveryEntries(data)
	if err != nil {
		return FullRecoveryReport{}, err
	}
	dir, err := os.MkdirTemp("", "hermetrix-recovery-verify-")
	if err != nil {
		return FullRecoveryReport{}, err
	}
	defer os.RemoveAll(dir)
	dbPath := filepath.Join(dir, "hermetrix.db")
	if err = os.WriteFile(dbPath, entries["database/hermetrix.db"], 0600); err != nil {
		return FullRecoveryReport{}, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return FullRecoveryReport{}, err
	}
	defer db.Close()
	var integrity string
	if err = db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return FullRecoveryReport{}, err
	}
	var violations int
	if err = db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		return FullRecoveryReport{}, err
	}
	report := FullRecoveryReport{Manifest: manifest, IntegrityCheck: integrity, ForeignKeyErrors: violations, VerifiedBlobCount: len(manifest.Blobs), Compatible: integrity == "ok" && violations == 0 && manifest.SchemaVersion > 0 && manifest.SchemaVersion <= store.CurrentSchemaVersion}
	if !report.Compatible {
		return report, fmt.Errorf("recovery package integrity or schema compatibility failed")
	}
	return report, nil
}

func RestoreFullRecoveryTo(data []byte, targetRoot string) (FullRecoveryReport, error) {
	report, err := VerifyFullRecovery(data)
	if err != nil {
		return report, err
	}
	_, entries, err := recoveryEntries(data)
	if err != nil {
		return report, err
	}
	target, err := filepath.Abs(targetRoot)
	if err != nil {
		return report, err
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		if statErr == nil {
			return report, fmt.Errorf("recovery destination already exists")
		}
		return report, statErr
	}
	parent := filepath.Dir(target)
	if err = os.MkdirAll(parent, 0700); err != nil {
		return report, err
	}
	stage, err := os.MkdirTemp(parent, ".hermetrix-recovery-")
	if err != nil {
		return report, err
	}
	defer os.RemoveAll(stage)
	if err = os.WriteFile(filepath.Join(stage, "hermetrix.db"), entries["database/hermetrix.db"], 0600); err != nil {
		return report, err
	}
	for name, body := range entries {
		if !strings.HasPrefix(name, "blobs/sha256/") {
			continue
		}
		relative := strings.TrimPrefix(name, "blobs/sha256/")
		targetPath := filepath.Join(stage, "blobs", "sha256", filepath.FromSlash(relative))
		if err = os.MkdirAll(filepath.Dir(targetPath), 0700); err == nil {
			err = os.WriteFile(targetPath, body, 0600)
		}
		if err != nil {
			return report, err
		}
	}
	if err = os.Rename(stage, target); err != nil {
		return report, err
	}
	return report, nil
}

// CaptureProtectedVault returns the already protected vault file separately.
// On Windows DPAPI CurrentUser ciphertext is only a same-account/machine
// recovery aid; cross-machine restore must reenroll credentials.
func (s *Service) CaptureProtectedVault() (VaultCapture, error) {
	body, err := os.ReadFile(filepath.Join(s.store.Root, secrets.FileName))
	if errors.Is(err, os.ErrNotExist) {
		return VaultCapture{Protection: secrets.Protection(), SameMachineOnly: true}, nil
	}
	if err != nil {
		return VaultCapture{}, err
	}
	return VaultCapture{Protection: secrets.Protection(), SameMachineOnly: true, SHA256: checksum(body), Data: body}, nil
}

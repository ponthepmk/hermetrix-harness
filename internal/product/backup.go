package product

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/skills"
)

const (
	backupFormatVersion = 1
	maxBackupBytes      = 256 << 20
)

var (
	backupTables = []string{
		"skills", "skill_versions", "skill_candidates", "skill_events", "skill_activations", "skill_archives",
		"skill_relations", "learning_reviews", "curator_runs", "provider_profiles", "agent_sessions", "agent_events",
		"context_snapshots", "step_bindings", "tool_approvals", "mcp_servers", "mcp_tools", "skill_replay_runs",
		"skill_replay_cases", "candidate_capability_reviews", "context_eval_cases", "context_eval_runs",
		"model_qualification_runs", "projects", "artifacts", "background_jobs", "settings", "memories", "backup_runs",
		"curator_findings", "maintenance_schedules", "gc_runs",
		"learning_trigger_outbox", "skill_authority_policy", "skill_authority_actions",
		"terminal_sessions", "browser_tabs", "agent_teams", "agent_team_members", "agent_team_runs", "agent_team_tasks",
	}
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type backupPayload struct {
	Tables map[string][]map[string]any `json:"tables"`
	Blobs  map[string]string           `json:"blobs"`
}

type backupEnvelope struct {
	Format    int           `json:"format"`
	CreatedAt time.Time     `json:"created_at"`
	Payload   backupPayload `json:"payload"`
	Checksum  string        `json:"payload_checksum"`
}

func (s *Service) ExportBackup(ctx context.Context, actor string) (BackupRun, []byte, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return BackupRun{}, nil, fmt.Errorf("backup actor is required")
	}
	now := time.Now().UTC()
	run := BackupRun{ID: identity.New("backup"), Kind: "export", State: "running", FormatVersion: backupFormatVersion,
		Counts: map[string]int{}, CreatedAt: now}
	if _, err := s.store.DB.ExecContext(ctx, `INSERT INTO backup_runs(id,kind,state,format_version,counts_json,created_at)
      VALUES(?,'export','running',?,'{}',?)`, run.ID, backupFormatVersion, formatTime(now)); err != nil {
		return BackupRun{}, nil, err
	}
	payload, counts, err := s.buildBackupPayload(ctx)
	if err != nil {
		s.failBackup(run.ID, err)
		return run, nil, err
	}
	payloadData, err := json.Marshal(payload)
	if err != nil {
		s.failBackup(run.ID, err)
		return run, nil, err
	}
	envelope := backupEnvelope{Format: backupFormatVersion, CreatedAt: now, Payload: payload, Checksum: checksum(payloadData)}
	encoded, err := json.Marshal(envelope)
	if err != nil || len(encoded) > maxBackupBytes {
		if err == nil {
			err = fmt.Errorf("backup exceeds %d bytes", maxBackupBytes)
		}
		s.failBackup(run.ID, err)
		return run, nil, err
	}
	ref, err := s.store.Blobs.Put(encoded)
	if err != nil {
		s.failBackup(run.ID, err)
		return run, nil, err
	}
	completed := time.Now().UTC()
	countsJSON, _ := json.Marshal(counts)
	if _, err := s.store.DB.ExecContext(ctx, `UPDATE backup_runs SET state='completed',manifest_blob_ref=?,checksum=?,
      counts_json=?,completed_at=? WHERE id=?`, ref, checksum(encoded), string(countsJSON), formatTime(completed), run.ID); err != nil {
		return run, nil, err
	}
	run.State, run.ManifestBlobRef, run.Checksum, run.Counts, run.CompletedAt = "completed", ref, checksum(encoded), counts, &completed
	return run, encoded, nil
}

func (s *Service) buildBackupPayload(ctx context.Context) (backupPayload, map[string]int, error) {
	payload := backupPayload{Tables: map[string][]map[string]any{}, Blobs: map[string]string{}}
	counts := map[string]int{}
	refs := map[string]bool{}
	for _, table := range backupTables {
		items, err := s.exportTable(ctx, table)
		if err != nil {
			return payload, nil, fmt.Errorf("export %s: %w", table, err)
		}
		payload.Tables[table] = items
		counts[table] = len(items)
		collectBlobRefs(items, refs)
	}
	ordered := make([]string, 0, len(refs))
	for ref := range refs {
		ordered = append(ordered, ref)
	}
	sort.Strings(ordered)
	bytesTotal := 0
	for _, ref := range ordered {
		if !s.store.Blobs.Exists(ref) {
			continue
		}
		data, err := s.store.Blobs.Get(ref)
		if err != nil {
			return payload, nil, err
		}
		bytesTotal += len(data)
		if bytesTotal > maxBackupBytes {
			return payload, nil, fmt.Errorf("referenced blobs exceed backup safety limit")
		}
		payload.Blobs[ref] = base64.StdEncoding.EncodeToString(data)
	}
	counts["blobs"] = len(payload.Blobs)
	counts["blob_bytes"] = bytesTotal
	return payload, counts, nil
}

func (s *Service) exportTable(ctx context.Context, table string) ([]map[string]any, error) {
	allowed := false
	for _, candidate := range backupTables {
		allowed = allowed || table == candidate
	}
	if !allowed {
		return nil, fmt.Errorf("table is not in backup allowlist")
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT * FROM `+table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		item := map[string]any{}
		for index, column := range columns {
			if raw, ok := values[index].([]byte); ok {
				values[index] = string(raw)
			}
			item[column] = values[index]
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) PreviewImport(ctx context.Context, data []byte, actor string) (ImportPreview, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" || len(data) == 0 || len(data) > maxBackupBytes {
		return ImportPreview{}, fmt.Errorf("actor and backup data up to 256 MiB are required")
	}
	envelope, err := validateBackupEnvelope(data)
	if err != nil {
		return ImportPreview{}, err
	}
	conflicts, err := s.countSkillConflicts(ctx, envelope.Payload)
	if err != nil {
		return ImportPreview{}, err
	}
	ref, err := s.store.Blobs.Put(data)
	if err != nil {
		return ImportPreview{}, err
	}
	bytesTotal := 0
	for _, encoded := range envelope.Payload.Blobs {
		decoded, _ := base64.StdEncoding.DecodeString(encoded)
		bytesTotal += len(decoded)
	}
	now := time.Now().UTC()
	counts := map[string]int{"skills": len(envelope.Payload.Tables["skills"]), "blobs": len(envelope.Payload.Blobs),
		"blob_bytes": bytesTotal, "skill_conflicts": conflicts}
	countsJSON, _ := json.Marshal(counts)
	run := BackupRun{ID: identity.New("backup"), Kind: "import_preview", State: "awaiting_apply",
		FormatVersion: envelope.Format, ManifestBlobRef: ref, Checksum: checksum(data), Counts: counts, CreatedAt: now}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO backup_runs(id,kind,state,format_version,manifest_blob_ref,checksum,
    counts_json,created_at) VALUES(?,'import_preview','awaiting_apply',?,?,?,?,?)`, run.ID, run.FormatVersion, ref,
		run.Checksum, string(countsJSON), formatTime(now))
	return ImportPreview{BackupRun: run, SkillConflicts: conflicts, BlobCount: len(envelope.Payload.Blobs), BlobBytes: bytesTotal}, err
}

func (s *Service) ApplyImport(ctx context.Context, previewID, actor string) (ImportResult, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ImportResult{}, fmt.Errorf("import actor is required")
	}
	run, err := s.GetBackup(ctx, previewID)
	if err != nil {
		return ImportResult{}, err
	}
	if run.Kind != "import_preview" || run.State != "awaiting_apply" {
		return ImportResult{}, fmt.Errorf("backup preview is not awaiting apply")
	}
	data, err := s.store.Blobs.Get(run.ManifestBlobRef)
	if err != nil || checksum(data) != run.Checksum {
		return ImportResult{}, fmt.Errorf("stored import preview failed integrity check")
	}
	envelope, err := validateBackupEnvelope(data)
	if err != nil {
		return ImportResult{}, err
	}
	for ref, encoded := range envelope.Payload.Blobs {
		decoded, _ := base64.StdEncoding.DecodeString(encoded)
		stored, err := s.store.Blobs.Put(decoded)
		if err != nil || stored != ref {
			return ImportResult{}, fmt.Errorf("restore blob %s failed integrity check", ref)
		}
	}
	versions := rowsByID(envelope.Payload.Tables["skill_versions"])
	candidateIDs := []string{}
	conflicts := 0
	for _, item := range envelope.Payload.Tables["skills"] {
		name, _ := item["canonical_name"].(string)
		versionID, _ := item["current_version_id"].(string)
		version := versions[versionID]
		ref, _ := version["package_blob_ref"].(string)
		encoded, ok := envelope.Payload.Blobs[ref]
		if name == "" || !ok {
			continue
		}
		packageData, _ := base64.StdEncoding.DecodeString(encoded)
		pkg, err := skills.ParsePackage(packageData)
		if err != nil {
			return ImportResult{}, fmt.Errorf("parse imported skill %s: %w", name, err)
		}
		scopeKind, _ := item["scope_kind"].(string)
		scopeRef, _ := item["scope_ref"].(string)
		var existing int
		_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills WHERE scope_kind=? AND scope_ref=? AND canonical_name=?`,
			scopeKind, scopeRef, name).Scan(&existing)
		if existing > 0 {
			conflicts++
		}
		candidate, err := s.skills.CreateCandidate(ctx, skills.CreateCandidateInput{CanonicalName: name, ScopeKind: scopeKind,
			ScopeRef: scopeRef, Origin: "imported", Owner: "user", ChangeKind: "create", CreatedBy: actor,
			TriggerKind: "backup_import", Reason: importReason(previewID, existing > 0),
			EvidenceRefs: []string{"backup:" + previewID, "source_version:" + versionID}, Markdown: pkg.Markdown(),
			Files: packageSupportingFiles(pkg)})
		if err != nil {
			return ImportResult{}, err
		}
		candidateIDs = append(candidateIDs, candidate.ID)
	}
	completed := time.Now().UTC()
	counts := run.Counts
	counts["candidates_created"] = len(candidateIDs)
	counts["skill_conflicts"] = conflicts
	countsJSON, _ := json.Marshal(counts)
	if _, err := s.store.DB.ExecContext(ctx, `UPDATE backup_runs SET state='imported',counts_json=?,completed_at=?
      WHERE id=? AND state='awaiting_apply'`, string(countsJSON), formatTime(completed), previewID); err != nil {
		return ImportResult{}, err
	}
	run.State, run.Counts, run.CompletedAt = "imported", counts, &completed
	return ImportResult{BackupRun: run, CandidateIDs: candidateIDs, Conflicts: conflicts,
		NotRestored: tablesNotRestored(envelope.Payload.Tables)}, nil
}

func validateBackupEnvelope(data []byte) (backupEnvelope, error) {
	var envelope backupEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Format != backupFormatVersion {
		return envelope, fmt.Errorf("unsupported or malformed Hermetrix backup")
	}
	payload, err := json.Marshal(envelope.Payload)
	if err != nil || checksum(payload) != envelope.Checksum {
		return envelope, fmt.Errorf("backup payload checksum mismatch")
	}
	for ref, encoded := range envelope.Payload.Blobs {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || checksum(decoded) != ref {
			return envelope, fmt.Errorf("backup blob %s failed integrity check", ref)
		}
	}
	return envelope, nil
}

func (s *Service) ListBackups(ctx context.Context) ([]BackupRun, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,kind,state,format_version,manifest_blob_ref,checksum,counts_json,
    error,created_at,completed_at FROM backup_runs ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BackupRun{}
	for rows.Next() {
		item, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetBackup(ctx context.Context, id string) (BackupRun, error) {
	return scanBackup(s.store.DB.QueryRowContext(ctx, `SELECT id,kind,state,format_version,manifest_blob_ref,checksum,
    counts_json,error,created_at,completed_at FROM backup_runs WHERE id=?`, id))
}

func (s *Service) BackupData(ctx context.Context, id string) (BackupRun, []byte, error) {
	run, err := s.GetBackup(ctx, id)
	if err != nil {
		return BackupRun{}, nil, err
	}
	data, err := s.store.Blobs.Get(run.ManifestBlobRef)
	if err != nil || checksum(data) != run.Checksum {
		return run, nil, fmt.Errorf("backup failed integrity check")
	}
	return run, data, nil
}

func (s *Service) countSkillConflicts(ctx context.Context, payload backupPayload) (int, error) {
	count := 0
	for _, item := range payload.Tables["skills"] {
		var existing int
		if err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills WHERE scope_kind=? AND scope_ref=? AND canonical_name=?`,
			item["scope_kind"], item["scope_ref"], item["canonical_name"]).Scan(&existing); err != nil {
			return 0, err
		}
		if existing > 0 {
			count++
		}
	}
	return count, nil
}

func collectBlobRefs(value any, refs map[string]bool) {
	switch typed := value.(type) {
	case []map[string]any:
		for _, nested := range typed {
			collectBlobRefs(nested, refs)
		}
	case map[string]any:
		for _, nested := range typed {
			collectBlobRefs(nested, refs)
		}
	case []any:
		for _, nested := range typed {
			collectBlobRefs(nested, refs)
		}
	case string:
		if sha256Pattern.MatchString(typed) {
			refs[typed] = true
			return
		}
		if strings.HasPrefix(strings.TrimSpace(typed), "{") || strings.HasPrefix(strings.TrimSpace(typed), "[") {
			var nested any
			if json.Unmarshal([]byte(typed), &nested) == nil {
				collectBlobRefs(nested, refs)
			}
		}
	}
}

func rowsByID(items []map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, item := range items {
		if id, ok := item["id"].(string); ok {
			out[id] = item
		}
	}
	return out
}

func packageSupportingFiles(pkg skills.Package) []skills.File {
	files := []skills.File{}
	for _, file := range pkg.Files {
		if file.Path != "SKILL.md" {
			files = append(files, file)
		}
	}
	return files
}

func importReason(previewID string, conflict bool) string {
	reason := "restored from verified backup " + previewID + " as a reviewable candidate"
	if conflict {
		reason += "; same-scope active name conflict requires an explicit resolution before promotion"
	}
	return reason
}

func (s *Service) failBackup(id string, cause error) {
	_, _ = s.store.DB.Exec(`UPDATE backup_runs SET state='failed',error=?,completed_at=? WHERE id=?`, cause.Error(),
		formatTime(time.Now().UTC()), id)
}

type backupScanner interface{ Scan(...any) error }

func scanBackup(row backupScanner) (BackupRun, error) {
	var item BackupRun
	var countsJSON, created string
	var completed sql.NullString
	if err := row.Scan(&item.ID, &item.Kind, &item.State, &item.FormatVersion, &item.ManifestBlobRef, &item.Checksum,
		&countsJSON, &item.Error, &created, &completed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BackupRun{}, sql.ErrNoRows
		}
		return BackupRun{}, err
	}
	_ = json.Unmarshal([]byte(countsJSON), &item.Counts)
	item.CreatedAt, _ = parseTime(created)
	if completed.Valid {
		value, _ := parseTime(completed.String)
		item.CompletedAt = &value
	}
	return item, nil
}

// restoredTables are the only tables ApplyImport reads. Everything else the
// export wrote is carried in the file and ignored.
var restoredTables = map[string]bool{"skills": true, "skill_versions": true}

// tablesNotRestored reports what the file held that the import did not use, so
// the caller can see the gap instead of inferring it from missing data later.
func tablesNotRestored(tables map[string][]map[string]any) map[string]int {
	out := map[string]int{}
	for name, rows := range tables {
		if restoredTables[name] || len(rows) == 0 {
			continue
		}
		out[name] = len(rows)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

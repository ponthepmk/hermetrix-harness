package product

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
)

const (
	shareFormat             = "hermetrix.project-share.v1"
	maxShareCompressedBytes = 512 << 20
	maxShareExpandedBytes   = int64(2 << 30)
	maxShareEntryBytes      = int64(256 << 20)
	maxShareEntries         = 10_000
	sharePreviewExpiry      = 30 * time.Minute
)

type shareManifest struct {
	Format              string                  `json:"format"`
	PackageKind         string                  `json:"package_kind"`
	FormatVersion       int                     `json:"format_version"`
	SourceApplication   string                  `json:"source_application"`
	SourceSchemaVersion int                     `json:"source_schema_version"`
	ExportID            string                  `json:"export_id"`
	ProjectName         string                  `json:"project_name"`
	SourceProjectID     string                  `json:"source_project_id,omitempty"`
	Entries             []ShareManifestEntry    `json:"entries"`
	Tasks               []shareTaskProjection   `json:"tasks,omitempty"`
	Memories            []shareMemoryProjection `json:"memories,omitempty"`
	Dependencies        []shareDependencyEdge   `json:"dependency_edges,omitempty"`
	Omitted             []string                `json:"omitted,omitempty"`
	Metadata            []string                `json:"remaining_metadata"`
}

type shareCriterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type shareTaskProjection struct {
	SourceID        string           `json:"source_id"`
	Title           string           `json:"title"`
	Objective       string           `json:"objective"`
	OriginalRequest string           `json:"original_request"`
	Constraints     []string         `json:"constraints"`
	Unknowns        []string         `json:"unknowns"`
	Criteria        []shareCriterion `json:"criteria"`
	SharingRevision int              `json:"sharing_revision"`
}

type shareMemoryProjection struct {
	SourceID        string `json:"source_id"`
	MemoryKind      string `json:"memory_kind"`
	Content         string `json:"content"`
	Source          string `json:"source"`
	SharingRevision int    `json:"sharing_revision"`
}

type shareDependencyEdge struct {
	SourceID     string `json:"source_id"`
	ResultID     string `json:"result_id"`
	RelationKind string `json:"relation_kind"`
	SourceHash   string `json:"source_hash"`
	ResultHash   string `json:"result_hash"`
}

func (s *Service) CreateSharePreview(ctx context.Context, input SharePreviewInput) (SharePreview, error) {
	input.Actor = strings.TrimSpace(input.Actor)
	if input.Actor == "" || strings.TrimSpace(input.ProjectID) == "" ||
		(len(input.Paths) == 0 && len(input.ArtifactIDs) == 0 && len(input.TaskIDs) == 0 && len(input.SkillIDs) == 0 && len(input.MemoryIDs) == 0) {
		return SharePreview{}, fmt.Errorf("project, actor and at least one explicit selection are required")
	}
	project, err := s.GetProject(ctx, input.ProjectID)
	if err != nil {
		return SharePreview{}, err
	}
	if project.Visibility != "project_shared" || project.ExportPolicy != "explicit_selection" {
		return SharePreview{}, fmt.Errorf("project is not eligible for explicit sharing")
	}
	root, err := requireRoot(project)
	if err != nil {
		return SharePreview{}, err
	}
	entries, omitted := []ShareManifestEntry{}, []string{}
	seenPaths := map[string]bool{}
	for _, relative := range input.Paths {
		relative, err = safeSharePath(relative)
		if err != nil {
			return SharePreview{}, err
		}
		if sharePathExcluded(relative) {
			omitted = append(omitted, relative+": excluded by secret/VCS policy")
			continue
		}
		if seenPaths[strings.ToLower(relative)] {
			return SharePreview{}, fmt.Errorf("duplicate or case-colliding selected path %q", relative)
		}
		seenPaths[strings.ToLower(relative)] = true
		absolute, err := resolveInside(root, filepath.FromSlash(relative), false)
		if err != nil {
			return SharePreview{}, err
		}
		info, err := os.Lstat(absolute)
		if err != nil {
			return SharePreview{}, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return SharePreview{}, fmt.Errorf("share path %q must be a regular non-symlink file", relative)
		}
		if info.Size() > maxArtifactBytes {
			return SharePreview{}, fmt.Errorf("share file %q exceeds 16 MiB", relative)
		}
		body, err := os.ReadFile(absolute)
		if err != nil {
			return SharePreview{}, err
		}
		entries = append(entries, ShareManifestEntry{Kind: "file", Path: relative, SHA256: checksum(body), Bytes: int64(len(body))})
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return SharePreview{}, err
	}
	artifactQueue := shareCleanStrings(input.ArtifactIDs)
	seenArtifacts := map[string]bool{}
	dependencies := []shareDependencyEdge{}
	seenDependencies := map[string]bool{}
	for len(artifactQueue) > 0 {
		id := artifactQueue[0]
		artifactQueue = artifactQueue[1:]
		if seenArtifacts[id] {
			continue
		}
		seenArtifacts[id] = true
		var entry ShareManifestEntry
		var visibility, policy, artifactOwner, artifactProject string
		err = s.store.DB.QueryRowContext(ctx, `SELECT id,name,mime_type,checksum,byte_size,sharing_revision,visibility,export_policy,
			owner_principal_id,COALESCE(project_id,'') FROM artifacts WHERE id=?`, id).Scan(&entry.ArtifactID, &entry.Name,
			&entry.MIMEType, &entry.SHA256, &entry.Bytes, &entry.SharingRevision, &visibility, &policy, &artifactOwner, &artifactProject)
		if err != nil || artifactOwner != ownerID || artifactProject != project.ID {
			return SharePreview{}, fmt.Errorf("selected artifact %q is unavailable", id)
		}
		if visibility != "project_shared" || policy != "explicit_selection" {
			return SharePreview{}, fmt.Errorf("artifact %q or a required derivation source is private", id)
		}
		entry.Kind = "artifact"
		entries = append(entries, entry)
		rows, queryErr := s.store.DB.QueryContext(ctx, `SELECT source_artifact_id,relation_kind,source_hash,result_hash FROM artifact_derivations WHERE result_artifact_id=? ORDER BY source_artifact_id`, id)
		if queryErr != nil {
			return SharePreview{}, queryErr
		}
		for rows.Next() {
			edge := shareDependencyEdge{ResultID: id}
			if scanErr := rows.Scan(&edge.SourceID, &edge.RelationKind, &edge.SourceHash, &edge.ResultHash); scanErr != nil {
				rows.Close()
				return SharePreview{}, scanErr
			}
			key := edge.SourceID + "\x00" + edge.ResultID + "\x00" + edge.RelationKind
			if !seenDependencies[key] {
				seenDependencies[key] = true
				dependencies = append(dependencies, edge)
			}
			artifactQueue = append(artifactQueue, edge.SourceID)
		}
		rows.Close()
	}
	tasks := []shareTaskProjection{}
	for _, id := range shareCleanStrings(input.TaskIDs) {
		var task shareTaskProjection
		var constraintsJSON, unknownsJSON, criteriaJSON, visibility, policy, taskOwner, taskProject string
		err = s.store.DB.QueryRowContext(ctx, `SELECT t.id,t.title,t.objective,t.original_request,t.sharing_revision,t.visibility,t.export_policy,
			t.owner_principal_id,COALESCE(t.project_id,''),r.constraints_json,r.unknowns_json,r.criteria_json
			FROM durable_tasks t JOIN task_requirement_revisions r ON r.task_id=t.id AND r.revision=t.active_requirement_revision
			WHERE t.id=?`, id).Scan(&task.SourceID, &task.Title, &task.Objective, &task.OriginalRequest, &task.SharingRevision,
			&visibility, &policy, &taskOwner, &taskProject, &constraintsJSON, &unknownsJSON, &criteriaJSON)
		if err != nil || taskOwner != ownerID || taskProject != project.ID || visibility != "project_shared" || policy != "explicit_selection" {
			return SharePreview{}, fmt.Errorf("selected task %q is unavailable or private", id)
		}
		if json.Unmarshal([]byte(constraintsJSON), &task.Constraints) != nil || json.Unmarshal([]byte(unknownsJSON), &task.Unknowns) != nil || json.Unmarshal([]byte(criteriaJSON), &task.Criteria) != nil {
			return SharePreview{}, fmt.Errorf("selected task %q has invalid requirement data", id)
		}
		tasks = append(tasks, task)
	}
	for _, id := range shareCleanStrings(input.SkillIDs) {
		skill, getErr := s.skills.GetSkill(ctx, id)
		if getErr != nil || skill.ScopeKind != "project" || skill.ScopeRef != project.ID || skill.Visibility != "project_shared" || skill.ExportPolicy != "explicit_selection" {
			return SharePreview{}, fmt.Errorf("selected skill %q is unavailable or private", id)
		}
		version, getErr := s.skills.GetVersion(ctx, skill.CurrentVersionID)
		if getErr != nil {
			return SharePreview{}, getErr
		}
		body, getErr := s.store.Blobs.Get(version.PackageBlobRef)
		if getErr != nil || checksum(body) != version.ContentHash {
			return SharePreview{}, fmt.Errorf("selected skill %q package failed integrity check", id)
		}
		entries = append(entries, ShareManifestEntry{Kind: "skill", ObjectID: skill.ID, Name: skill.CanonicalName,
			MIMEType: "application/vnd.hermetrix.skill+zip", SHA256: version.ContentHash, Bytes: int64(len(body)), SharingRevision: skill.SharingRevision})
	}
	memories := []shareMemoryProjection{}
	for _, id := range shareCleanStrings(input.MemoryIDs) {
		var memory shareMemoryProjection
		var visibility, policy, memoryOwner, scopeKind, scopeRef, state string
		err = s.store.DB.QueryRowContext(ctx, `SELECT id,memory_kind,content,source,sharing_revision,visibility,export_policy,
			owner_principal_id,scope_kind,scope_ref,state FROM memories WHERE id=?`, id).Scan(&memory.SourceID, &memory.MemoryKind,
			&memory.Content, &memory.Source, &memory.SharingRevision, &visibility, &policy, &memoryOwner, &scopeKind, &scopeRef, &state)
		if err != nil || memoryOwner != ownerID || scopeKind != "project" || scopeRef != project.ID || state != "active" || visibility != "project_shared" || policy != "explicit_selection" {
			return SharePreview{}, fmt.Errorf("selected memory %q is unavailable or private", id)
		}
		memories = append(memories, memory)
	}
	sort.Slice(entries, func(i, j int) bool { return shareEntryKey(entries[i]) < shareEntryKey(entries[j]) })
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].SourceID < tasks[j].SourceID })
	sort.Slice(memories, func(i, j int) bool { return memories[i].SourceID < memories[j].SourceID })
	sort.Slice(dependencies, func(i, j int) bool {
		left := dependencies[i].SourceID + "\x00" + dependencies[i].ResultID + "\x00" + dependencies[i].RelationKind
		right := dependencies[j].SourceID + "\x00" + dependencies[j].ResultID + "\x00" + dependencies[j].RelationKind
		return left < right
	})
	sort.Strings(omitted)
	previewID := identity.New("sharepreview")
	manifest := shareManifest{Format: shareFormat, PackageKind: "project_share", FormatVersion: 1,
		SourceApplication: "hermetrix-harness", SourceSchemaVersion: store.CurrentSchemaVersion, ExportID: previewID,
		ProjectName: project.Name, SourceProjectID: project.ID,
		Entries: entries, Tasks: tasks, Memories: memories, Dependencies: dependencies, Omitted: omitted,
		Metadata: []string{"file paths", "object names", "MIME types", "content hashes", "task requirements", "memory source labels"}}
	manifestBody, _ := json.Marshal(manifest)
	if len(entries)+len(tasks)+len(memories)+1 > maxShareEntries || int64(len(manifestBody)) > maxShareEntryBytes {
		return SharePreview{}, fmt.Errorf("share preview exceeds the 10,000 object or 256 MiB manifest limit")
	}
	totalBytes := int64(len(manifestBody))
	for _, entry := range entries {
		totalBytes += entry.Bytes
	}
	if totalBytes > maxShareExpandedBytes {
		return SharePreview{}, fmt.Errorf("share preview expands beyond 2 GiB")
	}
	manifestDigest := checksum(manifestBody)
	revisionBody, _ := json.Marshal(struct {
		ProjectRevision int
		Entries         []ShareManifestEntry
	}{project.SharingRevision, entries})
	revisionDigest := checksum(revisionBody)
	ref, err := s.store.Blobs.Put(manifestBody)
	if err != nil {
		return SharePreview{}, err
	}
	now, expires := time.Now().UTC(), time.Now().UTC().Add(sharePreviewExpiry)
	previewEntries := append([]ShareManifestEntry(nil), entries...)
	for _, task := range tasks {
		body, _ := json.Marshal(task)
		previewEntries = append(previewEntries, ShareManifestEntry{Kind: "task", ObjectID: task.SourceID, Name: task.Title,
			SHA256: checksum(body), Bytes: int64(len(body)), SharingRevision: task.SharingRevision})
	}
	for _, memory := range memories {
		body, _ := json.Marshal(memory)
		previewEntries = append(previewEntries, ShareManifestEntry{Kind: "memory", ObjectID: memory.SourceID, Name: memory.MemoryKind,
			SHA256: checksum(body), Bytes: int64(len(body)), SharingRevision: memory.SharingRevision})
	}
	sort.Slice(previewEntries, func(i, j int) bool { return shareEntryKey(previewEntries[i]) < shareEntryKey(previewEntries[j]) })
	preview := SharePreview{ID: previewID, ProjectID: project.ID, ManifestDigest: manifestDigest,
		SourceRevisionDigest: revisionDigest, Entries: previewEntries, Omitted: omitted, RemainingMetadata: manifest.Metadata,
		State: "ready", CreatedAt: now, ExpiresAt: expires}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO share_previews(id,owner_principal_id,project_id,actor,manifest_blob_ref,
		manifest_digest,source_revision_digest,state,created_at,expires_at) VALUES(?,?,?,?,?,?,?,'ready',?,?)`, preview.ID,
		ownerID, project.ID, input.Actor, ref, manifestDigest, revisionDigest, formatTime(now), formatTime(expires))
	return preview, err
}

func safeSharePath(value string) (string, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	cleaned := path.Clean(value)
	if value == "" || cleaned == "." || cleaned != value || path.IsAbs(value) || strings.HasPrefix(cleaned, "../") || strings.ContainsRune(value, 0) {
		return "", fmt.Errorf("invalid share path %q", value)
	}
	return cleaned, nil
}

func sharePathExcluded(value string) bool {
	lower := strings.ToLower("/" + value + "/")
	base := strings.ToLower(path.Base(value))
	if strings.Contains(lower, "/.git/") || strings.Contains(lower, "/.hg/") || strings.Contains(lower, "/.svn/") {
		return true
	}
	if base == ".env" || strings.HasPrefix(base, ".env.") || strings.Contains(base, "credential") || strings.Contains(base, "secret") || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") {
		return true
	}
	return false
}

func shareEntryKey(item ShareManifestEntry) string {
	if item.Kind == "file" {
		return "file:" + item.Path
	}
	if item.Kind == "skill" {
		return "skill:" + item.ObjectID
	}
	if item.Kind == "artifact" {
		return "artifact:" + item.ArtifactID
	}
	return item.Kind + ":" + item.ObjectID
}

func shareCleanStrings(items []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}

func canonicalProjectionDigest(value any) string {
	body, _ := json.Marshal(value)
	return checksum(body)
}

func (s *Service) shareTaskProjection(ctx context.Context, ownerID, projectID, id string) (shareTaskProjection, error) {
	var task shareTaskProjection
	var constraintsJSON, unknownsJSON, criteriaJSON, visibility, policy, taskOwner, taskProject string
	err := s.store.DB.QueryRowContext(ctx, `SELECT t.id,t.title,t.objective,t.original_request,t.sharing_revision,t.visibility,t.export_policy,
		t.owner_principal_id,COALESCE(t.project_id,''),r.constraints_json,r.unknowns_json,r.criteria_json
		FROM durable_tasks t JOIN task_requirement_revisions r ON r.task_id=t.id AND r.revision=t.active_requirement_revision WHERE t.id=?`, id).
		Scan(&task.SourceID, &task.Title, &task.Objective, &task.OriginalRequest, &task.SharingRevision, &visibility, &policy,
			&taskOwner, &taskProject, &constraintsJSON, &unknownsJSON, &criteriaJSON)
	if err != nil || taskOwner != ownerID || taskProject != projectID || visibility != "project_shared" || policy != "explicit_selection" {
		return shareTaskProjection{}, fmt.Errorf("task is unavailable or private")
	}
	if json.Unmarshal([]byte(constraintsJSON), &task.Constraints) != nil || json.Unmarshal([]byte(unknownsJSON), &task.Unknowns) != nil || json.Unmarshal([]byte(criteriaJSON), &task.Criteria) != nil {
		return shareTaskProjection{}, fmt.Errorf("task requirement projection is invalid")
	}
	return task, nil
}

func (s *Service) shareMemoryProjection(ctx context.Context, ownerID, projectID, id string) (shareMemoryProjection, error) {
	var memory shareMemoryProjection
	var visibility, policy, memoryOwner, scopeKind, scopeRef, state string
	err := s.store.DB.QueryRowContext(ctx, `SELECT id,memory_kind,content,source,sharing_revision,visibility,export_policy,
		owner_principal_id,scope_kind,scope_ref,state FROM memories WHERE id=?`, id).Scan(&memory.SourceID, &memory.MemoryKind,
		&memory.Content, &memory.Source, &memory.SharingRevision, &visibility, &policy, &memoryOwner, &scopeKind, &scopeRef, &state)
	if err != nil || memoryOwner != ownerID || scopeKind != "project" || scopeRef != projectID || state != "active" || visibility != "project_shared" || policy != "explicit_selection" {
		return shareMemoryProjection{}, fmt.Errorf("memory is unavailable or private")
	}
	return memory, nil
}

func (s *Service) ExportShare(ctx context.Context, input ShareExportInput) (ShareExport, error) {
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return ShareExport{}, err
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.PreviewID == "" || input.ManifestDigest == "" || input.IdempotencyKey == "" {
		return ShareExport{}, fmt.Errorf("preview, digest and idempotency key are required")
	}
	payloadHash := checksum([]byte(input.ProjectID + "\x00" + input.PreviewID + "\x00" + input.ManifestDigest))
	var existingID, existingPayload string
	err = s.store.DB.QueryRowContext(ctx, `SELECT id,payload_hash FROM share_export_jobs WHERE owner_principal_id=? AND idempotency_key=?`, ownerID, input.IdempotencyKey).Scan(&existingID, &existingPayload)
	if err == nil {
		if existingPayload != payloadHash {
			return ShareExport{}, ErrIdempotencyConflict
		}
		return s.GetShareExport(ctx, existingID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ShareExport{}, err
	}
	var projectID, manifestRef, storedDigest, state, expiresText string
	err = s.store.DB.QueryRowContext(ctx, `SELECT project_id,manifest_blob_ref,manifest_digest,state,expires_at FROM share_previews
		WHERE id=? AND owner_principal_id=?`, input.PreviewID, ownerID).Scan(&projectID, &manifestRef, &storedDigest, &state, &expiresText)
	if err != nil {
		return ShareExport{}, err
	}
	expires, _ := time.Parse(time.RFC3339Nano, expiresText)
	if projectID != input.ProjectID || storedDigest != input.ManifestDigest || state != "ready" || !expires.After(time.Now().UTC()) {
		return ShareExport{}, fmt.Errorf("share preview is stale, expired, or does not match the requested digest")
	}
	body, err := s.store.Blobs.Get(manifestRef)
	if err != nil || checksum(body) != storedDigest {
		return ShareExport{}, fmt.Errorf("share preview manifest failed integrity check")
	}
	var manifest shareManifest
	if err = json.Unmarshal(body, &manifest); err != nil || manifest.Format != shareFormat {
		return ShareExport{}, fmt.Errorf("invalid share preview manifest")
	}
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return ShareExport{}, err
	}
	if project.Visibility != "project_shared" || project.ExportPolicy != "explicit_selection" {
		return ShareExport{}, fmt.Errorf("project sharing policy changed after preview")
	}
	root, err := requireRoot(project)
	if err != nil {
		return ShareExport{}, err
	}
	contents := map[string][]byte{}
	for _, entry := range manifest.Entries {
		switch entry.Kind {
		case "file":
			absolute, pathErr := resolveInside(root, filepath.FromSlash(entry.Path), false)
			if pathErr != nil {
				return ShareExport{}, pathErr
			}
			info, statErr := os.Lstat(absolute)
			if statErr != nil || info.Mode()&os.ModeSymlink != 0 {
				return ShareExport{}, fmt.Errorf("share source %q changed after preview", entry.Path)
			}
			data, readErr := os.ReadFile(absolute)
			if readErr != nil || checksum(data) != entry.SHA256 {
				return ShareExport{}, fmt.Errorf("share source %q changed after preview", entry.Path)
			}
			contents["files/"+entry.Path] = data
		case "artifact":
			artifact, data, getErr := s.GetArtifact(ctx, entry.ArtifactID)
			if getErr != nil || artifact.ProjectID != project.ID || artifact.Visibility != "project_shared" || artifact.ExportPolicy != "explicit_selection" || artifact.SharingRevision != entry.SharingRevision || artifact.Checksum != entry.SHA256 {
				return ShareExport{}, fmt.Errorf("share artifact %q changed after preview", entry.ArtifactID)
			}
			contents["artifacts/"+entry.ArtifactID+".blob"] = data
		case "skill":
			skill, getErr := s.skills.GetSkill(ctx, entry.ObjectID)
			if getErr != nil || skill.ScopeKind != "project" || skill.ScopeRef != project.ID || skill.Visibility != "project_shared" ||
				skill.ExportPolicy != "explicit_selection" || skill.SharingRevision != entry.SharingRevision || skill.CanonicalName != entry.Name {
				return ShareExport{}, fmt.Errorf("share skill %q changed after preview", entry.ObjectID)
			}
			version, getErr := s.skills.GetVersion(ctx, skill.CurrentVersionID)
			if getErr != nil {
				return ShareExport{}, getErr
			}
			data, getErr := s.store.Blobs.Get(version.PackageBlobRef)
			if getErr != nil || checksum(data) != entry.SHA256 || version.ContentHash != entry.SHA256 || int64(len(data)) != entry.Bytes {
				return ShareExport{}, fmt.Errorf("share skill %q changed after preview", entry.ObjectID)
			}
			contents["skills/"+entry.ObjectID+".skill"] = data
		default:
			return ShareExport{}, fmt.Errorf("share entry kind %q is unsupported", entry.Kind)
		}
	}
	for _, task := range manifest.Tasks {
		current, currentErr := s.shareTaskProjection(ctx, ownerID, project.ID, task.SourceID)
		if currentErr != nil || canonicalProjectionDigest(current) != canonicalProjectionDigest(task) {
			return ShareExport{}, fmt.Errorf("share task %q changed after preview", task.SourceID)
		}
	}
	for _, memory := range manifest.Memories {
		current, currentErr := s.shareMemoryProjection(ctx, ownerID, project.ID, memory.SourceID)
		if currentErr != nil || canonicalProjectionDigest(current) != canonicalProjectionDigest(memory) {
			return ShareExport{}, fmt.Errorf("share memory %q changed after preview", memory.SourceID)
		}
	}
	for _, edge := range manifest.Dependencies {
		var count int
		if queryErr := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artifact_derivations WHERE source_artifact_id=? AND result_artifact_id=? AND relation_kind=? AND source_hash=? AND result_hash=?`,
			edge.SourceID, edge.ResultID, edge.RelationKind, edge.SourceHash, edge.ResultHash).Scan(&count); queryErr != nil || count != 1 {
			return ShareExport{}, fmt.Errorf("share dependency %q -> %q changed after preview", edge.SourceID, edge.ResultID)
		}
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	manifestHeader := &zip.FileHeader{Name: "manifest.json", Method: zip.Deflate}
	manifestHeader.SetMode(0600)
	w, _ := zw.CreateHeader(manifestHeader)
	_, _ = w.Write(body)
	keys := make([]string, 0, len(contents))
	for key := range contents {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		header := &zip.FileHeader{Name: key, Method: zip.Deflate}
		header.SetMode(0600)
		writer, createErr := zw.CreateHeader(header)
		if createErr != nil {
			return ShareExport{}, createErr
		}
		if _, err = writer.Write(contents[key]); err != nil {
			return ShareExport{}, err
		}
	}
	if err = zw.Close(); err != nil {
		return ShareExport{}, err
	}
	if archive.Len() > maxShareCompressedBytes {
		return ShareExport{}, fmt.Errorf("share package exceeds 512 MiB")
	}
	packageRef, err := s.store.Blobs.Put(archive.Bytes())
	if err != nil {
		return ShareExport{}, err
	}
	now := time.Now().UTC()
	item := ShareExport{ID: identity.New("shareexport"), ProjectID: project.ID, PreviewID: input.PreviewID,
		ManifestDigest: storedDigest, IdempotencyKey: input.IdempotencyKey, State: "completed", PackageChecksum: checksum(archive.Bytes()), CreatedAt: now, CompletedAt: &now}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return ShareExport{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO share_export_jobs(id,owner_principal_id,project_id,preview_id,manifest_digest,idempotency_key,
		payload_hash,state,package_blob_ref,package_checksum,created_at,completed_at) VALUES(?,?,?,?,?,?,?,'completed',?,?,?,?)`, item.ID,
		ownerID, project.ID, input.PreviewID, storedDigest, input.IdempotencyKey, payloadHash, packageRef, item.PackageChecksum, formatTime(now), formatTime(now))
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE share_previews SET state='consumed' WHERE id=? AND state='ready'`, input.PreviewID)
	}
	if err == nil {
		err = tx.Commit()
	}
	return item, err
}

func (s *Service) GetShareExport(ctx context.Context, id string) (ShareExport, error) {
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return ShareExport{}, err
	}
	var item ShareExport
	var created string
	var completed sql.NullString
	err = s.store.DB.QueryRowContext(ctx, `SELECT id,project_id,preview_id,manifest_digest,idempotency_key,state,package_checksum,error,created_at,completed_at
		FROM share_export_jobs WHERE id=? AND owner_principal_id=?`, id, ownerID).Scan(&item.ID, &item.ProjectID, &item.PreviewID, &item.ManifestDigest,
		&item.IdempotencyKey, &item.State, &item.PackageChecksum, &item.Error, &created, &completed)
	if err != nil {
		return item, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if completed.Valid {
		value, _ := time.Parse(time.RFC3339Nano, completed.String)
		item.CompletedAt = &value
	}
	return item, nil
}

func (s *Service) ShareExportContent(ctx context.Context, id string) (ShareExport, []byte, error) {
	item, err := s.GetShareExport(ctx, id)
	if err != nil {
		return item, nil, err
	}
	ownerID, _ := s.store.OwnerPrincipalID(ctx)
	var ref string
	if err = s.store.DB.QueryRowContext(ctx, `SELECT package_blob_ref FROM share_export_jobs WHERE id=? AND owner_principal_id=? AND state='completed'`, id, ownerID).Scan(&ref); err != nil {
		return item, nil, err
	}
	body, err := s.store.Blobs.Get(ref)
	if err != nil || checksum(body) != item.PackageChecksum {
		return item, nil, fmt.Errorf("share package failed integrity check")
	}
	return item, body, nil
}

func (s *Service) PreviewShareImport(ctx context.Context, data []byte, actor string) (ShareImportPreview, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" || len(data) == 0 || len(data) > maxShareCompressedBytes {
		return ShareImportPreview{}, fmt.Errorf("actor and share package up to 512 MiB are required")
	}
	manifest, err := validateShareArchive(data)
	if err != nil {
		return ShareImportPreview{}, err
	}
	ref, err := s.store.Blobs.Put(data)
	if err != nil {
		return ShareImportPreview{}, err
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return ShareImportPreview{}, err
	}
	body, _ := json.Marshal(manifest)
	digest := checksum(body)
	now, expires := time.Now().UTC(), time.Now().UTC().Add(sharePreviewExpiry)
	summary, _ := json.Marshal(map[string]any{"project_name": manifest.ProjectName, "entries": manifest.Entries})
	item := ShareImportPreview{ID: identity.New("shareimport"), ManifestDigest: digest, ProjectName: manifest.ProjectName, Entries: manifest.Entries, State: "awaiting_apply", CreatedAt: now, ExpiresAt: expires}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO share_import_staging(id,owner_principal_id,actor,package_blob_ref,package_checksum,manifest_digest,state,summary_json,created_at,expires_at)
		VALUES(?,?,?,?,?,?,'awaiting_apply',?,?,?)`, item.ID, ownerID, actor, ref, checksum(data), digest, string(summary), formatTime(now), formatTime(expires))
	return item, err
}

func validateShareArchive(data []byte) (shareManifest, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return shareManifest{}, fmt.Errorf("invalid share ZIP: %w", err)
	}
	if len(zr.File) == 0 || len(zr.File) > maxShareEntries {
		return shareManifest{}, fmt.Errorf("share ZIP entry count is outside limits")
	}
	seen := map[string]bool{}
	files := map[string]*zip.File{}
	var total int64
	for _, file := range zr.File {
		name := file.Name
		clean := path.Clean(name)
		lower := strings.ToLower(clean)
		if name != clean || path.IsAbs(name) || strings.HasPrefix(clean, "../") || strings.Contains(name, "\\") || seen[lower] {
			return shareManifest{}, fmt.Errorf("share ZIP contains unsafe or case-colliding path %q", name)
		}
		seen[lower] = true
		if file.Mode()&os.ModeSymlink != 0 || file.FileInfo().IsDir() {
			return shareManifest{}, fmt.Errorf("share ZIP links/directories are forbidden")
		}
		if file.UncompressedSize64 > uint64(maxShareEntryBytes) {
			return shareManifest{}, fmt.Errorf("share ZIP entry exceeds limit")
		}
		total += int64(file.UncompressedSize64)
		if total > maxShareExpandedBytes {
			return shareManifest{}, fmt.Errorf("share ZIP expands beyond 2 GiB")
		}
		if file.CompressedSize64 > 0 && file.UncompressedSize64/file.CompressedSize64 > 100 {
			return shareManifest{}, fmt.Errorf("share ZIP compression ratio exceeds limit")
		}
		files[name] = file
	}
	manifestFile, ok := files["manifest.json"]
	if !ok {
		return shareManifest{}, fmt.Errorf("share manifest is missing")
	}
	manifestBody, err := readBoundedZipEntry(manifestFile, maxShareEntryBytes)
	if err != nil {
		return shareManifest{}, fmt.Errorf("read share manifest: %w", err)
	}
	var manifest shareManifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBody))
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&manifest)
	var trailing any
	if decodeErr == nil && decoder.Decode(&trailing) != io.EOF {
		decodeErr = fmt.Errorf("share manifest contains trailing JSON")
	}
	if decodeErr != nil || manifest.Format != shareFormat || manifest.PackageKind != "project_share" ||
		manifest.FormatVersion != 1 || manifest.SourceApplication != "hermetrix-harness" || manifest.SourceSchemaVersion < 1 ||
		manifest.SourceSchemaVersion > store.CurrentSchemaVersion || strings.TrimSpace(manifest.ExportID) == "" || strings.TrimSpace(manifest.ProjectName) == "" {
		return shareManifest{}, fmt.Errorf("share manifest kind or format is invalid")
	}
	expected := map[string]bool{"manifest.json": true}
	artifactHashes := map[string]string{}
	for _, entry := range manifest.Entries {
		var name string
		switch entry.Kind {
		case "file":
			safe, err := safeSharePath(entry.Path)
			if err != nil || sharePathExcluded(safe) {
				return shareManifest{}, fmt.Errorf("manifest contains unsafe file path")
			}
			name = "files/" + safe
		case "artifact":
			if strings.TrimSpace(entry.ArtifactID) == "" {
				return shareManifest{}, fmt.Errorf("manifest artifact id is empty")
			}
			name = "artifacts/" + entry.ArtifactID + ".blob"
		case "skill":
			if strings.TrimSpace(entry.ObjectID) == "" || strings.ContainsAny(entry.ObjectID, `/\\`) || strings.TrimSpace(entry.Name) == "" {
				return shareManifest{}, fmt.Errorf("manifest skill identity is invalid")
			}
			name = "skills/" + entry.ObjectID + ".skill"
		default:
			return shareManifest{}, fmt.Errorf("manifest entry kind %q is invalid", entry.Kind)
		}
		if expected[name] {
			return shareManifest{}, fmt.Errorf("manifest contains duplicate entry")
		}
		expected[name] = true
		file, exists := files[name]
		if !exists || entry.Bytes < 0 || uint64(entry.Bytes) != file.UncompressedSize64 || verifyZipEntry(file, entry.SHA256) != nil {
			return shareManifest{}, fmt.Errorf("manifest entry %q failed integrity check", name)
		}
		if entry.Kind == "artifact" {
			artifactHashes[entry.ArtifactID] = entry.SHA256
		}
	}
	if len(expected) != len(files) {
		return shareManifest{}, fmt.Errorf("share ZIP contains undeclared entries")
	}
	seenObjects := map[string]bool{}
	for _, task := range manifest.Tasks {
		key := "task:" + task.SourceID
		if strings.TrimSpace(task.SourceID) == "" || strings.TrimSpace(task.Title) == "" || strings.TrimSpace(task.Objective) == "" ||
			strings.TrimSpace(task.OriginalRequest) == "" || task.SharingRevision < 1 || seenObjects[key] || len(task.Criteria) == 0 {
			return shareManifest{}, fmt.Errorf("share manifest contains an invalid task projection")
		}
		seenObjects[key] = true
	}
	for _, memory := range manifest.Memories {
		key := "memory:" + memory.SourceID
		if strings.TrimSpace(memory.SourceID) == "" || strings.TrimSpace(memory.MemoryKind) == "" || len(memory.Content) > 1<<20 ||
			memory.SharingRevision < 1 || seenObjects[key] {
			return shareManifest{}, fmt.Errorf("share manifest contains an invalid memory projection")
		}
		seenObjects[key] = true
	}
	seenEdges := map[string]bool{}
	for _, edge := range manifest.Dependencies {
		key := edge.SourceID + "\x00" + edge.ResultID + "\x00" + edge.RelationKind
		if edge.SourceID == "" || edge.ResultID == "" || edge.RelationKind == "" || seenEdges[key] ||
			artifactHashes[edge.SourceID] != edge.SourceHash || artifactHashes[edge.ResultID] != edge.ResultHash {
			return shareManifest{}, fmt.Errorf("share manifest contains an invalid dependency edge")
		}
		seenEdges[key] = true
	}
	return manifest, nil
}

func readBoundedZipEntry(file *zip.File, limit int64) ([]byte, error) {
	if file == nil || file.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("ZIP entry exceeds bounded read limit")
	}
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	body, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil || int64(len(body)) > limit || uint64(len(body)) != file.UncompressedSize64 {
		return nil, fmt.Errorf("ZIP entry read failed")
	}
	return body, nil
}

func verifyZipEntry(file *zip.File, expected string) error {
	if file == nil || !sha256Pattern.MatchString(expected) {
		return fmt.Errorf("invalid ZIP integrity binding")
	}
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(rc, maxShareEntryBytes+1))
	if err != nil || written != int64(file.UncompressedSize64) || written > maxShareEntryBytes || hex.EncodeToString(hash.Sum(nil)) != expected {
		return fmt.Errorf("ZIP entry failed integrity check")
	}
	return nil
}

func (s *Service) ApplyShareImport(ctx context.Context, input ApplyShareImportInput) (Project, error) {
	input.Actor = strings.TrimSpace(input.Actor)
	if input.PreviewID == "" || input.ManifestDigest == "" || input.Actor == "" || strings.TrimSpace(input.ProjectName) == "" || strings.TrimSpace(input.DestinationRoot) == "" {
		return Project{}, fmt.Errorf("preview, digest, project name, destination root and actor are required")
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return Project{}, err
	}
	var ref, storedDigest, state, expiresText string
	err = s.store.DB.QueryRowContext(ctx, `SELECT package_blob_ref,manifest_digest,state,expires_at FROM share_import_staging WHERE id=? AND owner_principal_id=?`, input.PreviewID, ownerID).Scan(&ref, &storedDigest, &state, &expiresText)
	if err != nil {
		return Project{}, err
	}
	expires, _ := time.Parse(time.RFC3339Nano, expiresText)
	if state != "awaiting_apply" || storedDigest != input.ManifestDigest || !expires.After(time.Now().UTC()) {
		return Project{}, fmt.Errorf("share import preview is stale or expired")
	}
	data, err := s.store.Blobs.Get(ref)
	if err != nil {
		return Project{}, err
	}
	manifest, err := validateShareArchive(data)
	if err != nil {
		return Project{}, err
	}
	destination, err := filepath.Abs(input.DestinationRoot)
	if err != nil {
		return Project{}, err
	}
	if info, statErr := os.Stat(destination); statErr == nil && (info.IsDir() || !info.IsDir()) {
		return Project{}, fmt.Errorf("destination root must not already exist")
	} else if !os.IsNotExist(statErr) {
		return Project{}, statErr
	}
	parent := filepath.Dir(destination)
	if err = os.MkdirAll(parent, 0700); err != nil {
		return Project{}, err
	}
	staging, err := os.MkdirTemp(parent, ".hermetrix-share-")
	if err != nil {
		return Project{}, err
	}
	defer os.RemoveAll(staging)
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	archive := map[string]*zip.File{}
	for _, file := range zr.File {
		archive[file.Name] = file
	}
	for _, entry := range manifest.Entries {
		if entry.Kind != "file" {
			continue
		}
		target := filepath.Join(staging, filepath.FromSlash(entry.Path))
		relative, relErr := filepath.Rel(staging, target)
		if relErr != nil || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return Project{}, fmt.Errorf("import path escapes staging")
		}
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return Project{}, err
		}
		file := archive["files/"+entry.Path]
		if file == nil {
			return Project{}, fmt.Errorf("import file %q is missing", entry.Path)
		}
		reader, openErr := file.Open()
		if openErr != nil {
			return Project{}, openErr
		}
		output, openErr := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if openErr != nil {
			reader.Close()
			return Project{}, openErr
		}
		written, copyErr := io.Copy(output, io.LimitReader(reader, maxShareEntryBytes+1))
		closeErr := output.Close()
		reader.Close()
		if copyErr != nil || closeErr != nil || written != entry.Bytes {
			if copyErr != nil {
				return Project{}, copyErr
			}
			if closeErr != nil {
				return Project{}, closeErr
			}
			return Project{}, fmt.Errorf("import file %q changed during extraction", entry.Path)
		}
		if err = verifyExtractedFile(target, entry.SHA256, entry.Bytes); err != nil {
			return Project{}, err
		}
	}
	projectID := identity.New("project")
	now := time.Now().UTC()
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO projects(id,name,root_path,state,created_at,updated_at,owner_principal_id) VALUES(?,?,?,'active',?,?,?)`, projectID, input.ProjectName, destination, formatTime(now), formatTime(now), ownerID)
	for _, task := range manifest.Tasks {
		if err != nil {
			break
		}
		newTaskID, requirementID := identity.New("task"), identity.New("reqrev")
		_, err = tx.ExecContext(ctx, `INSERT INTO durable_tasks(id,project_id,title,objective,original_request,state,created_at,updated_at,owner_principal_id,egress_policy)
			VALUES(?,?,?,?,?,'draft',?,?,?,'local_only')`, newTaskID, projectID, task.Title, task.Objective, task.OriginalRequest,
			formatTime(now), formatTime(now), ownerID)
		constraints, _ := json.Marshal(task.Constraints)
		unknowns, _ := json.Marshal(task.Unknowns)
		criteria, _ := json.Marshal(task.Criteria)
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO task_requirement_revisions(id,task_id,revision,constraints_json,unknowns_json,criteria_json,actor,created_at)
				VALUES(?,?,1,?,?,?,?,?)`, requirementID, newTaskID, string(constraints), string(unknowns), string(criteria), "project_share_import", formatTime(now))
		}
	}
	for _, memory := range manifest.Memories {
		if err != nil {
			break
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO memories(id,scope_kind,scope_ref,memory_kind,content,source,state,created_at,updated_at,owner_principal_id)
			VALUES(?,'project',?,?,?,?, 'active',?,?,?)`, identity.New("memory"), projectID, memory.MemoryKind, memory.Content,
			"project_share_import", formatTime(now), formatTime(now), ownerID)
	}
	artifactIDMap := map[string]string{}
	for _, entry := range manifest.Entries {
		if err != nil || (entry.Kind != "artifact" && entry.Kind != "skill") {
			continue
		}
		entryName := "artifacts/" + entry.ArtifactID + ".blob"
		if entry.Kind == "skill" {
			entryName = "skills/" + entry.ObjectID + ".skill"
		}
		body, readErr := readBoundedZipEntry(archive[entryName], maxShareEntryBytes)
		if readErr != nil || checksum(body) != entry.SHA256 {
			if readErr != nil {
				err = readErr
			} else {
				err = fmt.Errorf("object %q changed during extraction", entryName)
			}
			break
		}
		if entry.Kind == "skill" {
			pkg, parseErr := skills.ParsePackage(body)
			if parseErr != nil {
				err = parseErr
				break
			}
			_, err = s.skills.CreateCandidateInTx(ctx, tx, skills.CreateCandidateInput{CanonicalName: entry.Name, ScopeKind: "project",
				ScopeRef: projectID, Origin: "imported", Owner: "user", ChangeKind: "create", CreatedBy: input.Actor,
				TriggerKind: "project_share_import", Reason: "imported from verified project share " + input.PreviewID,
				EvidenceRefs: []string{"share:" + input.PreviewID, "source_skill:" + entry.ObjectID}, Markdown: pkg.Markdown(),
				Files: packageSupportingFiles(pkg)})
			continue
		}
		blobRef, putErr := s.store.Blobs.Put(body)
		if putErr != nil {
			err = putErr
			break
		}
		newID := identity.New("artifact")
		metadata, _ := json.Marshal(map[string]any{"imported_from_share_artifact": entry.ArtifactID, "source_manifest_digest": storedDigest})
		_, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,project_id,name,kind,mime_type,blob_ref,byte_size,checksum,metadata_json,created_at,owner_principal_id)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)`, newID, projectID, entry.Name, "shared_import", entry.MIMEType, blobRef, len(body), blobRef, string(metadata), formatTime(now), ownerID)
		if err == nil {
			artifactIDMap[entry.ArtifactID] = newID
		}
	}
	for _, edge := range manifest.Dependencies {
		if err != nil {
			break
		}
		sourceID, sourceOK := artifactIDMap[edge.SourceID]
		resultID, resultOK := artifactIDMap[edge.ResultID]
		if !sourceOK || !resultOK {
			err = fmt.Errorf("share dependency references an artifact outside the import map")
			break
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO artifact_derivations(id,owner_principal_id,source_artifact_id,result_artifact_id,relation_kind,source_hash,result_hash,created_at)
			VALUES(?,?,?,?,?,?,?,?)`, identity.New("derivation"), ownerID, sourceID, resultID, edge.RelationKind, edge.SourceHash, edge.ResultHash, formatTime(now))
	}
	renamed := false
	if err == nil {
		err = os.Rename(staging, destination)
		renamed = err == nil
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE share_import_staging SET state='completed',destination_project_id=?,completed_at=? WHERE id=? AND state='awaiting_apply'`, projectID, formatTime(now), input.PreviewID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		if renamed {
			_ = os.RemoveAll(destination)
		}
		return Project{}, err
	}
	return s.GetProject(ctx, projectID)
}

func verifyExtractedFile(name, expected string, expectedBytes int64) error {
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maxShareEntryBytes+1))
	if err != nil || written != expectedBytes || hex.EncodeToString(hash.Sum(nil)) != expected {
		return fmt.Errorf("extracted share file failed integrity check")
	}
	return nil
}

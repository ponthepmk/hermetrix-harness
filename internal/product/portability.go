package product

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
	"hermetrix-harness/internal/identity"
)

const (
	workspaceFormatVersion = 1
	workspaceFormatKind    = "hermetrix.workspace-migration.v1"
	maxWorkspaceBytes      = 256 << 20
)

type portableFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Data   []byte `json:"data"`
}
type portableArtifact struct {
	SourceID, Name, Kind, MIMEType, SHA256 string
	Data                                   []byte
}
type portableMemory struct{ SourceID, MemoryKind, Content, Source string }
type portableProject struct {
	SourceID, Name string
	Files          []portableFile
	Artifacts      []portableArtifact
	Memories       []portableMemory
}
type workspacePayload struct {
	Format            string            `json:"format"`
	FormatVersion     int               `json:"format_version"`
	SourcePrincipalID string            `json:"source_principal_id"`
	CreatedAt         time.Time         `json:"created_at"`
	Projects          []portableProject `json:"projects"`
	ExcludedAuthority []string          `json:"excluded_authority"`
}

func encryptWorkspace(payload workspacePayload, passphrase string) ([]byte, error) {
	if len(passphrase) < 12 {
		return nil, fmt.Errorf("workspace migration passphrase must be at least 12 characters")
	}
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, err
	}
	var encrypted bytes.Buffer
	writer, err := age.Encrypt(&encrypted, recipient)
	if err != nil {
		return nil, err
	}
	encoder := json.NewEncoder(writer)
	if err = encoder.Encode(payload); err != nil {
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	if encrypted.Len() > maxWorkspaceBytes {
		return nil, fmt.Errorf("encrypted workspace package exceeds 256 MiB")
	}
	return encrypted.Bytes(), nil
}

func decryptWorkspace(data []byte, passphrase string) (workspacePayload, error) {
	if len(data) == 0 || len(data) > maxWorkspaceBytes || len(passphrase) < 12 {
		return workspacePayload{}, fmt.Errorf("encrypted package and passphrase of at least 12 characters are required")
	}
	identityValue, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return workspacePayload{}, err
	}
	reader, err := age.Decrypt(bytes.NewReader(data), identityValue)
	if err != nil {
		return workspacePayload{}, fmt.Errorf("decrypt workspace package: %w", err)
	}
	limited := io.LimitReader(reader, maxWorkspaceBytes+1)
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	var payload workspacePayload
	if err = decoder.Decode(&payload); err != nil {
		return workspacePayload{}, fmt.Errorf("decode workspace package: %w", err)
	}
	if payload.Format != workspaceFormatKind || payload.FormatVersion != workspaceFormatVersion || payload.SourcePrincipalID == "" || len(payload.Projects) == 0 {
		return workspacePayload{}, fmt.Errorf("workspace package format is unsupported or incomplete")
	}
	if err = validateWorkspacePayload(payload); err != nil {
		return workspacePayload{}, err
	}
	return payload, nil
}

func validateWorkspacePayload(payload workspacePayload) error {
	seenProjects := map[string]bool{}
	total := 0
	entries := 0
	for _, project := range payload.Projects {
		if project.SourceID == "" || strings.TrimSpace(project.Name) == "" || seenProjects[project.SourceID] {
			return fmt.Errorf("workspace project identity is invalid or duplicated")
		}
		seenProjects[project.SourceID] = true
		seenPaths := map[string]bool{}
		for _, file := range project.Files {
			safe, err := safeSharePath(file.Path)
			if err != nil || sharePathExcluded(safe) || seenPaths[strings.ToLower(safe)] {
				return fmt.Errorf("workspace file path is unsafe or duplicated")
			}
			seenPaths[strings.ToLower(safe)] = true
			if checksum(file.Data) != file.SHA256 || len(file.Data) > maxArtifactBytes {
				return fmt.Errorf("workspace file integrity or size check failed")
			}
			total += len(file.Data)
			entries++
		}
		seenArtifacts := map[string]bool{}
		for _, artifact := range project.Artifacts {
			if artifact.SourceID == "" || seenArtifacts[artifact.SourceID] || checksum(artifact.Data) != artifact.SHA256 || len(artifact.Data) > maxArtifactBytes {
				return fmt.Errorf("workspace artifact integrity or identity check failed")
			}
			seenArtifacts[artifact.SourceID] = true
			total += len(artifact.Data)
			entries++
		}
		for _, memory := range project.Memories {
			if memory.SourceID == "" || len(memory.Content) > 1<<20 {
				return fmt.Errorf("workspace memory is invalid or too large")
			}
			total += len(memory.Content)
			entries++
		}
	}
	if total > maxWorkspaceBytes || entries > maxShareEntries {
		return fmt.Errorf("workspace package expands beyond entry or byte limits")
	}
	return nil
}

func (s *Service) ExportWorkspace(ctx context.Context, input WorkspaceExportInput) (WorkspaceMigration, []byte, error) {
	input.Actor = strings.TrimSpace(input.Actor)
	projectIDs := shareCleanStrings(input.ProjectIDs)
	if input.Actor == "" || len(projectIDs) == 0 {
		return WorkspaceMigration{}, nil, fmt.Errorf("actor and selected projects are required")
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return WorkspaceMigration{}, nil, err
	}
	payload := workspacePayload{Format: workspaceFormatKind, FormatVersion: workspaceFormatVersion, SourcePrincipalID: ownerID, CreatedAt: time.Now().UTC(), ExcludedAuthority: []string{"credentials", "sessions and turns", "approvals", "task attempts and effects", "browser profiles", "process handles", "runtime qualification state"}}
	for _, projectID := range projectIDs {
		project, projectErr := s.GetProject(ctx, projectID)
		if projectErr != nil {
			return WorkspaceMigration{}, nil, projectErr
		}
		root, rootErr := requireRoot(project)
		if rootErr != nil {
			return WorkspaceMigration{}, nil, rootErr
		}
		portable := portableProject{SourceID: project.ID, Name: project.Name}
		err = filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, relErr := filepath.Rel(root, current)
			if relErr != nil {
				return relErr
			}
			if relative == "." {
				return nil
			}
			slash := filepath.ToSlash(relative)
			if entry.IsDir() {
				if sharePathExcluded(slash) || strings.EqualFold(entry.Name(), "node_modules") {
					return filepath.SkipDir
				}
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			if sharePathExcluded(slash) {
				return nil
			}
			if info.Size() > maxArtifactBytes {
				return fmt.Errorf("workspace file %q exceeds 16 MiB", slash)
			}
			body, readErr := os.ReadFile(current)
			if readErr != nil {
				return readErr
			}
			portable.Files = append(portable.Files, portableFile{Path: slash, SHA256: checksum(body), Data: body})
			return nil
		})
		if err != nil {
			return WorkspaceMigration{}, nil, err
		}
		artifacts, artifactErr := s.ListArtifacts(ctx, project.ID)
		if artifactErr != nil {
			return WorkspaceMigration{}, nil, artifactErr
		}
		for _, artifact := range artifacts {
			_, body, getErr := s.GetArtifact(ctx, artifact.ID)
			if getErr != nil {
				return WorkspaceMigration{}, nil, getErr
			}
			portable.Artifacts = append(portable.Artifacts, portableArtifact{SourceID: artifact.ID, Name: artifact.Name, Kind: artifact.Kind, MIMEType: artifact.MIMEType, SHA256: artifact.Checksum, Data: body})
		}
		memories, memoryErr := s.ListMemories(ctx, "project", project.ID)
		if memoryErr != nil {
			return WorkspaceMigration{}, nil, memoryErr
		}
		for _, memory := range memories {
			portable.Memories = append(portable.Memories, portableMemory{SourceID: memory.ID, MemoryKind: memory.MemoryKind, Content: memory.Content, Source: memory.Source})
		}
		sort.Slice(portable.Files, func(i, j int) bool { return portable.Files[i].Path < portable.Files[j].Path })
		sort.Slice(portable.Artifacts, func(i, j int) bool { return portable.Artifacts[i].SourceID < portable.Artifacts[j].SourceID })
		payload.Projects = append(payload.Projects, portable)
	}
	if err = validateWorkspacePayload(payload); err != nil {
		return WorkspaceMigration{}, nil, err
	}
	encrypted, err := encryptWorkspace(payload, input.Passphrase)
	if err != nil {
		return WorkspaceMigration{}, nil, err
	}
	ref, err := s.store.Blobs.Put(encrypted)
	if err != nil {
		return WorkspaceMigration{}, nil, err
	}
	now := time.Now().UTC()
	summary := map[string]any{"projects": len(payload.Projects), "encrypted": true, "format": "age-v1-scrypt", "excluded_authority": payload.ExcludedAuthority}
	summaryJSON, _ := json.Marshal(summary)
	item := WorkspaceMigration{ID: identity.New("workspacemigration"), Kind: "export", State: "completed", FormatVersion: workspaceFormatVersion, PackageChecksum: checksum(encrypted), SourcePrincipalID: ownerID, Summary: summary, CreatedAt: now, CompletedAt: &now}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO workspace_migration_jobs(id,owner_principal_id,kind,state,format_version,package_blob_ref,package_checksum,source_principal_id,summary_json,created_at,completed_at)
		VALUES(?,?,'export','completed',?,?,?,?,?,?,?)`, item.ID, ownerID, workspaceFormatVersion, ref, item.PackageChecksum, ownerID, string(summaryJSON), formatTime(now), formatTime(now))
	return item, encrypted, err
}

func (s *Service) PreviewWorkspaceImport(ctx context.Context, input WorkspaceImportPreviewInput) (WorkspaceMigration, error) {
	input.Actor = strings.TrimSpace(input.Actor)
	if input.Actor == "" {
		return WorkspaceMigration{}, fmt.Errorf("import actor is required")
	}
	payload, err := decryptWorkspace(input.EncryptedPackage, input.Passphrase)
	if err != nil {
		return WorkspaceMigration{}, err
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return WorkspaceMigration{}, err
	}
	ref, err := s.store.Blobs.Put(input.EncryptedPackage)
	if err != nil {
		return WorkspaceMigration{}, err
	}
	now := time.Now().UTC()
	summary := map[string]any{"projects": len(payload.Projects), "project_ids": func() []string {
		ids := []string{}
		for _, p := range payload.Projects {
			ids = append(ids, p.SourceID)
		}
		return ids
	}(), "encrypted": true, "source_principal_id": payload.SourcePrincipalID, "authority_imported": false}
	summaryJSON, _ := json.Marshal(summary)
	item := WorkspaceMigration{ID: identity.New("workspacemigration"), Kind: "import_preview", State: "awaiting_apply", FormatVersion: workspaceFormatVersion, PackageChecksum: checksum(input.EncryptedPackage), SourcePrincipalID: payload.SourcePrincipalID, DestinationPrincipalID: ownerID, Summary: summary, CreatedAt: now}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO workspace_migration_jobs(id,owner_principal_id,kind,state,format_version,package_blob_ref,package_checksum,source_principal_id,destination_principal_id,summary_json,created_at)
		VALUES(?,?,'import_preview','awaiting_apply',?,?,?,?,?,?,?)`, item.ID, ownerID, workspaceFormatVersion, ref, item.PackageChecksum, payload.SourcePrincipalID, ownerID, string(summaryJSON), formatTime(now))
	return item, err
}

func (s *Service) ApplyWorkspaceImport(ctx context.Context, input WorkspaceImportApplyInput) (WorkspaceMigration, error) {
	input.Actor = strings.TrimSpace(input.Actor)
	if input.PreviewID == "" || input.Actor == "" || len(input.RootMappings) == 0 {
		return WorkspaceMigration{}, fmt.Errorf("preview, actor and explicit project root mappings are required")
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return WorkspaceMigration{}, err
	}
	var ref, expectedChecksum, sourcePrincipal, state string
	err = s.store.DB.QueryRowContext(ctx, `SELECT package_blob_ref,package_checksum,source_principal_id,state FROM workspace_migration_jobs WHERE id=? AND owner_principal_id=? AND kind='import_preview'`, input.PreviewID, ownerID).Scan(&ref, &expectedChecksum, &sourcePrincipal, &state)
	if err != nil {
		return WorkspaceMigration{}, err
	}
	if state != "awaiting_apply" {
		return WorkspaceMigration{}, fmt.Errorf("workspace preview is not awaiting apply")
	}
	encrypted, err := s.store.Blobs.Get(ref)
	if err != nil || checksum(encrypted) != expectedChecksum {
		return WorkspaceMigration{}, fmt.Errorf("stored encrypted package failed integrity check")
	}
	payload, err := decryptWorkspace(encrypted, input.Passphrase)
	if err != nil {
		return WorkspaceMigration{}, err
	}
	if payload.SourcePrincipalID != sourcePrincipal {
		return WorkspaceMigration{}, fmt.Errorf("workspace source principal binding changed")
	}
	type stagedProject struct {
		source                      portableProject
		destination, staging, newID string
		renamed                     bool
	}
	staged := []stagedProject{}
	destinations := map[string]bool{}
	for _, project := range payload.Projects {
		root := strings.TrimSpace(input.RootMappings[project.SourceID])
		if root == "" {
			return WorkspaceMigration{}, fmt.Errorf("missing destination root for project %s", project.SourceID)
		}
		destination, absErr := filepath.Abs(root)
		if absErr != nil {
			return WorkspaceMigration{}, absErr
		}
		lower := strings.ToLower(destination)
		if destinations[lower] {
			return WorkspaceMigration{}, fmt.Errorf("destination roots collide")
		}
		destinations[lower] = true
		if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
			if statErr == nil {
				return WorkspaceMigration{}, fmt.Errorf("destination root already exists")
			}
			return WorkspaceMigration{}, statErr
		}
		if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return WorkspaceMigration{}, err
		}
		stage, stageErr := os.MkdirTemp(filepath.Dir(destination), ".hermetrix-workspace-")
		if stageErr != nil {
			return WorkspaceMigration{}, stageErr
		}
		item := stagedProject{source: project, destination: destination, staging: stage, newID: identity.New("project")}
		for _, file := range project.Files {
			target := filepath.Join(stage, filepath.FromSlash(file.Path))
			if err = os.MkdirAll(filepath.Dir(target), 0700); err == nil {
				err = os.WriteFile(target, file.Data, 0600)
			}
			if err != nil {
				os.RemoveAll(stage)
				return WorkspaceMigration{}, err
			}
		}
		staged = append(staged, item)
	}
	cleanup := func(removeDest bool) {
		for _, item := range staged {
			_ = os.RemoveAll(item.staging)
			if removeDest && item.renamed {
				_ = os.RemoveAll(item.destination)
			}
		}
	}
	defer cleanup(false)
	now := time.Now().UTC()
	jobID := identity.New("workspacemigration")
	summary := map[string]any{"projects": len(staged), "source_principal_id": sourcePrincipal, "destination_principal_id": ownerID, "authority_imported": false, "runtime_configs_enabled": false}
	summaryJSON, _ := json.Marshal(summary)
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return WorkspaceMigration{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO workspace_migration_jobs(id,owner_principal_id,kind,state,format_version,source_principal_id,destination_principal_id,summary_json,created_at)
		VALUES(?,?,'import_apply','running',?,?,?,?,?)`, jobID, ownerID, workspaceFormatVersion, sourcePrincipal, ownerID, string(summaryJSON), formatTime(now))
	for _, item := range staged {
		if err != nil {
			break
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO projects(id,name,root_path,state,created_at,updated_at,owner_principal_id) VALUES(?,?,?,'active',?,?,?)`, item.newID, item.source.Name, item.destination, formatTime(now), formatTime(now), ownerID)
		if err != nil {
			break
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO workspace_migration_maps(id,job_id,object_kind,source_id,destination_id,destination_root,created_at) VALUES(?,?,?,?,?,?,?)`, identity.New("migrationmap"), jobID, "project", item.source.SourceID, item.newID, item.destination, formatTime(now))
		for _, artifact := range item.source.Artifacts {
			if err != nil {
				break
			}
			blobRef, putErr := s.store.Blobs.Put(artifact.Data)
			if putErr != nil {
				err = putErr
				break
			}
			newID := identity.New("artifact")
			metadata, _ := json.Marshal(map[string]any{"workspace_source_artifact_id": artifact.SourceID, "workspace_source_principal_id": sourcePrincipal})
			_, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,project_id,name,kind,mime_type,blob_ref,byte_size,checksum,metadata_json,created_at,owner_principal_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, newID, item.newID, artifact.Name, artifact.Kind, artifact.MIMEType, blobRef, len(artifact.Data), blobRef, string(metadata), formatTime(now), ownerID)
			if err == nil {
				_, err = tx.ExecContext(ctx, `INSERT INTO workspace_migration_maps(id,job_id,object_kind,source_id,destination_id,created_at) VALUES(?,?,?,?,?,?)`, identity.New("migrationmap"), jobID, "artifact", artifact.SourceID, newID, formatTime(now))
			}
		}
		for _, memory := range item.source.Memories {
			if err != nil {
				break
			}
			newID := identity.New("memory")
			_, err = tx.ExecContext(ctx, `INSERT INTO memories(id,scope_kind,scope_ref,memory_kind,content,source,state,created_at,updated_at,owner_principal_id) VALUES(?,'project',?,?,?,?, 'active',?,?,?)`, newID, item.newID, memory.MemoryKind, memory.Content, memory.Source, formatTime(now), formatTime(now), ownerID)
			if err == nil {
				_, err = tx.ExecContext(ctx, `INSERT INTO workspace_migration_maps(id,job_id,object_kind,source_id,destination_id,created_at) VALUES(?,?,?,?,?,?)`, identity.New("migrationmap"), jobID, "memory", memory.SourceID, newID, formatTime(now))
			}
		}
	}
	if err == nil {
		for index := range staged {
			if renameErr := os.Rename(staged[index].staging, staged[index].destination); renameErr != nil {
				err = renameErr
				break
			}
			staged[index].renamed = true
		}
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE workspace_migration_jobs SET state='completed',completed_at=? WHERE id=? AND state='running'`, formatTime(now), jobID)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE workspace_migration_jobs SET state='completed',completed_at=? WHERE id=? AND state='awaiting_apply'`, formatTime(now), input.PreviewID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		cleanup(true)
		return WorkspaceMigration{}, err
	}
	item := WorkspaceMigration{ID: jobID, Kind: "import_apply", State: "completed", FormatVersion: workspaceFormatVersion, SourcePrincipalID: sourcePrincipal, DestinationPrincipalID: ownerID, Summary: summary, CreatedAt: now, CompletedAt: &now}
	return item, nil
}

func (s *Service) GetWorkspaceMigration(ctx context.Context, id string) (WorkspaceMigration, error) {
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return WorkspaceMigration{}, err
	}
	var item WorkspaceMigration
	var summary, created string
	var completed sql.NullString
	err = s.store.DB.QueryRowContext(ctx, `SELECT id,kind,state,format_version,COALESCE(package_checksum,''),COALESCE(source_principal_id,''),COALESCE(destination_principal_id,''),summary_json,error,created_at,completed_at FROM workspace_migration_jobs WHERE id=? AND owner_principal_id=?`, id, ownerID).Scan(&item.ID, &item.Kind, &item.State, &item.FormatVersion, &item.PackageChecksum, &item.SourcePrincipalID, &item.DestinationPrincipalID, &summary, &item.Error, &created, &completed)
	if err != nil {
		return item, err
	}
	_ = json.Unmarshal([]byte(summary), &item.Summary)
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if completed.Valid {
		value, _ := time.Parse(time.RFC3339Nano, completed.String)
		item.CompletedAt = &value
	}
	return item, nil
}
func (s *Service) WorkspaceMigrationContent(ctx context.Context, id string) (WorkspaceMigration, []byte, error) {
	item, err := s.GetWorkspaceMigration(ctx, id)
	if err != nil {
		return item, nil, err
	}
	if item.Kind != "export" || item.State != "completed" {
		return item, nil, fmt.Errorf("workspace migration is not a completed export")
	}
	ownerID, _ := s.store.OwnerPrincipalID(ctx)
	var ref string
	if err = s.store.DB.QueryRowContext(ctx, `SELECT package_blob_ref FROM workspace_migration_jobs WHERE id=? AND owner_principal_id=?`, id, ownerID).Scan(&ref); err != nil {
		return item, nil, err
	}
	body, err := s.store.Blobs.Get(ref)
	if err != nil || checksum(body) != item.PackageChecksum {
		return item, nil, errors.New("encrypted workspace package failed integrity check")
	}
	return item, body, nil
}

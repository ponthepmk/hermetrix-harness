package product

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"hermetrix-harness/internal/agent"
	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
)

const maxArtifactBytes = 16 << 20

var settingKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,80}$`)

type Service struct {
	store       *store.Store
	skills      *skills.Service
	mu          sync.Mutex
	cancels     map[string]context.CancelFunc
	terminals   map[string]*terminalRuntime
	browser     *browserRuntime
	browserTabs map[string]*browserTabRuntime
	agent       teamAgentRunner
	teamCtx     context.Context
	teamCancel  context.CancelFunc
	teamRuns    map[string]teamRunHandle
	teamWG      sync.WaitGroup
}

type teamAgentRunner interface {
	CreateSession(context.Context, agent.CreateSessionInput) (agent.Session, error)
	RunTurn(context.Context, string, agent.TurnInput, func(agent.StreamEvent) error) (agent.TurnResult, error)
	DecideApproval(context.Context, string, agent.ApprovalDecisionInput, func(agent.StreamEvent) error) (agent.TurnResult, error)
}

type teamRunHandle struct {
	Generation string
	Cancel     context.CancelFunc
}

func NewService(dataStore *store.Store, skillService *skills.Service) *Service {
	teamCtx, teamCancel := context.WithCancel(context.Background())
	return &Service{store: dataStore, skills: skillService, cancels: map[string]context.CancelFunc{},
		terminals: map[string]*terminalRuntime{}, browserTabs: map[string]*browserTabRuntime{},
		teamCtx: teamCtx, teamCancel: teamCancel, teamRuns: map[string]teamRunHandle{}}
}

func (s *Service) WithAgentRunner(runner teamAgentRunner) *Service {
	s.agent = runner
	return s
}

func (s *Service) RecoverInterruptedJobs(ctx context.Context) (int64, error) {
	result, err := s.store.DB.ExecContext(ctx, `UPDATE background_jobs SET state='interrupted',error='process interrupted; command was not retried',
      completed_at=? WHERE state IN ('queued','running')`, formatTime(time.Now().UTC()))
	if err != nil {
		return 0, err
	}
	jobs, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	terminals, err := s.store.DB.ExecContext(ctx, `UPDATE terminal_sessions SET state='interrupted',
		error='terminal process ended when Hermetrix stopped',updated_at=?,completed_at=? WHERE state='running'`,
		formatTime(time.Now().UTC()), formatTime(time.Now().UTC()))
	if err != nil {
		return jobs, err
	}
	terminalCount, err := terminals.RowsAffected()
	if err != nil {
		return jobs + terminalCount, err
	}
	browserTabs, err := s.store.DB.ExecContext(ctx, `UPDATE browser_tabs SET state='interrupted',
		error='browser tab ended when Hermetrix stopped',updated_at=? WHERE state IN ('starting','ready','navigating')`,
		formatTime(time.Now().UTC()))
	if err != nil {
		return jobs + terminalCount, err
	}
	browserCount, err := browserTabs.RowsAffected()
	if err != nil {
		return jobs + terminalCount, err
	}
	teamTasks, err := s.store.DB.ExecContext(ctx, `UPDATE agent_team_tasks SET state='interrupted',
		error='team task interrupted; model/tool effects were not retried',completed_at=?
		WHERE state IN ('running','resolving_approval') OR (state='queued' AND EXISTS (
			SELECT 1 FROM agent_team_runs WHERE agent_team_runs.id=agent_team_tasks.run_id
			AND agent_team_runs.state IN ('queued','running')))`,
		formatTime(time.Now().UTC()))
	if err != nil {
		return jobs + terminalCount + browserCount, err
	}
	taskCount, err := teamTasks.RowsAffected()
	if err != nil {
		return jobs + terminalCount + browserCount, err
	}
	teamRuns, err := s.store.DB.ExecContext(ctx, `UPDATE agent_team_runs SET state='interrupted',
		error='team run interrupted; inspect child sessions before starting a new run',completed_at=?
		WHERE state IN ('queued','running') OR (state='awaiting_approval' AND EXISTS (
			SELECT 1 FROM agent_team_tasks WHERE agent_team_tasks.run_id=agent_team_runs.id AND agent_team_tasks.state='interrupted'))`,
		formatTime(time.Now().UTC()))
	if err != nil {
		return jobs + terminalCount + browserCount + taskCount, err
	}
	runCount, err := teamRuns.RowsAffected()
	return jobs + terminalCount + browserCount + taskCount + runCount, err
}

func (s *Service) Close() {
	s.teamCancel()
	s.teamWG.Wait()
	s.closeTerminals()
	s.closeBrowser()
}

func (s *Service) SaveProject(ctx context.Context, input ProjectInput) (Project, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 100 {
		return Project{}, fmt.Errorf("project name is required and must be at most 100 characters")
	}
	root, err := resolveProjectRoot(input.RootPath)
	if err != nil {
		return Project{}, err
	}
	now := time.Now().UTC()
	if input.ID == "" {
		input.ID = identity.New("project")
		_, err = s.store.DB.ExecContext(ctx, `INSERT INTO projects(id,name,root_path,state,created_at,updated_at)
      VALUES(?,?,?,'active',?,?)`, input.ID, input.Name, root, formatTime(now), formatTime(now))
	} else {
		var changed sql.Result
		changed, err = s.store.DB.ExecContext(ctx, `UPDATE projects SET name=?,root_path=?,updated_at=? WHERE id=?`,
			input.Name, root, formatTime(now), input.ID)
		if err == nil {
			if count, _ := changed.RowsAffected(); count != 1 {
				err = sql.ErrNoRows
			}
		}
	}
	if err != nil {
		return Project{}, err
	}
	return s.GetProject(ctx, input.ID)
}

func (s *Service) EnsureWorkspaceProject(ctx context.Context, root string) (Project, error) {
	resolved, err := resolveProjectRoot(root)
	if err != nil {
		return Project{}, err
	}
	var id string
	if err := s.store.DB.QueryRowContext(ctx, `SELECT id FROM projects WHERE root_path=?`, resolved).Scan(&id); err == nil {
		return s.GetProject(ctx, id)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Project{}, err
	}
	name := filepath.Base(resolved)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "Workspace"
	}
	return s.SaveProject(ctx, ProjectInput{Name: name, RootPath: resolved})
}

func (s *Service) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,name,root_path,state,pinned,last_opened_at,created_at,updated_at FROM projects ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Project{}
	for rows.Next() {
		item, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetProject(ctx context.Context, id string) (Project, error) {
	return scanProject(s.store.DB.QueryRowContext(ctx, `SELECT id,name,root_path,state,pinned,last_opened_at,created_at,updated_at FROM projects WHERE id=?`, id))
}

func (s *Service) BrowseProject(ctx context.Context, projectID, relative string) ([]FileEntry, error) {
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	path, err := resolveInside(project.RootPath, relative, true)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	if len(entries) > 500 {
		entries = entries[:500]
	}
	items := make([]FileEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(project.RootPath, filepath.Join(path, entry.Name()))
		items = append(items, FileEntry{Name: entry.Name(), Path: filepath.ToSlash(rel), Directory: entry.IsDir(),
			Symlink: entry.Type()&os.ModeSymlink != 0, Bytes: info.Size(), ModifiedAt: info.ModTime().UTC()})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Directory != items[j].Directory {
			return items[i].Directory
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (s *Service) CreateArtifact(ctx context.Context, input ArtifactInput) (Artifact, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Kind = strings.TrimSpace(input.Kind)
	input.MIMEType = strings.TrimSpace(input.MIMEType)
	if input.Name == "" || input.Kind == "" || input.MIMEType == "" || len(input.Content) > maxArtifactBytes {
		return Artifact{}, fmt.Errorf("artifact name, kind, mime_type and content up to 16 MiB are required")
	}
	if input.ProjectID != "" {
		if _, err := s.GetProject(ctx, input.ProjectID); err != nil {
			return Artifact{}, err
		}
	}
	ref, err := s.store.Blobs.Put([]byte(input.Content))
	if err != nil {
		return Artifact{}, err
	}
	now := time.Now().UTC()
	item := Artifact{ID: identity.New("artifact"), ProjectID: input.ProjectID, SessionID: input.SessionID,
		Name: input.Name, Kind: input.Kind, MIMEType: input.MIMEType, BlobRef: ref, ByteSize: len(input.Content),
		Checksum: ref, Metadata: input.Metadata, CreatedAt: now}
	metadata, _ := json.Marshal(item.Metadata)
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO artifacts(id,project_id,session_id,name,kind,mime_type,blob_ref,
      byte_size,checksum,metadata_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, item.ID, nullIfEmpty(item.ProjectID),
		nullIfEmpty(item.SessionID), item.Name, item.Kind, item.MIMEType, item.BlobRef, item.ByteSize, item.Checksum,
		string(metadata), formatTime(now))
	return item, err
}

func (s *Service) ListArtifacts(ctx context.Context, projectID string) ([]Artifact, error) {
	query := `SELECT id,COALESCE(project_id,''),COALESCE(session_id,''),name,kind,mime_type,blob_ref,byte_size,
    checksum,metadata_json,created_at FROM artifacts`
	args := []any{}
	if projectID != "" {
		query += ` WHERE project_id=?`
		args = append(args, projectID)
	}
	query += ` ORDER BY created_at DESC LIMIT 500`
	rows, err := s.store.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Artifact{}
	for rows.Next() {
		item, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetArtifact(ctx context.Context, id string) (Artifact, []byte, error) {
	item, err := scanArtifact(s.store.DB.QueryRowContext(ctx, `SELECT id,COALESCE(project_id,''),COALESCE(session_id,''),
    name,kind,mime_type,blob_ref,byte_size,checksum,metadata_json,created_at FROM artifacts WHERE id=?`, id))
	if err != nil {
		return Artifact{}, nil, err
	}
	data, err := s.store.Blobs.Get(item.BlobRef)
	return item, data, err
}

func (s *Service) SaveSetting(ctx context.Context, key string, value any) (Setting, error) {
	key = strings.TrimSpace(key)
	if !settingKeyPattern.MatchString(key) || containsSecretField(value) {
		return Setting{}, fmt.Errorf("invalid setting key or secret-like value")
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 64<<10 {
		return Setting{}, fmt.Errorf("setting must be valid JSON up to 64 KiB")
	}
	now := time.Now().UTC()
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO settings(key,value_json,updated_at) VALUES(?,?,?)
    ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json,updated_at=excluded.updated_at`, key,
		string(encoded), formatTime(now))
	return Setting{Key: key, Value: value, UpdatedAt: now}, err
}

func (s *Service) ListSettings(ctx context.Context) ([]Setting, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT key,value_json,updated_at FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Setting{}
	for rows.Next() {
		var item Setting
		var raw, updated string
		if err := rows.Scan(&item.Key, &raw, &updated); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &item.Value)
		item.UpdatedAt, _ = parseTime(updated)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) SaveMemory(ctx context.Context, input MemoryInput) (Memory, error) {
	input.ScopeKind = strings.TrimSpace(input.ScopeKind)
	input.ScopeRef = strings.TrimSpace(input.ScopeRef)
	input.MemoryKind = strings.TrimSpace(input.MemoryKind)
	input.Content = strings.TrimSpace(input.Content)
	input.Source = strings.TrimSpace(input.Source)
	if input.Source != "user" || (input.ScopeKind != "user" && input.ScopeKind != "project") ||
		input.MemoryKind == "" || input.Content == "" || len(input.Content) > 64<<10 {
		return Memory{}, fmt.Errorf("only explicit user memory in user/project scope up to 64 KiB can become active")
	}
	if input.ScopeKind == "project" {
		if _, err := s.GetProject(ctx, input.ScopeRef); err != nil {
			return Memory{}, err
		}
	}
	now := time.Now().UTC()
	item := Memory{ID: identity.New("memory"), ScopeKind: input.ScopeKind, ScopeRef: input.ScopeRef,
		MemoryKind: input.MemoryKind, Content: input.Content, Source: input.Source, State: "active", CreatedAt: now, UpdatedAt: now}
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO memories(id,scope_kind,scope_ref,memory_kind,content,source,state,
    created_at,updated_at) VALUES(?,?,?,?,?,?,'active',?,?)`, item.ID, item.ScopeKind, item.ScopeRef, item.MemoryKind,
		item.Content, item.Source, formatTime(now), formatTime(now))
	return item, err
}

func (s *Service) ListMemories(ctx context.Context, scopeKind, scopeRef string) ([]Memory, error) {
	query := `SELECT id,scope_kind,scope_ref,memory_kind,content,source,state,created_at,updated_at FROM memories WHERE 1=1`
	args := []any{}
	if scopeKind != "" {
		query += ` AND scope_kind=?`
		args = append(args, scopeKind)
	}
	if scopeRef != "" {
		query += ` AND scope_ref=?`
		args = append(args, scopeRef)
	}
	query += ` ORDER BY updated_at DESC`
	rows, err := s.store.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Memory{}
	for rows.Next() {
		var item Memory
		var created, updated string
		if err := rows.Scan(&item.ID, &item.ScopeKind, &item.ScopeRef, &item.MemoryKind, &item.Content, &item.Source,
			&item.State, &created, &updated); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = parseTime(created)
		item.UpdatedAt, _ = parseTime(updated)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ArchiveMemory(ctx context.Context, id string) error {
	result, err := s.store.DB.ExecContext(ctx, `UPDATE memories SET state='archived',updated_at=? WHERE id=? AND state='active'`,
		formatTime(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Service) Usage(ctx context.Context) (UsageSummary, error) {
	var summary UsageSummary
	if err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_sessions`).Scan(&summary.Sessions); err != nil {
		return summary, err
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT event_kind,metadata_json,content FROM agent_events ORDER BY created_at`)
	if err != nil {
		return summary, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, metadataJSON, content string
		if err := rows.Scan(&kind, &metadataJSON, &content); err != nil {
			return summary, err
		}
		switch kind {
		case "model_step_bound":
			summary.ModelSteps++
		case "tool_call":
			summary.ToolCalls++
		case "tool_result":
			var receipt struct {
				Status string `json:"status"`
			}
			if json.Unmarshal([]byte(content), &receipt) == nil && receipt.Status == "succeeded" {
				summary.ToolSucceeded++
			} else {
				summary.ToolFailed++
			}
		}
		var metadata struct {
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(metadataJSON), &metadata) == nil {
			summary.PromptTokens += metadata.Usage.PromptTokens
			summary.CompletionTokens += metadata.Usage.CompletionTokens
			summary.TotalTokens += metadata.Usage.TotalTokens
		}
	}
	return summary, rows.Err()
}

func resolveProjectRoot(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	// Empty is a project with no code, which is an ordinary kind of project.
	if trimmed == "" {
		return "", nil
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("project root must be an existing directory")
	}
	return real, nil
}

func resolveInside(root, relative string, mustExist bool) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes project root")
	}
	joined := filepath.Join(root, clean)
	real := joined
	var err error
	if mustExist {
		real, err = filepath.EvalSymlinks(joined)
		if err != nil {
			return "", err
		}
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes project root")
	}
	return real, nil
}

func containsSecretField(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "api_key") {
				return true
			}
			if containsSecretField(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if containsSecretField(nested) {
				return true
			}
		}
	}
	return false
}

type projectScanner interface{ Scan(...any) error }

func scanProject(row projectScanner) (Project, error) {
	var item Project
	var pinned int
	var lastOpened sql.NullString
	var created, updated string
	if err := row.Scan(&item.ID, &item.Name, &item.RootPath, &item.State, &pinned, &lastOpened, &created, &updated); err != nil {
		return Project{}, err
	}
	item.Pinned = pinned != 0
	if lastOpened.Valid {
		value, _ := parseTime(lastOpened.String)
		item.LastOpenedAt = &value
	}
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

type artifactScanner interface{ Scan(...any) error }

func scanArtifact(row artifactScanner) (Artifact, error) {
	var item Artifact
	var metadata, created string
	if err := row.Scan(&item.ID, &item.ProjectID, &item.SessionID, &item.Name, &item.Kind, &item.MIMEType,
		&item.BlobRef, &item.ByteSize, &item.Checksum, &metadata, &created); err != nil {
		return Artifact{}, err
	}
	_ = json.Unmarshal([]byte(metadata), &item.Metadata)
	item.CreatedAt, _ = parseTime(created)
	return item, nil
}

func copyLimited(reader io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("payload exceeds %d bytes", max)
	}
	return data, nil
}

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func formatTime(value time.Time) string         { return value.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }

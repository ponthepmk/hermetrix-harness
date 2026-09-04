package product

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hermetrix-harness/internal/identity"
)

const maxCommandOutput = 2 << 20

// maxCommandTimeoutSeconds is ten minutes. The old ceiling was two, which is
// under the time a full build or test suite takes on a real project, and the
// agent is expected to run exactly those. The default stays at 30 seconds, so
// nothing that did not ask for a longer wait gets one.
const maxCommandTimeoutSeconds = 600

var allowedExecutables = map[string]bool{
	"go": true, "git": true, "node": true, "npm": true, "python3": true, "rg": true, "ls": true,
}

func (s *Service) StartCommand(ctx context.Context, input CommandInput) (Job, error) {
	input.Actor = strings.TrimSpace(input.Actor)
	input.Executable = strings.TrimSpace(input.Executable)
	if input.Actor == "" || !allowedExecutables[input.Executable] || filepath.Base(input.Executable) != input.Executable {
		return Job{}, fmt.Errorf("actor and an allowed executable without a path are required")
	}
	if len(input.Arguments) > 64 || containsSensitiveArgument(input.Arguments) {
		return Job{}, fmt.Errorf("command arguments are invalid or appear to contain credentials")
	}
	for _, argument := range input.Arguments {
		if len(argument) > 8192 || strings.ContainsRune(argument, 0) {
			return Job{}, fmt.Errorf("command argument exceeds safety bounds")
		}
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 30
	}
	if input.TimeoutSeconds < 1 || input.TimeoutSeconds > maxCommandTimeoutSeconds {
		return Job{}, fmt.Errorf("timeout_seconds must be between 1 and %d", maxCommandTimeoutSeconds)
	}
	project, err := s.GetProject(ctx, input.ProjectID)
	if err != nil {
		return Job{}, err
	}
	root, err := requireRoot(project)
	if err != nil {
		return Job{}, err
	}
	workingDir, err := resolveInside(root, input.WorkingDir, true)
	if err != nil {
		return Job{}, err
	}
	info, err := os.Stat(workingDir)
	if err != nil || !info.IsDir() {
		return Job{}, fmt.Errorf("working directory must be an existing directory")
	}
	resolvedExecutable, err := exec.LookPath(input.Executable)
	if err != nil {
		return Job{}, err
	}
	now := time.Now().UTC()
	payload := map[string]any{"project_id": project.ID, "actor": input.Actor, "executable": input.Executable,
		"arguments": input.Arguments, "working_dir": input.WorkingDir, "timeout_seconds": input.TimeoutSeconds,
		"shell": false, "environment": "minimal"}
	payloadJSON, _ := json.Marshal(payload)
	job := Job{ID: identity.New("job"), Kind: "command", State: "queued", Payload: payload, Result: map[string]any{}, CreatedAt: now}
	if _, err := s.store.DB.ExecContext(ctx, `INSERT INTO background_jobs(id,kind,state,progress,payload_json,created_at)
    VALUES(?,'command','queued',0,?,?)`, job.ID, string(payloadJSON), formatTime(now)); err != nil {
		return Job{}, err
	}
	jobCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.mu.Lock()
	s.cancels[job.ID] = cancel
	s.mu.Unlock()
	go s.runCommand(jobCtx, job, project, resolvedExecutable, input)
	return job, nil
}

func (s *Service) runCommand(parent context.Context, job Job, project Project, executable string, input CommandInput) {
	started := time.Now().UTC()
	_, _ = s.store.DB.ExecContext(context.Background(), `UPDATE background_jobs SET state='running',progress=0.1,started_at=?
    WHERE id=? AND state='queued'`, formatTime(started), job.ID)
	ctx, cancel := context.WithTimeout(parent, time.Duration(input.TimeoutSeconds)*time.Second)
	defer cancel()
	// project.RootPath is already known non-empty: StartCommand required it
	// before this goroutine was ever launched. input.WorkingDir is a
	// different story: StartCommand resolved it once, before this goroutine
	// was even scheduled, and that resolution can go stale before this
	// second one runs -- the directory deleted out from under it, a symlink
	// inside it retargeted. resolveInside returns ("", err) on every failure
	// path, and exec.Cmd treats an empty Dir as "inherit this server
	// process's own working directory": an unchecked error here would let
	// the command run outside the project entirely, silently, with nothing
	// recorded. So the error is checked, and turns the job into a recorded
	// failure instead of a command running somewhere no one chose.
	workingDir, workingDirErr := resolveInside(project.RootPath, input.WorkingDir, true)
	if workingDirErr != nil {
		errorMessage := "working directory could not be resolved: " + workingDirErr.Error()
		resultJSON, _ := json.Marshal(map[string]any{})
		completed := time.Now().UTC()
		_, _ = s.store.DB.ExecContext(context.Background(), `UPDATE background_jobs SET state='failed',progress=1,result_json=?,error=?,
    completed_at=? WHERE id=?`, string(resultJSON), errorMessage, formatTime(completed), job.ID)
		s.mu.Lock()
		delete(s.cancels, job.ID)
		s.mu.Unlock()
		return
	}
	command := exec.CommandContext(ctx, executable, input.Arguments...)
	command.Dir = workingDir
	command.Env = minimalEnvironment()
	subtreeTerminated := configureProcessTermination(command)
	command.WaitDelay = 2 * time.Second
	buffer := &boundedBuffer{limit: maxCommandOutput}
	command.Stdout, command.Stderr = buffer, buffer
	runStarted := time.Now()
	err := command.Run()
	duration := time.Since(runStarted)
	output := buffer.String()
	exitCode := 0
	state := "completed"
	errorMessage := ""
	if err != nil {
		exitCode = -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			exitCode = exit.ExitCode()
		}
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			state, errorMessage = "failed", "command timed out and was terminated"
		case errors.Is(ctx.Err(), context.Canceled):
			state, errorMessage = "canceled", "command canceled and was terminated"
		default:
			state, errorMessage = "failed", err.Error()
		}
	}
	artifact, artifactErr := s.CreateArtifact(context.Background(), ArtifactInput{ProjectID: project.ID,
		Name: "command-" + job.ID + ".txt", Kind: "terminal_log", MIMEType: "text/plain; charset=utf-8", Content: output,
		Metadata: map[string]any{"job_id": job.ID, "executable": input.Executable, "exit_code": exitCode,
			"truncated": buffer.truncated}})
	result := map[string]any{"exit_code": exitCode, "duration_ms": duration.Milliseconds(), "output": output,
		"truncated": buffer.truncated, "process_group_terminated_on_cancel": subtreeTerminated}
	if artifactErr == nil {
		result["artifact_id"] = artifact.ID
	} else if errorMessage == "" {
		errorMessage = "persist command artifact: " + artifactErr.Error()
		state = "failed"
	}
	resultJSON, _ := json.Marshal(result)
	completed := time.Now().UTC()
	_, _ = s.store.DB.ExecContext(context.Background(), `UPDATE background_jobs SET state=?,progress=1,result_json=?,error=?,
    completed_at=? WHERE id=?`, state, string(resultJSON), errorMessage, formatTime(completed), job.ID)
	s.mu.Lock()
	delete(s.cancels, job.ID)
	s.mu.Unlock()
}

func (s *Service) CancelJob(ctx context.Context, id string) (Job, error) {
	result, err := s.store.DB.ExecContext(ctx, `UPDATE background_jobs SET cancel_requested=1 WHERE id=? AND state IN ('queued','running')`, id)
	if err != nil {
		return Job{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Job{}, fmt.Errorf("job is not cancelable")
	}
	s.mu.Lock()
	cancel := s.cancels[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return s.GetJob(ctx, id)
}

func (s *Service) GetJob(ctx context.Context, id string) (Job, error) {
	return scanJob(s.store.DB.QueryRowContext(ctx, `SELECT id,kind,state,progress,payload_json,result_json,error,
    cancel_requested,created_at,started_at,completed_at FROM background_jobs WHERE id=?`, id))
}

func (s *Service) ListJobs(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,kind,state,progress,payload_json,result_json,error,
    cancel_requested,created_at,started_at,completed_at FROM background_jobs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Job{}
	for rows.Next() {
		item, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type boundedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	requested := len(data)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
			b.truncated = true
		}
		_, _ = b.buffer.Write(data)
	} else {
		b.truncated = true
	}
	return requested, nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func minimalEnvironment() []string {
	keys := []string{"PATH", "LANG", "LC_ALL", "TERM", "TMPDIR", "GOCACHE", "GOMODCACHE", "GOPATH", "GOROOT"}
	values := []string{}
	found := map[string]bool{}
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			values = append(values, key+"="+value)
			found[key] = true
		}
	}
	// Go installations commonly derive these from HOME without exporting them.
	// Resolve only the non-secret cache paths so allowlisted Go jobs work while
	// the complete parent environment (and its credentials) remains hidden.
	if userRoot, err := os.UserHomeDir(); err == nil {
		if !found["GOPATH"] {
			values = append(values, "GOPATH="+filepath.Join(userRoot, "go"))
		}
		if !found["GOMODCACHE"] {
			values = append(values, "GOMODCACHE="+filepath.Join(userRoot, "go", "pkg", "mod"))
		}
	}
	if cacheRoot, err := os.UserCacheDir(); err == nil && !found["GOCACHE"] {
		values = append(values, "GOCACHE="+filepath.Join(cacheRoot, "go-build"))
	}
	return values
}

func containsSensitiveArgument(arguments []string) bool {
	for _, argument := range arguments {
		lower := strings.ToLower(argument)
		for _, marker := range []string{"--password", "--token", "--secret", "api_key=", "authorization:"} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	return false
}

type jobScanner interface{ Scan(...any) error }

func scanJob(row jobScanner) (Job, error) {
	var item Job
	var payloadJSON, resultJSON, created string
	var started, completed sql.NullString
	if err := row.Scan(&item.ID, &item.Kind, &item.State, &item.Progress, &payloadJSON, &resultJSON, &item.Error,
		&item.CancelRequested, &created, &started, &completed); err != nil {
		return Job{}, err
	}
	_ = json.Unmarshal([]byte(payloadJSON), &item.Payload)
	_ = json.Unmarshal([]byte(resultJSON), &item.Result)
	item.CreatedAt, _ = parseTime(created)
	if started.Valid {
		value, _ := parseTime(started.String)
		item.StartedAt = &value
	}
	if completed.Valid {
		value, _ := parseTime(completed.String)
		item.CompletedAt = &value
	}
	return item, nil
}

//go:build !windows

package product

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"hermetrix-harness/internal/identity"
)

const terminalTailLimit = 1 << 20

type terminalRuntime struct {
	mu      sync.Mutex
	session TerminalSession
	command *exec.Cmd
	ptmx    *os.File
	tail    []byte
	total   int64
}

func (s *Service) StartTerminal(ctx context.Context, input StartTerminalInput) (TerminalSession, error) {
	input.Actor, input.Shell = strings.TrimSpace(input.Actor), strings.TrimSpace(input.Shell)
	if input.Actor == "" {
		return TerminalSession{}, fmt.Errorf("terminal actor is required")
	}
	if input.Shell == "" {
		input.Shell = "zsh"
	}
	if filepath.Base(input.Shell) != input.Shell || (input.Shell != "zsh" && input.Shell != "bash" && input.Shell != "sh") {
		return TerminalSession{}, fmt.Errorf("terminal shell must be zsh, bash or sh")
	}
	project, err := s.GetProject(ctx, input.ProjectID)
	if err != nil {
		return TerminalSession{}, err
	}
	root, err := requireRoot(project)
	if err != nil {
		return TerminalSession{}, err
	}
	workingDir, err := resolveInside(root, input.WorkingDir, true)
	if err != nil {
		return TerminalSession{}, err
	}
	shellPath, err := exec.LookPath(input.Shell)
	if err != nil {
		return TerminalSession{}, err
	}
	columns, rows := input.Columns, input.Rows
	if columns < 40 || columns > 400 {
		columns = 120
	}
	if rows < 10 || rows > 160 {
		rows = 32
	}
	command := exec.Command(shellPath)
	command.Dir = workingDir
	command.Env = append(minimalEnvironment(), "TERM=xterm-256color", "HERMETRIX_TERMINAL=1")
	ptmx, err := pty.StartWithSize(command, &pty.Winsize{Cols: columns, Rows: rows})
	if err != nil {
		return TerminalSession{}, err
	}
	now := time.Now().UTC()
	session := TerminalSession{ID: identity.New("term"), ProjectID: project.ID, Shell: input.Shell,
		WorkingDir: filepath.ToSlash(input.WorkingDir), State: "running", CreatedAt: now, UpdatedAt: now}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO terminal_sessions(id,project_id,shell,working_dir,state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?)`, session.ID, session.ProjectID, session.Shell, session.WorkingDir, session.State,
		formatTime(now), formatTime(now))
	if err != nil {
		_ = ptmx.Close()
		_ = command.Process.Kill()
		return TerminalSession{}, err
	}
	runtime := &terminalRuntime{session: session, command: command, ptmx: ptmx}
	s.mu.Lock()
	s.terminals[session.ID] = runtime
	s.mu.Unlock()
	go s.captureTerminal(runtime)
	return session, nil
}

func (s *Service) captureTerminal(runtime *terminalRuntime) {
	buffer := make([]byte, 8192)
	for {
		count, err := runtime.ptmx.Read(buffer)
		if count > 0 {
			runtime.mu.Lock()
			runtime.total += int64(count)
			runtime.tail = append(runtime.tail, buffer[:count]...)
			if len(runtime.tail) > terminalTailLimit {
				runtime.tail = append([]byte(nil), runtime.tail[len(runtime.tail)-terminalTailLimit:]...)
			}
			runtime.session.Cursor = runtime.total
			runtime.session.UpdatedAt = time.Now().UTC()
			tail, cursor, updated := string(runtime.tail), runtime.total, runtime.session.UpdatedAt
			runtime.mu.Unlock()
			_, _ = s.store.DB.ExecContext(context.Background(), `UPDATE terminal_sessions SET output_tail=?,cursor=?,updated_at=?
				WHERE id=? AND state='running'`, tail, cursor, formatTime(updated), runtime.session.ID)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				runtime.mu.Lock()
				runtime.session.Error = err.Error()
				runtime.mu.Unlock()
			}
			break
		}
	}
	waitErr := runtime.command.Wait()
	exitCode, state, errorMessage := 0, "completed", ""
	if waitErr != nil {
		state = "failed"
		exitCode = -1
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) {
			exitCode = exit.ExitCode()
		}
		errorMessage = waitErr.Error()
	}
	runtime.mu.Lock()
	if runtime.session.State == "closing" {
		state, errorMessage = "closed", ""
	}
	now := time.Now().UTC()
	runtime.session.State, runtime.session.ExitCode, runtime.session.Error = state, &exitCode, errorMessage
	runtime.session.UpdatedAt, runtime.session.CompletedAt = now, &now
	tail, cursor := string(runtime.tail), runtime.total
	runtime.mu.Unlock()
	_, _ = s.store.DB.ExecContext(context.Background(), `UPDATE terminal_sessions SET state=?,output_tail=?,cursor=?,exit_code=?,
		error=?,updated_at=?,completed_at=? WHERE id=?`, state, tail, cursor, exitCode, errorMessage,
		formatTime(now), formatTime(now), runtime.session.ID)
	_ = runtime.ptmx.Close()
	s.mu.Lock()
	delete(s.terminals, runtime.session.ID)
	s.mu.Unlock()
}

func (s *Service) WriteTerminal(ctx context.Context, id, input string) error {
	if input == "" || len(input) > 64<<10 || strings.IndexByte(input, 0) >= 0 {
		return fmt.Errorf("terminal input must be 1 to 65536 bytes without NUL")
	}
	runtime, err := s.liveTerminal(id)
	if err != nil {
		return err
	}
	_, err = runtime.ptmx.Write([]byte(input))
	return err
}

func (s *Service) ResizeTerminal(ctx context.Context, id string, columns, rows uint16) error {
	if columns < 20 || columns > 500 || rows < 5 || rows > 200 {
		return fmt.Errorf("terminal size is outside safe bounds")
	}
	runtime, err := s.liveTerminal(id)
	if err != nil {
		return err
	}
	return pty.Setsize(runtime.ptmx, &pty.Winsize{Cols: columns, Rows: rows})
}

func (s *Service) CloseTerminal(ctx context.Context, id string) (TerminalSession, error) {
	runtime, err := s.liveTerminal(id)
	if err != nil {
		return TerminalSession{}, err
	}
	runtime.mu.Lock()
	runtime.session.State = "closing"
	runtime.mu.Unlock()
	if runtime.command.Process != nil {
		_ = runtime.command.Process.Signal(syscall.SIGHUP)
	}
	_ = runtime.ptmx.Close()
	return s.GetTerminal(ctx, id)
}

func (s *Service) TerminalOutput(ctx context.Context, id string, cursor int64) (TerminalOutput, error) {
	if runtime, err := s.liveTerminal(id); err == nil {
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		start := runtime.total - int64(len(runtime.tail))
		truncated := cursor < start
		if cursor < start {
			cursor = start
		}
		offset := cursor - start
		if offset < 0 || offset > int64(len(runtime.tail)) {
			offset = int64(len(runtime.tail))
		}
		return TerminalOutput{ID: id, Output: string(runtime.tail[offset:]), Cursor: runtime.total, Truncated: truncated,
			State: runtime.session.State, ExitCode: runtime.session.ExitCode, Error: runtime.session.Error}, nil
	}
	var tail, state, errorMessage string
	var total int64
	var exit sql.NullInt64
	if err := s.store.DB.QueryRowContext(ctx, `SELECT output_tail,cursor,state,exit_code,error FROM terminal_sessions WHERE id=?`, id).
		Scan(&tail, &total, &state, &exit, &errorMessage); err != nil {
		return TerminalOutput{}, err
	}
	start := total - int64(len(tail))
	truncated := cursor < start
	if cursor < start {
		cursor = start
	}
	offset := cursor - start
	if offset < 0 || offset > int64(len(tail)) {
		offset = int64(len(tail))
	}
	var exitCode *int
	if exit.Valid {
		value := int(exit.Int64)
		exitCode = &value
	}
	return TerminalOutput{ID: id, Output: tail[offset:], Cursor: total, Truncated: truncated, State: state,
		ExitCode: exitCode, Error: errorMessage}, nil
}

func (s *Service) ListTerminals(ctx context.Context, limit int) ([]TerminalSession, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.store.DB.QueryContext(ctx, terminalSelect+` ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TerminalSession
	for rows.Next() {
		item, err := scanTerminal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) GetTerminal(ctx context.Context, id string) (TerminalSession, error) {
	return scanTerminal(s.store.DB.QueryRowContext(ctx, terminalSelect+` WHERE id=?`, id))
}

func (s *Service) liveTerminal(id string) (*terminalRuntime, error) {
	s.mu.Lock()
	runtime := s.terminals[id]
	s.mu.Unlock()
	if runtime == nil {
		return nil, fmt.Errorf("terminal is not running")
	}
	return runtime, nil
}

func (s *Service) closeTerminals() {
	s.mu.Lock()
	runtimes := make([]*terminalRuntime, 0, len(s.terminals))
	for _, runtime := range s.terminals {
		runtimes = append(runtimes, runtime)
	}
	s.mu.Unlock()
	for _, runtime := range runtimes {
		if runtime.command.Process != nil {
			_ = runtime.command.Process.Kill()
		}
		_ = runtime.ptmx.Close()
	}
}

const terminalSelect = `SELECT id,project_id,shell,working_dir,state,cursor,exit_code,error,created_at,updated_at,completed_at
	FROM terminal_sessions`

type terminalScanner interface{ Scan(...any) error }

func scanTerminal(row terminalScanner) (TerminalSession, error) {
	var item TerminalSession
	var exit sql.NullInt64
	var created, updated string
	var completed sql.NullString
	if err := row.Scan(&item.ID, &item.ProjectID, &item.Shell, &item.WorkingDir, &item.State, &item.Cursor, &exit,
		&item.Error, &created, &updated, &completed); err != nil {
		return TerminalSession{}, err
	}
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	if exit.Valid {
		value := int(exit.Int64)
		item.ExitCode = &value
	}
	if completed.Valid {
		value, _ := parseTime(completed.String)
		item.CompletedAt = &value
	}
	return item, nil
}

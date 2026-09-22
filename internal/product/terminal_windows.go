//go:build windows

package product

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"hermetrix-harness/internal/identity"
)

const terminalTailLimit = 1 << 20

type terminalRuntime struct {
	mu            sync.Mutex
	session       TerminalSession
	input, output *os.File
	// controlMu serializes resize/termination against native handle teardown.
	controlMu             sync.Mutex
	console, process, job windows.Handle
	processID             uint32
	stopOnce              sync.Once
	done                  chan struct{}
	tail                  []byte
	total                 int64
	persistErr, readErr   error
}

func windowsTerminalAvailable() bool {
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	for _, name := range []string{"CreatePseudoConsole", "ResizePseudoConsole", "ClosePseudoConsole"} {
		if kernel.NewProc(name).Find() != nil {
			return false
		}
	}
	return true
}

func (s *Service) StartTerminal(ctx context.Context, input StartTerminalInput) (TerminalSession, error) {
	if err := ctx.Err(); err != nil {
		return TerminalSession{}, err
	}
	if s.teamCtx.Err() != nil {
		return TerminalSession{}, fmt.Errorf("service is shutting down")
	}
	input.Actor, input.Shell = strings.TrimSpace(input.Actor), strings.TrimSpace(input.Shell)
	if input.Actor == "" {
		return TerminalSession{}, fmt.Errorf("terminal actor is required")
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
	if info, statErr := os.Stat(workingDir); statErr != nil || !info.IsDir() {
		return TerminalSession{}, fmt.Errorf("terminal working directory must be an existing directory")
	}
	if !windowsTerminalAvailable() {
		return TerminalSession{}, ErrInteractiveTerminalUnavailable
	}
	if input.Shell == "" {
		input.Shell = "powershell.exe"
	}
	shell := strings.ToLower(input.Shell)
	if filepath.Base(shell) != shell || strings.ContainsAny(shell, "/\\:") {
		return TerminalSession{}, fmt.Errorf("terminal shell must be a program name")
	}
	args := []string{}
	switch strings.TrimSuffix(shell, ".exe") {
	case "powershell", "pwsh":
		args = []string{"-NoLogo", "-NoProfile"}
	case "cmd":
		args = []string{"/d", "/q"}
	case "sh", "bash", "zsh":
	default:
		return TerminalSession{}, fmt.Errorf("terminal shell must be powershell, pwsh, cmd, sh, bash or zsh")
	}
	shellPath, err := exec.LookPath(shell)
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
	runtime, err := startWindowsTerminal(shellPath, args, workingDir, columns, rows)
	if err != nil {
		return TerminalSession{}, err
	}
	now := time.Now().UTC()
	session := TerminalSession{ID: identity.New("term"), ProjectID: project.ID, Shell: shell,
		WorkingDir: filepath.ToSlash(input.WorkingDir), State: "running", CreatedAt: now, UpdatedAt: now}
	runtime.session = session
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO terminal_sessions(id,project_id,shell,working_dir,state,created_at,updated_at)
        VALUES(?,?,?,?,?,?,?)`, session.ID, session.ProjectID, session.Shell, session.WorkingDir, session.State,
		formatTime(now), formatTime(now))
	if err != nil {
		runtime.discard()
		return TerminalSession{}, err
	}
	s.mu.Lock()
	if s.teamCtx.Err() != nil {
		s.mu.Unlock()
		runtime.discard()
		_, _ = s.store.DB.ExecContext(context.Background(), `UPDATE terminal_sessions SET state='closed',
			updated_at=?,completed_at=? WHERE id=?`, formatTime(time.Now().UTC()), formatTime(time.Now().UTC()), session.ID)
		return TerminalSession{}, fmt.Errorf("service is shutting down")
	}
	s.terminals[session.ID] = runtime
	s.terminalWG.Add(1)
	s.mu.Unlock()
	go s.captureTerminal(runtime)
	// The terminal outlives this HTTP creation request, like the Unix PTY.
	// Explicit CloseTerminal or Service.Close owns cancellation after creation.
	return session, nil
}

// A failed or canceled creation still owns native resources even though it was
// never registered with the service. Drain the final frame before releasing them.
func (runtime *terminalRuntime) discard() {
	drained := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, runtime.output); close(drained) }()
	runtime.stop()
	_, _ = windows.WaitForSingleObject(runtime.process, windows.INFINITE)
	runtime.closeNative()
	<-drained
	_ = runtime.output.Close()
	_ = windows.CloseHandle(runtime.process)
}

// ConPTY hosts the console without creating a desktop console window. Creating
// the shell suspended lets us assign a kill-on-close Job Object before it can
// spawn any descendants. No breakaway flag is enabled; this is process lifetime
// containment, not filesystem or network isolation.
func startWindowsTerminal(shell string, args []string, dir string, columns, rows uint16) (_ *terminalRuntime, err error) {
	runtime := &terminalRuntime{done: make(chan struct{})}
	inputRead, inputWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer inputRead.Close()
	runtime.input = inputWrite
	outputRead, outputWrite, err := os.Pipe()
	if err != nil {
		_ = inputWrite.Close()
		return nil, err
	}
	defer outputWrite.Close()
	runtime.output = outputRead
	// Failure cleanup is also responsible for draining the final console frame.
	defer func() {
		if err == nil {
			return
		}
		drained := make(chan struct{})
		go func() { _, _ = io.Copy(io.Discard, outputRead); close(drained) }()
		if runtime.process != 0 {
			_ = windows.TerminateProcess(runtime.process, 1)
		}
		runtime.closeNative()
		_ = outputWrite.Close()
		_ = inputWrite.Close()
		<-drained
		_ = outputRead.Close()
		if runtime.process != 0 {
			_, _ = windows.WaitForSingleObject(runtime.process, windows.INFINITE)
			_ = windows.CloseHandle(runtime.process)
		}
	}()
	if err = windows.CreatePseudoConsole(windows.Coord{X: int16(columns), Y: int16(rows)},
		windows.Handle(inputRead.Fd()), windows.Handle(outputWrite.Fd()), 0, &runtime.console); err != nil {
		return nil, fmt.Errorf("create Windows pseudoconsole: %w", err)
	}
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, err
	}
	defer attributes.Delete()
	// Unlike most attributes, PSEUDOCONSOLE takes the HPCON value, not its address.
	// Pass that integer handle directly to the native API rather than creating
	// an invalid Go pointer (the typed x/sys Update method expects a pointer).
	updated, _, updateErr := windows.NewLazySystemDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute").Call(
		uintptr(unsafe.Pointer(attributes.List())), 0, windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		uintptr(runtime.console), unsafe.Sizeof(runtime.console), 0, 0)
	if updated == 0 {
		return nil, fmt.Errorf("attach Windows pseudoconsole: %w", updateErr)
	}
	runtime.job, err = windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(runtime.job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return nil, err
	}
	application, err := windows.UTF16PtrFromString(shell)
	if err != nil {
		return nil, err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{shell}, args...)))
	if err != nil {
		return nil, err
	}
	directory, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, err
	}
	environment := append(minimalEnvironment(os.TempDir()), "TERM=xterm-256color", "HERMETRIX_TERMINAL=1")
	sort.Slice(environment, func(i, j int) bool { return strings.ToUpper(environment[i]) < strings.ToUpper(environment[j]) })
	environmentBlock := utf16.Encode([]rune(strings.Join(environment, "\x00") + "\x00\x00"))
	// Explicit null standard handles prevent Windows from duplicating the
	// server's redirected stdout/stdin into the child instead of attaching it
	// to ConPTY. This is also how Windows Terminal starts its console clients.
	startup := windows.StartupInfoEx{StartupInfo: windows.StartupInfo{
		Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})), Flags: windows.STARTF_USESHOWWINDOW | windows.STARTF_USESTDHANDLES, ShowWindow: windows.SW_HIDE},
		ProcThreadAttributeList: attributes.List()}
	process := windows.ProcessInformation{}
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if err = windows.CreateProcess(application, commandLine, nil, nil, false, flags, &environmentBlock[0],
		directory, &startup.StartupInfo, &process); err != nil {
		return nil, err
	}
	runtime.process, runtime.processID = process.Process, process.ProcessId
	defer windows.CloseHandle(process.Thread)
	if err = windows.AssignProcessToJobObject(runtime.job, runtime.process); err != nil {
		return nil, fmt.Errorf("contain terminal process tree: %w", err)
	}
	if _, err = windows.ResumeThread(process.Thread); err != nil {
		return nil, err
	}
	return runtime, nil
}

func (runtime *terminalRuntime) stop() {
	runtime.stopOnce.Do(func() {
		runtime.mu.Lock()
		runtime.session.State = "closing"
		runtime.mu.Unlock()
		runtime.controlMu.Lock()
		if runtime.job != 0 {
			_ = windows.TerminateJobObject(runtime.job, 1)
		}
		runtime.controlMu.Unlock()
		_ = runtime.input.Close()
	})
}

// The reader must remain active during ClosePseudoConsole: ConPTY can write a
// final screen frame while closing. Waiting for EOF before closing deadlocks.
func (runtime *terminalRuntime) closeNative() {
	runtime.controlMu.Lock()
	defer runtime.controlMu.Unlock()
	if runtime.job != 0 {
		_ = windows.CloseHandle(runtime.job)
		runtime.job = 0
	}
	if runtime.console != 0 {
		windows.ClosePseudoConsole(runtime.console)
		runtime.console = 0
	}
}

func (s *Service) captureTerminal(runtime *terminalRuntime) {
	defer s.terminalWG.Done()
	defer close(runtime.done)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buffer := make([]byte, 8192)
		for {
			count, err := runtime.output.Read(buffer)
			if count > 0 {
				runtime.mu.Lock()
				runtime.total += int64(count)
				runtime.tail = append(runtime.tail, buffer[:count]...)
				if len(runtime.tail) > terminalTailLimit {
					runtime.tail = append([]byte(nil), runtime.tail[len(runtime.tail)-terminalTailLimit:]...)
				}
				runtime.session.Cursor, runtime.session.UpdatedAt = runtime.total, time.Now().UTC()
				cursor, updated := runtime.total, runtime.session.UpdatedAt
				runtime.mu.Unlock()
				if persistErr := s.appendTerminalOutput(context.Background(), runtime.session.ID, buffer[:count], cursor, updated); persistErr != nil {
					runtime.mu.Lock()
					runtime.persistErr = errors.Join(runtime.persistErr, persistErr)
					runtime.mu.Unlock()
					slog.Error("terminal output durability write failed", "terminal_id", runtime.session.ID, "error", persistErr)
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) && !errors.Is(err, windows.ERROR_BROKEN_PIPE) {
					runtime.mu.Lock()
					runtime.readErr = err
					runtime.mu.Unlock()
				}
				return
			}
		}
	}()
	_, waitErr := windows.WaitForSingleObject(runtime.process, windows.INFINITE)
	var nativeExit uint32
	waitErr = errors.Join(waitErr, windows.GetExitCodeProcess(runtime.process, &nativeExit))
	runtime.closeNative()
	_ = runtime.input.Close()
	<-readDone
	_ = runtime.output.Close()
	_ = windows.CloseHandle(runtime.process)
	exitCode, state, errorMessage := int(nativeExit), "completed", ""
	runtime.mu.Lock()
	if nativeExit != 0 || waitErr != nil || runtime.readErr != nil {
		state = "failed"
		errorMessage = fmt.Sprintf("terminal exited with code %d", nativeExit)
		if combined := errors.Join(waitErr, runtime.readErr); combined != nil {
			errorMessage += ": " + combined.Error()
		}
	}
	if runtime.session.State == "closing" {
		state, errorMessage = "closed", ""
	}
	if runtime.persistErr != nil {
		state = "failed"
		errorMessage = errors.Join(errors.New(errorMessage), fmt.Errorf("terminal output was not fully persisted: %w", runtime.persistErr)).Error()
	}
	now := time.Now().UTC()
	runtime.session.State, runtime.session.ExitCode, runtime.session.Error = state, &exitCode, errorMessage
	runtime.session.UpdatedAt, runtime.session.CompletedAt = now, &now
	tail, cursor := string(runtime.tail), runtime.total
	runtime.mu.Unlock()
	if _, persistErr := s.store.DB.ExecContext(context.Background(), `UPDATE terminal_sessions SET state=?,output_tail=?,cursor=?,exit_code=?,
        error=?,updated_at=?,completed_at=? WHERE id=?`, state, tail, cursor, exitCode, errorMessage,
		formatTime(now), formatTime(now), runtime.session.ID); persistErr != nil {
		slog.Error("terminal completion durability write failed", "terminal_id", runtime.session.ID, "error", persistErr)
	}
	s.mu.Lock()
	delete(s.terminals, runtime.session.ID)
	s.mu.Unlock()
}

func (s *Service) WriteTerminal(ctx context.Context, id, input string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if input == "" || len(input) > 64<<10 || strings.IndexByte(input, 0) >= 0 {
		return fmt.Errorf("terminal input must be 1 to 65536 bytes without NUL")
	}
	runtime, err := s.liveTerminal(id)
	if err != nil {
		return err
	}
	_, err = runtime.input.Write([]byte(input))
	return err
}

func (s *Service) ResizeTerminal(ctx context.Context, id string, columns, rows uint16) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if columns < 20 || columns > 500 || rows < 5 || rows > 200 {
		return fmt.Errorf("terminal size is outside safe bounds")
	}
	runtime, err := s.liveTerminal(id)
	if err != nil {
		return err
	}
	runtime.controlMu.Lock()
	defer runtime.controlMu.Unlock()
	if runtime.console == 0 {
		return fmt.Errorf("terminal is not running")
	}
	return windows.ResizePseudoConsole(runtime.console, windows.Coord{X: int16(columns), Y: int16(rows)})
}

func (s *Service) CloseTerminal(ctx context.Context, id string) (TerminalSession, error) {
	runtime, err := s.liveTerminal(id)
	if err != nil {
		return s.GetTerminal(ctx, id)
	}
	runtime.stop()
	select {
	case <-runtime.done:
		return s.GetTerminal(ctx, id)
	case <-ctx.Done():
		return TerminalSession{}, ctx.Err()
	}
}

func (s *Service) closeTerminals() {
	s.mu.Lock()
	runtimes := make([]*terminalRuntime, 0, len(s.terminals))
	for _, runtime := range s.terminals {
		runtimes = append(runtimes, runtime)
	}
	s.mu.Unlock()
	for _, runtime := range runtimes {
		runtime.stop()
	}
}

// appendTerminalOutput persists only the newly-read bytes. The previous code
// rebound and rewrote the complete tail after every 8 KiB read, turning a 1 MiB
// terminal into roughly 128 MiB of bound data. SQLite trims the byte suffix in
// place here; the final completion update still writes one full snapshot as a
// reconciliation point.
func (s *Service) appendTerminalOutput(ctx context.Context, terminalID string, chunk []byte, cursor int64, updated time.Time) error {
	result, err := s.store.DB.ExecContext(ctx, `UPDATE terminal_sessions SET
		output_tail=CAST(substr(CAST(COALESCE(output_tail,'') AS BLOB) || CAST(? AS BLOB), -?) AS BLOB),
		cursor=?,updated_at=? WHERE id=? AND state='running'`, chunk, terminalTailLimit, cursor, formatTime(updated), terminalID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("terminal %s is no longer running", terminalID)
	}
	return nil
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

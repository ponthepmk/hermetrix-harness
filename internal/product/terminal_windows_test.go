//go:build windows

package product

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func startWindowsTestTerminal(t *testing.T, shell string) (*Service, TerminalSession, string) {
	t.Helper()
	service, _, _ := testProductService(t)
	root := t.TempDir()
	project, err := service.SaveProject(context.Background(), ProjectInput{Name: "ConPTY", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := service.StartTerminal(context.Background(), StartTerminalInput{ProjectID: project.ID, Shell: shell,
		WorkingDir: ".", Actor: "test-user", Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	return service, terminal, root
}

func waitWindowsTerminalOutput(t *testing.T, service *Service, id string, cursor int64, marker string) TerminalOutput {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		output, err := service.TerminalOutput(context.Background(), id, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.Output, marker) {
			return output
		}
		if time.Now().After(deadline) {
			t.Fatalf("terminal did not produce %q: %+v", marker, output)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWindowsTerminalRealShellCursorResizeClose(t *testing.T) {
	service, session, root := startWindowsTestTerminal(t, "cmd.exe")
	ctx := context.Background()
	runtime, err := service.liveTerminal(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, runtime.processID)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process)
	// The complete marker is never sent as input: seeing it proves shell execution.
	if err := service.WriteTerminal(ctx, session.ID, "set HERMETRIX_MARK=HERMETRIX_\recho %HERMETRIX_MARK%CONPTY_OK\rcd\r"); err != nil {
		t.Fatal(err)
	}
	first := waitWindowsTerminalOutput(t, service, session.ID, 0, "HERMETRIX_CONPTY_OK")
	waitWindowsTerminalOutput(t, service, session.ID, 0, root)
	if err := service.ResizeTerminal(ctx, session.ID, 110, 35); err != nil {
		t.Fatal(err)
	}
	if err := service.ResizeTerminal(ctx, session.ID, 1, 1); err == nil {
		t.Fatal("unsafe dimensions accepted")
	}
	if err := service.WriteTerminal(ctx, session.ID, "echo %HERMETRIX_MARK%SECOND\r"); err != nil {
		t.Fatal(err)
	}
	second := waitWindowsTerminalOutput(t, service, session.ID, first.Cursor, "HERMETRIX_SECOND")
	full, err := service.TerminalOutput(ctx, session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	// A resize legitimately redraws previous screen text. Validate the byte
	// cursor against the captured stream, not repeated visible characters.
	if second.Cursor <= first.Cursor || full.Output[first.Cursor:second.Cursor] != second.Output {
		t.Fatalf("output cursor did not return the exact new byte range: first=%+v second=%+v", first, second)
	}
	closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	closed, err := service.CloseTerminal(closeCtx, session.ID)
	if err != nil || closed.State != "closed" || closed.CompletedAt == nil {
		t.Fatalf("closed=%+v err=%v", closed, err)
	}
	if status, err := windows.WaitForSingleObject(process, 0); err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("process still alive: %d %v", status, err)
	}
	if _, err := service.liveTerminal(session.ID); err == nil {
		t.Fatal("closed terminal remains registered")
	}
	persisted, err := service.TerminalOutput(ctx, session.ID, 0)
	if err != nil || !strings.Contains(persisted.Output, "HERMETRIX_CONPTY_OK") || persisted.Cursor < second.Cursor {
		t.Fatalf("persisted=%+v err=%v", persisted, err)
	}
	if _, err := service.CloseTerminal(ctx, session.ID); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if err := service.WriteTerminal(ctx, session.ID, "echo late\r"); err == nil {
		t.Fatal("closed terminal accepted input")
	}
}

func TestWindowsTerminalDefaultPowerShellAndNaturalExit(t *testing.T) {
	service, session, _ := startWindowsTestTerminal(t, "")
	ctx := context.Background()
	if session.Shell != "powershell.exe" {
		t.Fatalf("default shell=%s", session.Shell)
	}
	runtime, err := service.liveTerminal(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.WriteTerminal(ctx, session.ID, "$part='HERMETRIX_'; Write-Output ($part + 'POWERSHELL_OK')\r"); err != nil {
		t.Fatal(err)
	}
	waitWindowsTerminalOutput(t, service, session.ID, 0, "HERMETRIX_POWERSHELL_OK")
	if err := service.WriteTerminal(ctx, session.ID, "exit 0\r"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.done:
	case <-time.After(10 * time.Second):
		t.Fatal("natural exit did not close ConPTY")
	}
	completed, err := service.GetTerminal(ctx, session.ID)
	if err != nil || completed.State != "completed" || completed.ExitCode == nil || *completed.ExitCode != 0 {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
}

func TestWindowsTerminalServiceShutdownKillsDescendants(t *testing.T) {
	service, session, root := startWindowsTestTerminal(t, "cmd.exe")
	ctx := context.Background()
	// /b prevents a second desktop window. The child remains alive after its shell
	// is idle so shutdown must kill it through the Job Object, not just the root.
	command := `start /b powershell.exe -NoLogo -NoProfile -Command "$PID | Set-Content child.pid; Start-Sleep -Seconds 300"` + "\r"
	if err := service.WriteTerminal(ctx, session.ID, command); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	var childPID int
	for childPID == 0 {
		data, _ := os.ReadFile(filepath.Join(root, "child.pid"))
		childPID, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		if time.Now().After(deadline) {
			t.Fatalf("child PID was not written: %q", data)
		}
		time.Sleep(20 * time.Millisecond)
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(childPID))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(child)
	if status, _ := windows.WaitForSingleObject(child, 0); status != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("fixture child already exited")
	}
	done := make(chan struct{})
	go func() { service.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("service shutdown hung")
	}
	if status, err := windows.WaitForSingleObject(child, 1000); err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("child survived shutdown: %d %v", status, err)
	}
}

func TestWindowsTerminalCreationCancellationAndValidation(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	project, err := service.SaveProject(ctx, ProjectInput{Name: "validation", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	input := StartTerminalInput{ProjectID: project.ID, Shell: "cmd.exe", Actor: "test-user"}
	if _, err := service.StartTerminal(canceled, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled create: %v", err)
	}
	for _, bad := range []StartTerminalInput{
		{ProjectID: project.ID, Shell: "cmd.exe"},
		{ProjectID: project.ID, Actor: "test-user", Shell: "cmd.exe", WorkingDir: "../"},
		{ProjectID: project.ID, Actor: "test-user", Shell: "C:/Windows/System32/cmd.exe"},
		{ProjectID: project.ID, Actor: "test-user", Shell: "not-a-shell"},
	} {
		if _, err := service.StartTerminal(ctx, bad); err == nil {
			t.Fatalf("invalid input accepted: %+v", bad)
		}
	}
	terminals, err := service.ListTerminals(ctx, 50)
	if err != nil || len(terminals) != 0 {
		t.Fatalf("invalid requests created terminals: %+v %v", terminals, err)
	}
	createCtx, cancelCreate := context.WithCancel(ctx)
	session, err := service.StartTerminal(createCtx, input)
	if err != nil {
		t.Fatal(err)
	}
	cancelCreate() // Completing the HTTP create request must not end its session.
	if err := service.WriteTerminal(ctx, session.ID, "set HERMETRIX_MARK=HERMETRIX_\recho %HERMETRIX_MARK%AFTER_REQUEST\r"); err != nil {
		t.Fatal(err)
	}
	waitWindowsTerminalOutput(t, service, session.ID, 0, "HERMETRIX_AFTER_REQUEST")
	for _, invalid := range []string{"", "a\x00b", strings.Repeat("a", (64<<10)+1)} {
		if err := service.WriteTerminal(ctx, session.ID, invalid); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	runtime, err := service.liveTerminal(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Canceling the HTTP close response must not abandon native cleanup.
	_, _ = service.CloseTerminal(canceled, session.ID)
	select {
	case <-runtime.done:
	case <-time.After(10 * time.Second):
		t.Fatal("canceled close abandoned cleanup")
	}
	service.Close()
	if _, err := service.StartTerminal(ctx, input); err == nil {
		t.Fatal("closed service started a terminal")
	}
}

func TestWindowsTerminalCtrlCInterruptsRunningCommand(t *testing.T) {
	service, session, root := startWindowsTestTerminal(t, "")
	ctx := context.Background()
	if err := service.WriteTerminal(ctx, session.ID, "Write-Output ('HERMETRIX_' + 'SLEEPING'); Start-Sleep -Seconds 60\r"); err != nil {
		t.Fatal(err)
	}
	sleeping := waitWindowsTerminalOutput(t, service, session.ID, 0, "HERMETRIX_SLEEPING")
	if err := service.WriteTerminal(ctx, session.ID, "\x03"); err != nil {
		t.Fatal(err)
	}
	waitWindowsTerminalOutput(t, service, session.ID, sleeping.Cursor, "PS "+root+">")
	if err := service.WriteTerminal(ctx, session.ID, "Write-Output ('HERMETRIX_' + 'INTERRUPTED')\r"); err != nil {
		t.Fatal(err)
	}
	waitWindowsTerminalOutput(t, service, session.ID, sleeping.Cursor, "HERMETRIX_INTERRUPTED")
}

func TestWindowsTerminalPromptBeforeInputAndFocusSequence(t *testing.T) {
	service, session, root := startWindowsTestTerminal(t, "")
	ctx := context.Background()
	waitWindowsTerminalOutput(t, service, session.ID, 0, "PS "+root+">")
	for _, input := range []string{"\x1b[I", "Write-Output ('PTY-' + 'E2E')", "\r"} {
		if err := service.WriteTerminal(ctx, session.ID, input); err != nil {
			t.Fatal(err)
		}
	}
	waitWindowsTerminalOutput(t, service, session.ID, 0, "PTY-E2E")
}

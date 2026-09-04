package product

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/agent"
	"hermetrix-harness/internal/agentruntime"
)

func newProductTestServiceWithProject(t *testing.T) (*Service, Project, func()) {
	t.Helper()
	service, _, _ := testProductService(t)
	project, err := service.SaveProject(context.Background(), ProjectInput{Name: "Runtime Bridge", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return service, project, func() {}
}

func TestStartRunAndLookupRunReportTheCommandOutcome(t *testing.T) {
	service, project, cleanup := newProductTestServiceWithProject(t)
	defer cleanup()
	ctx := context.Background()
	started, err := service.StartRun(ctx, agentRunRequest(project.ID))
	if err != nil {
		t.Fatal(err)
	}
	if started.JobID == "" {
		t.Fatal("StartRun returned no job id")
	}
	final := waitForRun(t, service, started.JobID)
	if final.State != "completed" {
		t.Fatalf("state = %q, error = %q", final.State, final.Error)
	}
	if final.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", final.ExitCode)
	}
	if !strings.Contains(final.Output, "hermetrix") {
		t.Fatalf("output = %q, want the echoed marker", final.Output)
	}
}

func TestStartRunRefusesAnExecutableOutsideTheAllowlist(t *testing.T) {
	service, project, cleanup := newProductTestServiceWithProject(t)
	defer cleanup()
	request := agentRunRequest(project.ID)
	request.Executable = "bash"
	if _, err := service.StartRun(context.Background(), request); err == nil {
		t.Fatal("StartRun accepted bash")
	}
}

func TestStartRunAcceptsATenMinuteCeiling(t *testing.T) {
	service, project, cleanup := newProductTestServiceWithProject(t)
	defer cleanup()
	request := agentRunRequest(project.ID)
	request.TimeoutSeconds = 600
	started, err := service.StartRun(context.Background(), request)
	if err != nil {
		t.Fatalf("600 second timeout was refused: %v", err)
	}
	// The command itself (echoing a marker) finishes in milliseconds
	// regardless of the ceiling; wait for it rather than returning while its
	// goroutine is still mid-flight against a store this test's cleanup is
	// about to close and a t.TempDir() about to be removed out from under it.
	waitForRun(t, service, started.JobID)
	request.TimeoutSeconds = 601
	if _, err := service.StartRun(context.Background(), request); err == nil {
		t.Fatal("StartRun accepted a timeout above the ceiling")
	}
}

func agentRunRequest(projectID string) agentruntime.RunRequest {
	return agentruntime.RunRequest{ProjectID: projectID, Actor: "agent:ses_test", Executable: "node",
		Arguments: []string{"-e", "console.log('hermetrix')"}, TimeoutSeconds: 30}
}

func waitForRun(t *testing.T, service *Service, jobID string) agentruntime.RunResult {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		state, err := service.LookupRun(context.Background(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		if state.Done() {
			return state
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job %s never finished", jobID)
	return agentruntime.RunResult{}
}

func mustStart(t *testing.T, service *Service, request agentruntime.RunRequest) agentruntime.RunResult {
	t.Helper()
	result, err := service.StartRun(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestStartRunGivesArgumentsNoShellToLiveIn(t *testing.T) {
	service, project, cleanup := newProductTestServiceWithProject(t)
	defer cleanup()
	request := agentRunRequest(project.ID)
	// If any shell were involved, the semicolon and the pipe would start a
	// second command. exec.Command passes them as one literal argument, so the
	// program sees the punctuation and nothing runs it.
	request.Executable = "node"
	request.Arguments = []string{"-e", `console.log(process.argv[1])`, "; touch pwned | whoami"}
	final := waitForRun(t, service, mustStart(t, service, request).JobID)
	if final.ExitCode != 0 {
		t.Fatalf("exit code = %d, error = %q", final.ExitCode, final.Error)
	}
	if !strings.Contains(final.Output, "; touch pwned | whoami") {
		t.Fatalf("output = %q, want the punctuation echoed back as a literal argument", final.Output)
	}
	if _, err := os.Stat(filepath.Join(project.RootPath, "pwned")); err == nil {
		t.Fatal("a shell ran: the injected command created a file")
	}
}

func TestStartRunRefusesAWorkingDirectoryOutsideTheProject(t *testing.T) {
	service, project, cleanup := newProductTestServiceWithProject(t)
	defer cleanup()
	outside := t.TempDir()
	link := filepath.Join(project.RootPath, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, workingDir := range []string{"..", "../..", outside, "escape"} {
		request := agentRunRequest(project.ID)
		request.WorkingDir = workingDir
		if _, err := service.StartRun(context.Background(), request); err == nil {
			t.Errorf("working_dir %q was accepted", workingDir)
		}
	}
}

func TestStartRunAcceptsASubdirectoryOfTheProject(t *testing.T) {
	service, project, cleanup := newProductTestServiceWithProject(t)
	defer cleanup()
	if err := os.MkdirAll(filepath.Join(project.RootPath, "internal", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	request := agentRunRequest(project.ID)
	request.WorkingDir = "internal/web"
	started, err := service.StartRun(context.Background(), request)
	if err != nil {
		t.Fatalf("a directory inside the project was refused: %v", err)
	}
	// Route through waitForRun rather than returning right after StartRun:
	// otherwise the node process and its result-writing goroutine outlive
	// this test, racing t.TempDir()'s removal of their own cwd and writing to
	// a store this test's cleanup is about to close.
	final := waitForRun(t, service, started.JobID)
	if final.ExitCode != 0 {
		t.Fatalf("exit code = %d, error = %q", final.ExitCode, final.Error)
	}
}

func TestStartRunTerminatesACommandThatWillNotFinish(t *testing.T) {
	service, project, cleanup := newProductTestServiceWithProject(t)
	defer cleanup()
	request := agentRunRequest(project.ID)
	request.Executable = "node"
	request.Arguments = []string{"-e", "setInterval(() => {}, 1000)"}
	request.TimeoutSeconds = 1
	final := waitForRun(t, service, mustStart(t, service, request).JobID)
	if final.State != "failed" {
		t.Fatalf("state = %q, want failed", final.State)
	}
	if !strings.Contains(final.Error, "timed out") {
		t.Fatalf("error = %q, want it to say the command timed out", final.Error)
	}
	// Neither assertion above is evidence about a process: both come from
	// ctx.Err() at the 1 second deadline regardless of whether the kill
	// reaches anything. This one distinguishes a wired Cancel from a no-op
	// one: with Cancel neutered, Cmd's WaitDelay fallback still ends the run,
	// but only after its own 2 second grace period on top of the timeout, so
	// a duration well under that boundary is evidence the kill fired
	// promptly rather than falling back to it.
	if final.DurationMS >= 2000 {
		t.Fatalf("duration = %dms, want comfortably under the 2s WaitDelay grace period", final.DurationMS)
	}
}

func TestLookupRunSaysWhenThereIsNoSuchJob(t *testing.T) {
	service, _, cleanup := newProductTestServiceWithProject(t)
	defer cleanup()
	_, err := service.LookupRun(context.Background(), "job_does_not_exist")
	if err == nil {
		t.Fatal("LookupRun invented a state for a job that does not exist")
	}
	// internal/agent's isJobNotFound is errors.Is(err, sql.ErrNoRows): that
	// classification decides whether a stale job frees a concurrency slot. If
	// GetJob ever wrapped this error, this bare err != nil check would stay
	// green while the agent-side sweep silently stopped recognizing missing
	// jobs.
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("error = %v, want it to wrap sql.ErrNoRows", err)
	}
}

// TestStartRunBoundsOutputAtTheCeiling proves the output ceiling actually
// bounds bytes, not just a flag: a command that writes well past
// maxCommandOutput must come back with output no longer than the ceiling
// itself, with Truncated set to say why it is short.
func TestStartRunBoundsOutputAtTheCeiling(t *testing.T) {
	service, project, cleanup := newProductTestServiceWithProject(t)
	defer cleanup()
	request := agentRunRequest(project.ID)
	request.Executable = "node"
	request.Arguments = []string{"-e", fmt.Sprintf("process.stdout.write('x'.repeat(%d))", maxCommandOutput*2)}
	final := waitForRun(t, service, mustStart(t, service, request).JobID)
	if final.ExitCode != 0 {
		t.Fatalf("exit code = %d, error = %q", final.ExitCode, final.Error)
	}
	if !final.Truncated {
		t.Fatal("output exceeded the ceiling but Truncated was false")
	}
	if len(final.Output) > maxCommandOutput {
		t.Fatalf("output length = %d, want at most %d (the ceiling)", len(final.Output), maxCommandOutput)
	}
}

// TestStartRunRefusesCredentialShapedArguments checks containsSensitiveArgument
// against each marker it actually looks for, and includes an ordinary
// argument list that must still pass -- otherwise a function that refused
// everything would satisfy this test just as well as the real one.
func TestStartRunRefusesCredentialShapedArguments(t *testing.T) {
	service, project, cleanup := newProductTestServiceWithProject(t)
	defer cleanup()
	for _, arguments := range [][]string{
		{"--password", "hunter2"},
		{"deploy", "--token=abc123"},
		{"--secret", "s3cr3t"},
		{"push", "--api_key=xyz"},
		{"-H", "Authorization: Bearer abc"},
	} {
		request := agentRunRequest(project.ID)
		request.Arguments = arguments
		if _, err := service.StartRun(context.Background(), request); err == nil {
			t.Errorf("arguments %v were accepted", arguments)
		}
	}
	// An ordinary argument list must still pass through unrefused, so the
	// check above is not satisfiable by a function that refuses everything.
	if _, err := service.StartRun(context.Background(), agentRunRequest(project.ID)); err != nil {
		t.Fatalf("an ordinary argument list was refused: %v", err)
	}
}

// TestAgentAllowedExecutablesMatchesTheRunner guards the duplication in
// internal/agent/runtool.go: that list exists only so the model is told what
// it may use instead of only what it may not, but the runner's own allowlist
// here is the one that matters for safety. If the two ever disagree, either
// the model is told it may run something the runner will refuse, or the
// model is never told about something the runner would accept -- both are a
// worse experience than duplication caught immediately by a test.
func TestAgentAllowedExecutablesMatchesTheRunner(t *testing.T) {
	agentSide := agent.AllowedExecutables()
	// Build a set rather than comparing raw lengths: a duplicate entry on
	// agent's side masking a dropped one (same length, still every remaining
	// name found in the runner's map) would pass a length-plus-one-direction
	// check while quietly telling the model it may not run something the
	// runner would actually accept.
	agentSet := make(map[string]bool, len(agentSide))
	for _, name := range agentSide {
		agentSet[name] = true
	}
	if len(agentSet) != len(allowedExecutables) {
		t.Fatalf("agent's allowlist has %d distinct entries, runner's has %d: %v vs %v",
			len(agentSet), len(allowedExecutables), agentSide, allowedExecutables)
	}
	for _, name := range agentSide {
		if !allowedExecutables[name] {
			t.Fatalf("agent's allowlist names %q, which the runner's allowlist does not", name)
		}
	}
	for name := range allowedExecutables {
		if !agentSet[name] {
			t.Fatalf("runner's allowlist names %q, which agent's allowlist does not -- the model is never told it may run this", name)
		}
	}
}

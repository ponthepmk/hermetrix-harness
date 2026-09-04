package product

import (
	"context"
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
	if _, err := service.StartRun(context.Background(), request); err != nil {
		t.Fatalf("600 second timeout was refused: %v", err)
	}
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
	if _, err := service.StartRun(context.Background(), request); err != nil {
		t.Fatalf("a directory inside the project was refused: %v", err)
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
}

func TestLookupRunSaysWhenThereIsNoSuchJob(t *testing.T) {
	service, _, cleanup := newProductTestServiceWithProject(t)
	defer cleanup()
	if _, err := service.LookupRun(context.Background(), "job_does_not_exist"); err == nil {
		t.Fatal("LookupRun invented a state for a job that does not exist")
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
	if len(agentSide) != len(allowedExecutables) {
		t.Fatalf("agent's allowlist has %d entries, runner's has %d: %v vs %v",
			len(agentSide), len(allowedExecutables), agentSide, allowedExecutables)
	}
	for _, name := range agentSide {
		if !allowedExecutables[name] {
			t.Fatalf("agent's allowlist names %q, which the runner's allowlist does not", name)
		}
	}
}

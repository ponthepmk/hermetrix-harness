package product

import (
	"context"
	"strings"
	"testing"
	"time"

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

package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/providers"
	toolruntime "hermetrix-harness/internal/tools"
)

type fakeRunner struct {
	mu       sync.Mutex
	started  []RunRequest
	results  map[string]RunResult
	canceled []string
	startErr error
}

func (f *fakeRunner) StartRun(_ context.Context, request RunRequest) (RunResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return RunResult{}, f.startErr
	}
	id := fmt.Sprintf("job_%d", len(f.started))
	f.started = append(f.started, request)
	if f.results == nil {
		f.results = map[string]RunResult{}
	}
	if _, ok := f.results[id]; !ok {
		f.results[id] = RunResult{JobID: id, State: "running"}
	}
	return RunResult{JobID: id, State: "queued"}, nil
}

func (f *fakeRunner) LookupRun(_ context.Context, jobID string) (RunResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result, ok := f.results[jobID]
	if !ok {
		return RunResult{}, errors.New("no such job")
	}
	return result, nil
}

func (f *fakeRunner) CancelRun(_ context.Context, jobID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.canceled = append(f.canceled, jobID)
	result := f.results[jobID]
	// A real runner only flips a job to canceled if it was still queued or
	// running when the request landed; one that already finished on its own
	// keeps its real outcome. Mirroring that here is what makes it possible to
	// test that the agent reports the runner's actual answer rather than
	// assuming "canceled" just because the call succeeded.
	if !result.Done() {
		result.JobID, result.State = jobID, "canceled"
		f.results[jobID] = result
	}
	return nil
}

func (f *fakeRunner) finish(jobID string, result RunResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[jobID] = result
}

func runCall(arguments string) providers.ToolCall {
	return providers.ToolCall{ID: "call_run", Type: "function", Name: "workspace.run", Arguments: arguments}
}

func TestRunToolStartReturnsAJobIDWithoutWaiting(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{}
	service.WithRuntime(runner, nil)
	receipt := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"go","arguments":["test","./..."]}`),
		toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"})
	if receipt.Status != "succeeded" {
		t.Fatalf("status = %q, error = %q", receipt.Status, receipt.Error)
	}
	if receipt.Metadata["job_id"] != "job_0" {
		t.Fatalf("job_id = %v, want job_0", receipt.Metadata["job_id"])
	}
	if runner.started[0].WorkingDir != "" {
		t.Fatalf("working dir = %q, want empty so the runner uses the project root", runner.started[0].WorkingDir)
	}
	if !strings.HasPrefix(runner.started[0].Actor, "agent:") {
		t.Fatalf("actor = %q, want an agent-prefixed actor", runner.started[0].Actor)
	}
}

func TestRunToolStatusLongPollsUntilTheCommandFinishes(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{}
	service.WithRuntime(runner, nil)
	start := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"go","arguments":["build","./..."]}`),
		toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"})
	jobID, _ := start.Metadata["job_id"].(string)
	go func() {
		time.Sleep(120 * time.Millisecond)
		runner.finish(jobID, RunResult{JobID: jobID, State: "completed", ExitCode: 0, Output: "ok", DurationMS: 12})
	}()
	receipt := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"status","job_id":"`+jobID+`"}`),
		toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"})
	if receipt.Status != "succeeded" {
		t.Fatalf("status = %q, error = %q", receipt.Status, receipt.Error)
	}
	if receipt.Metadata["state"] != "completed" {
		t.Fatalf("state = %v, want completed", receipt.Metadata["state"])
	}
	if receipt.Metadata["exit_code"] != 0 {
		t.Fatalf("exit_code = %v, want 0", receipt.Metadata["exit_code"])
	}
}

func TestRunToolCapsConcurrentJobsPerSession(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{}
	service.WithRuntime(runner, nil)
	definition := toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"}
	arguments := `{"action":"start","executable":"go","arguments":["vet","./..."]}`
	for attempt := 0; attempt < 2; attempt++ {
		if receipt := service.executeRunTool(context.Background(), session, runCall(arguments), definition); receipt.Status != "succeeded" {
			t.Fatalf("start %d failed: %s", attempt, receipt.Error)
		}
	}
	third := service.executeRunTool(context.Background(), session, runCall(arguments), definition)
	if third.Status != "failed" {
		t.Fatal("third concurrent start was accepted")
	}
	if !strings.Contains(third.Error, "already running") {
		t.Fatalf("error = %q, want it to name the concurrency limit", third.Error)
	}
	// A session can hit this cap with no job_id left in view -- a compacted
	// turn, a topic change -- so the refusal has to name the jobs holding the
	// slots, or there is no way back into workspace.run for the rest of the
	// process.
	if !strings.Contains(third.Error, "job_0") || !strings.Contains(third.Error, "job_1") {
		t.Fatalf("error = %q, want it to name the in-flight jobs", third.Error)
	}
}

// TestRunToolStartSweepsFinishedJobsBeforeCountingTheCap pins "concurrent"
// to mean running, not "started and not yet observed". A job that finished
// without ever being polled must free its slot the moment something asks for
// a new one -- otherwise a session that stops polling is locked out of
// workspace.run for the rest of the process, with no error ever occurring.
func TestRunToolStartSweepsFinishedJobsBeforeCountingTheCap(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{}
	service.WithRuntime(runner, nil)
	definition := toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"}
	arguments := `{"action":"start","executable":"go","arguments":["vet","./..."]}`
	first := service.executeRunTool(context.Background(), session, runCall(arguments), definition)
	firstJobID, _ := first.Metadata["job_id"].(string)
	second := service.executeRunTool(context.Background(), session, runCall(arguments), definition)
	if first.Status != "succeeded" || second.Status != "succeeded" {
		t.Fatalf("setup: first=%s second=%s", first.Error, second.Error)
	}
	// The model never calls action=status on the first job; it just finishes
	// on its own, the same way a turn ending or a topic change would strand it.
	runner.finish(firstJobID, RunResult{JobID: firstJobID, State: "completed", ExitCode: 0})
	third := service.executeRunTool(context.Background(), session, runCall(arguments), definition)
	if third.Status != "succeeded" {
		t.Fatalf("a finished-but-unpolled job kept holding its slot: %s", third.Error)
	}
}

// TestRunToolStartSurfacesARunnerStartError pins startRun's error path: a
// runner that refuses to start a command must fail the tool call, not report
// success with no job_id.
func TestRunToolStartSurfacesARunnerStartError(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{startErr: errors.New("executable not found")}
	service.WithRuntime(runner, nil)
	receipt := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"go","arguments":["vet","./..."]}`),
		toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"})
	if receipt.Status != "failed" {
		t.Fatal("a runner start error was reported as success")
	}
	if !strings.Contains(receipt.Error, "executable not found") {
		t.Fatalf("error = %q, want the runner's error surfaced", receipt.Error)
	}
}

// TestRunToolStatusReportsStillRunningWhenThePollExpires exercises the one
// branch of statusRun that a package-level 30-second constant made
// unreachable in a test: the poll expiring while the command is still
// running. Service.runStatusPoll exists so a test can shrink that wait.
func TestRunToolStatusReportsStillRunningWhenThePollExpires(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{}
	service.WithRuntime(runner, nil)
	service.runStatusPoll = 50 * time.Millisecond
	definition := toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"}
	start := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"go","arguments":["build","./..."]}`), definition)
	jobID, _ := start.Metadata["job_id"].(string)
	// The command never finishes inside the shortened poll window.
	receipt := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"status","job_id":"`+jobID+`"}`), definition)
	if receipt.Status != "succeeded" {
		t.Fatalf("status = %q, error = %q", receipt.Status, receipt.Error)
	}
	if receipt.Metadata["running"] != true {
		t.Fatalf("metadata = %v, want running=true", receipt.Metadata)
	}
	if !service.runs.owns(session.ID, jobID) {
		t.Fatal("the slot was released even though the command is still running")
	}
}

// TestRunToolStatusRefusesAJobFromAnotherSession and the cancel test below pin
// the ownership check: the runner scopes lookups and cancellation by job id
// alone, so the tracker is the only thing stopping one session from reading or
// stopping another session's command.
func TestRunToolStatusRefusesAJobFromAnotherSession(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{}
	service.WithRuntime(runner, nil)
	definition := toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"}
	start := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"go","arguments":["vet","./..."]}`), definition)
	jobID, _ := start.Metadata["job_id"].(string)
	stranger := session
	stranger.ID = "a-different-session"
	receipt := service.executeRunTool(context.Background(), stranger,
		runCall(`{"action":"status","job_id":"`+jobID+`"}`), definition)
	if receipt.Status != "failed" {
		t.Fatal("a foreign session read another session's command output")
	}
	if !strings.Contains(receipt.Error, "not tracked") {
		t.Fatalf("error = %q, want the ownership refusal", receipt.Error)
	}
}

func TestRunToolCancelRefusesAJobFromAnotherSession(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{}
	service.WithRuntime(runner, nil)
	definition := toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"}
	start := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"go","arguments":["vet","./..."]}`), definition)
	jobID, _ := start.Metadata["job_id"].(string)
	stranger := session
	stranger.ID = "a-different-session"
	receipt := service.executeRunTool(context.Background(), stranger,
		runCall(`{"action":"cancel","job_id":"`+jobID+`"}`), definition)
	if receipt.Status != "failed" {
		t.Fatal("a foreign session canceled another session's command")
	}
	if len(runner.canceled) != 0 {
		t.Fatal("the runner was asked to cancel a job the caller does not own")
	}
}

// TestRunToolCancelReportsTheRunnersActualState pins that cancelRun reports
// what the runner says happened, not what the agent assumed would happen: a
// job that finished on its own in the moment before the cancel request landed
// is not "canceled".
func TestRunToolCancelReportsTheRunnersActualState(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{}
	service.WithRuntime(runner, nil)
	definition := toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"}
	start := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"go","arguments":["vet","./..."]}`), definition)
	jobID, _ := start.Metadata["job_id"].(string)
	runner.finish(jobID, RunResult{JobID: jobID, State: "completed", ExitCode: 0})
	cancel := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"cancel","job_id":"`+jobID+`"}`), definition)
	if cancel.Status != "succeeded" {
		t.Fatalf("cancel failed: %s", cancel.Error)
	}
	if cancel.Metadata["state"] != "completed" {
		t.Fatalf("state = %v, want completed, the runner's actual answer, not the agent's assumption", cancel.Metadata["state"])
	}
}

func TestRunToolCancelReleasesTheSlot(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{}
	service.WithRuntime(runner, nil)
	definition := toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"}
	arguments := `{"action":"start","executable":"go","arguments":["vet","./..."]}`
	first := service.executeRunTool(context.Background(), session, runCall(arguments), definition)
	jobID, _ := first.Metadata["job_id"].(string)
	service.executeRunTool(context.Background(), session, runCall(arguments), definition)
	cancel := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"cancel","job_id":"`+jobID+`"}`), definition)
	if cancel.Status != "succeeded" {
		t.Fatalf("cancel failed: %s", cancel.Error)
	}
	if len(runner.canceled) != 1 || runner.canceled[0] != jobID {
		t.Fatalf("canceled = %v, want [%s]; cancelRun must actually forward to the runner", runner.canceled, jobID)
	}
	if receipt := service.executeRunTool(context.Background(), session, runCall(arguments), definition); receipt.Status != "succeeded" {
		t.Fatalf("slot was not released: %s", receipt.Error)
	}
}

func TestRunToolRefusesWithoutAProjectRoot(t *testing.T) {
	service, session, cleanup := newAgentServiceWithRootlessProject(t)
	defer cleanup()
	service.WithRuntime(&fakeRunner{}, nil)
	receipt := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"go","arguments":["vet","./..."]}`),
		toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"})
	if receipt.Status != "failed" {
		t.Fatal("a project with no code folder was allowed to run a command")
	}
}

func TestRunToolRefusesUnknownArgumentFields(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	service.WithRuntime(&fakeRunner{}, nil)
	receipt := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"go","arguments":["vet"],"shell":true}`),
		toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"})
	if receipt.Status != "failed" {
		t.Fatal("an unknown argument field was accepted")
	}
}

// newAgentServiceWithProject builds on skillManageFixture's store and service,
// adding a project row with a real t.TempDir() root so workspace.run has
// somewhere to execute. The fixture's own t.Cleanup already closes the store,
// so there is nothing left for this cleanup to do beyond satisfying the
// three-return shape the run tests share with the rootless variant below.
func newAgentServiceWithProject(t *testing.T) (*Service, Session, func()) {
	t.Helper()
	service, session := skillManageFixture(t)
	projectID := identity.New("prj")
	root := t.TempDir()
	if _, err := service.store.DB.ExecContext(context.Background(),
		`INSERT INTO projects(id,name,root_path,state,created_at,updated_at)
		 VALUES(?,'run-tool',?,'active',datetime('now'),datetime('now'))`, projectID, root); err != nil {
		t.Fatal(err)
	}
	session.ProjectID = projectID
	return service, session, func() {}
}

// newAgentServiceWithRootlessProject is the same shape, but the project row
// has no code folder -- the state a model is in when it opened a project
// before pointing it at a directory.
func newAgentServiceWithRootlessProject(t *testing.T) (*Service, Session, func()) {
	t.Helper()
	service, session := skillManageFixture(t)
	projectID := identity.New("prj")
	if _, err := service.store.DB.ExecContext(context.Background(),
		`INSERT INTO projects(id,name,root_path,state,created_at,updated_at)
		 VALUES(?,'run-tool-rootless','','active',datetime('now'),datetime('now'))`, projectID); err != nil {
		t.Fatal(err)
	}
	session.ProjectID = projectID
	return service, session, func() {}
}

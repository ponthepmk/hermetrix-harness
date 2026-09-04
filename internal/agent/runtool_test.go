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
	result.JobID, result.State = jobID, "canceled"
	f.results[jobID] = result
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

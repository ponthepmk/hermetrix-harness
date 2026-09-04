package agent

import (
	"context"
	"database/sql"
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
	mu         sync.Mutex
	started    []RunRequest
	results    map[string]RunResult
	canceled   []string
	startErr   error
	lookupErrs map[string]error // jobID -> error to return once from LookupRun
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
	if err, ok := f.lookupErrs[jobID]; ok {
		delete(f.lookupErrs, jobID)
		return RunResult{}, err
	}
	result, ok := f.results[jobID]
	if !ok {
		return RunResult{}, errors.New("no such job")
	}
	return result, nil
}

func (f *fakeRunner) CancelRun(_ context.Context, jobID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if result, ok := f.results[jobID]; ok && result.Done() {
		// product.CancelJob's UPDATE only flips a row that is still queued or
		// running; an already-terminal job affects zero rows and it reports
		// "job is not cancelable". Mirroring that here is what makes it
		// possible to test that the agent never claims an outcome the real
		// runner could not have produced.
		return errors.New("job is not cancelable")
	}
	// The real runner does not rewrite state to "canceled" here either --
	// that happens later, asynchronously, once the process actually exits.
	// CancelRun only means the request was accepted.
	f.canceled = append(f.canceled, jobID)
	return nil
}

func (f *fakeRunner) finish(jobID string, result RunResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[jobID] = result
}

// failNextLookup makes the next LookupRun(jobID) call return err instead of a
// result, once.
func (f *fakeRunner) failNextLookup(jobID string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lookupErrs == nil {
		f.lookupErrs = map[string]error{}
	}
	f.lookupErrs[jobID] = err
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

// TestRunToolStartHoldsTheCapUnderConcurrentStarts is the one test in this
// file that actually drives executeRunTool from multiple goroutines, so
// `-race` has something to say about runTracker under real contention rather
// than only the sequential calls every other test makes. claim's own mutex is
// the only hard gate; the pre-flight inFlight check is just an optimization,
// so every goroutine still has to reach claim, and at most runsPerSession of
// them can win it no matter how the scheduler interleaves them.
func TestRunToolStartHoldsTheCapUnderConcurrentStarts(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{}
	service.WithRuntime(runner, nil)
	definition := toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"}
	const attempts = 8
	receipts := make([]toolruntime.Receipt, attempts)
	var wg sync.WaitGroup
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			defer wg.Done()
			receipts[i] = service.executeRunTool(context.Background(), session,
				runCall(`{"action":"start","executable":"go","arguments":["vet","./..."]}`), definition)
		}(i)
	}
	wg.Wait()
	succeeded := 0
	for _, receipt := range receipts {
		switch {
		case receipt.Status == "succeeded":
			succeeded++
		case strings.Contains(receipt.Error, "already running"):
			// expected: this attempt lost the race for a slot.
		default:
			t.Fatalf("unexpected failure: %s", receipt.Error)
		}
	}
	if succeeded != runsPerSession {
		t.Fatalf("succeeded = %d, want exactly %d -- the per-session cap must hold under real contention, not just sequential calls",
			succeeded, runsPerSession)
	}
	if got := service.runs.inFlight(session.ID); got != runsPerSession {
		t.Fatalf("inFlight = %d, want %d", got, runsPerSession)
	}
	// However the scheduler interleaved these: some losers may have been
	// turned away by the pre-flight check before ever calling StartRun, but
	// every one that did start a job and then lost the claim must have had
	// that job stopped -- there is no other way it stays untracked.
	if got, want := len(runner.canceled), len(runner.started)-runsPerSession; got != want {
		t.Fatalf("canceled = %d, want %d -- every started-but-not-claimed job must be stopped", got, want)
	}
}

// slotStealingRunner reaches startRun's claim-race unwind deterministically.
// Two real goroutines can land in that branch too (see the test above), but
// only nondeterministically; this fake reproduces the exact interleaving by
// claiming runsPerSession phantom slots as a side effect of StartRun itself,
// between startRun's pre-flight cap check (which it has already passed) and
// its own claim of the job StartRun is about to return.
type slotStealingRunner struct {
	*fakeRunner
	tracker   *runTracker
	sessionID string
	cancelErr error
}

func (r *slotStealingRunner) StartRun(ctx context.Context, request RunRequest) (RunResult, error) {
	for i := 0; i < runsPerSession; i++ {
		r.tracker.claim(r.sessionID, fmt.Sprintf("stolen-slot-%d", i), 1<<30)
	}
	return r.fakeRunner.StartRun(ctx, request)
}

func (r *slotStealingRunner) CancelRun(_ context.Context, jobID string) error {
	r.fakeRunner.mu.Lock()
	r.fakeRunner.canceled = append(r.fakeRunner.canceled, jobID)
	r.fakeRunner.mu.Unlock()
	return r.cancelErr
}

// TestRunToolStartSurfacesAFailedUnwindCancelOnAClaimRace pins the smaller
// finding that the claim-race unwind used to discard a failed cancel with
// `_ = s.runner.CancelRun(...)`: if the job this call just started cannot be
// stopped either, the model needs to know a process is now running with
// nothing tracking it, not just that its own start was refused.
func TestRunToolStartSurfacesAFailedUnwindCancelOnAClaimRace(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &slotStealingRunner{fakeRunner: &fakeRunner{}, tracker: &service.runs, sessionID: session.ID,
		cancelErr: errors.New("stop failed")}
	service.WithRuntime(runner, nil)
	receipt := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"go","arguments":["vet","./..."]}`),
		toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"})
	if receipt.Status != "failed" {
		t.Fatal("a claim race that also failed to unwind was reported as success")
	}
	if !strings.Contains(receipt.Error, "already running") {
		t.Fatalf("error = %q, want the concurrency refusal", receipt.Error)
	}
	if !strings.Contains(receipt.Error, "stop failed") {
		t.Fatalf("error = %q, want the failed unwind-cancel surfaced", receipt.Error)
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

// TestRunToolStatusStillAnswersForAJobTheSweepReclaimed is the ownership-vs-
// occupancy split in action: reclaiming a slot for the cap must not also cut
// off the one route back to that job's output.
func TestRunToolStatusStillAnswersForAJobTheSweepReclaimed(t *testing.T) {
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
	runner.finish(firstJobID, RunResult{JobID: firstJobID, State: "completed", ExitCode: 0})
	third := service.executeRunTool(context.Background(), session, runCall(arguments), definition)
	if third.Status != "succeeded" {
		t.Fatalf("the sweep did not reclaim the finished job's slot: %s", third.Error)
	}
	status := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"status","job_id":"`+firstJobID+`"}`), definition)
	if status.Status != "succeeded" || status.Metadata["state"] != "completed" {
		t.Fatalf("status on a swept job = %+v, want the completed result still reachable", status)
	}
}

// TestRunToolStatusAnswersRepeatedlyForAFinishedJob pins the other half of the
// same split: finishing a job on the first status call must not make a second
// one refuse it.
func TestRunToolStatusAnswersRepeatedlyForAFinishedJob(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{}
	service.WithRuntime(runner, nil)
	definition := toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"}
	start := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"go","arguments":["vet","./..."]}`), definition)
	jobID, _ := start.Metadata["job_id"].(string)
	runner.finish(jobID, RunResult{JobID: jobID, State: "completed", ExitCode: 0})
	for attempt := 0; attempt < 2; attempt++ {
		status := service.executeRunTool(context.Background(), session,
			runCall(`{"action":"status","job_id":"`+jobID+`"}`), definition)
		if status.Status != "succeeded" || status.Metadata["state"] != "completed" {
			t.Fatalf("status call %d = %+v, want the completed result", attempt, status)
		}
	}
}

// TestRunToolStatusFreesTheSlotWhenTheRunnerNoLongerResolvesTheJob pins the
// not-found classification: a job the runner can no longer resolve at all
// frees its slot, because nothing will ever report it Done().
func TestRunToolStatusFreesTheSlotWhenTheRunnerNoLongerResolvesTheJob(t *testing.T) {
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
	runner.failNextLookup(firstJobID, sql.ErrNoRows)
	status := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"status","job_id":"`+firstJobID+`"}`), definition)
	if status.Status != "failed" {
		t.Fatalf("status = %+v, want the runner's error surfaced", status)
	}
	third := service.executeRunTool(context.Background(), session, runCall(arguments), definition)
	if third.Status != "succeeded" {
		t.Fatalf("a job the runner no longer resolves kept holding its slot: %s", third.Error)
	}
}

// TestRunToolStatusKeepsTheSlotOnATransientLookupError pins the other half:
// an error that does not mean the job is gone -- a deadline, a busy pool --
// must not free the slot, or a run of transient errors quietly raises the
// effective cap above runsPerSession.
func TestRunToolStatusKeepsTheSlotOnATransientLookupError(t *testing.T) {
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
	runner.failNextLookup(firstJobID, errors.New("context deadline exceeded"))
	status := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"status","job_id":"`+firstJobID+`"}`), definition)
	if status.Status != "failed" {
		t.Fatalf("status = %+v, want the transient error surfaced", status)
	}
	third := service.executeRunTool(context.Background(), session, runCall(arguments), definition)
	if third.Status != "failed" || !strings.Contains(third.Error, "already running") {
		t.Fatalf("a transient lookup error freed a slot: third start = %+v", third)
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
	if got := service.runs.inFlight(session.ID); got != 1 {
		t.Fatalf("inFlight = %d, want 1 -- the slot must still be held while the command is still running", got)
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

// TestRunToolCancelReportsARequestNotACompletedOutcome pins what a successful
// cancel actually means against the real runner: product.CancelJob only flags
// cancellation and returns immediately -- it does not rewrite the job's state
// until the process actually exits, asynchronously, possibly seconds later.
// The receipt must say a cancellation was requested, not assert an outcome
// the runner has not reported.
func TestRunToolCancelReportsARequestNotACompletedOutcome(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{}
	service.WithRuntime(runner, nil)
	definition := toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"}
	start := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"go","arguments":["vet","./..."]}`), definition)
	jobID, _ := start.Metadata["job_id"].(string)
	cancel := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"cancel","job_id":"`+jobID+`"}`), definition)
	if cancel.Status != "succeeded" {
		t.Fatalf("cancel failed: %s", cancel.Error)
	}
	if strings.Contains(cancel.Output, "canceled job") {
		t.Fatalf("output = %q, claims an outcome the runner has not confirmed", cancel.Output)
	}
	if !strings.Contains(cancel.Output, "requested") {
		t.Fatalf("output = %q, want it to say cancellation was requested", cancel.Output)
	}
	if cancel.Metadata["state"] != "running" {
		t.Fatalf("state = %v, want the runner's actual last-known state, not an assumed outcome", cancel.Metadata["state"])
	}
}

// TestRunToolCancelRefusesAnAlreadyFinishedJob mirrors product.CancelJob's
// real behavior: its UPDATE only flips a row that is still queued or running,
// so canceling a job that already finished on its own reports "job is not
// cancelable" rather than pretending to succeed.
func TestRunToolCancelRefusesAnAlreadyFinishedJob(t *testing.T) {
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
	if cancel.Status != "failed" {
		t.Fatal("canceling an already-finished job was reported as success")
	}
	// The job is still this session's own, even though cancel refused it --
	// ownership does not depend on what cancel or status most recently did.
	status := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"status","job_id":"`+jobID+`"}`), definition)
	if status.Status != "succeeded" || status.Metadata["state"] != "completed" {
		t.Fatalf("status after a refused cancel = %+v, want the completed job", status)
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

// TestRunToolStatusStillAnswersForAJobAfterCancel is the first scenario the
// review named: canceling a job releases its slot, but must not also erase
// this session's ownership of it -- only a foreign session gets the
// "not tracked" refusal, and this session started the job cancel just acted on.
func TestRunToolStatusStillAnswersForAJobAfterCancel(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	runner := &fakeRunner{}
	service.WithRuntime(runner, nil)
	// The fake never marks a canceled-while-running job Done(), matching the
	// real runner's async cancellation -- so without a short poll this status
	// call would long-poll for the full default thirty seconds.
	service.runStatusPoll = time.Millisecond
	definition := toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"}
	start := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"go","arguments":["vet","./..."]}`), definition)
	jobID, _ := start.Metadata["job_id"].(string)
	cancel := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"cancel","job_id":"`+jobID+`"}`), definition)
	if cancel.Status != "succeeded" {
		t.Fatalf("cancel failed: %s", cancel.Error)
	}
	status := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"status","job_id":"`+jobID+`"}`), definition)
	if strings.Contains(status.Error, "not tracked") {
		t.Fatalf("error = %q, canceling a job must not erase this session's ownership of it", status.Error)
	}
	if status.Status != "succeeded" {
		t.Fatalf("status after cancel = %+v, want the runner's own answer", status)
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

// TestRunToolNamesTheAllowedExecutables pins the spec's requirement that a
// refused executable says what is allowed, not only that this one is not. The
// fake runner's startErr would surface the runner's own generic refusal if
// startRun ever reached it; it must not, because executableAllowed refuses
// bash before the runner is ever called.
func TestRunToolNamesTheAllowedExecutables(t *testing.T) {
	service, session, cleanup := newAgentServiceWithProject(t)
	defer cleanup()
	service.WithRuntime(&fakeRunner{startErr: errors.New("actor and an allowed executable without a path are required")}, nil)
	receipt := service.executeRunTool(context.Background(), session,
		runCall(`{"action":"start","executable":"bash","arguments":["-c","ls"]}`),
		toolruntime.Definition{Name: "workspace.run", Revision: "v1", Effect: "execute"})
	if receipt.Status != "failed" {
		t.Fatal("bash was accepted")
	}
	// Check every allowed name, not just two of seven: a message that had
	// silently lost git, node, npm, rg or ls would still satisfy a check that
	// only looked for "go" and "python3".
	for _, name := range AllowedExecutables() {
		if !strings.Contains(receipt.Error, name) {
			t.Fatalf("error = %q, want it to name allowed executable %q", receipt.Error, name)
		}
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

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"hermetrix-harness/internal/providers"
	toolruntime "hermetrix-harness/internal/tools"
)

// defaultRunStatusPoll is how long one status call waits before answering with
// whatever it has. A model that asked for a build wants the result, not a busy
// loop, but a call that never returns is a hung turn: thirty seconds is long
// enough to catch most commands in one call and short enough to stay inside the
// turn's budget. It is the default for Service.runStatusPoll, not a hardcoded
// constant, so a test can shrink the wait and reach the "still running" branch
// without a real thirty-second sleep.
const defaultRunStatusPoll = 30 * time.Second

// runsPerSession is how many commands one session may have in flight. Two lets
// a build and a test run together; more than that is a model spraying work at
// the machine rather than waiting for an answer.
const runsPerSession = 2

// runTracker remembers which jobs a session started. It answers two questions:
// how many of this session's jobs are in flight, for the per-session cap, and
// whether a given job belongs to this session at all, which status and cancel
// both have to know before they hand a job id to the runner -- the runner
// itself scopes lookups and cancellation by job id alone, with no session or
// project check, so this tracker is the only thing standing between one
// session and another session's command output.
type runTracker struct {
	mu   sync.Mutex
	jobs map[string]map[string]bool
}

func (t *runTracker) claim(sessionID, jobID string, limit int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.jobs == nil {
		t.jobs = map[string]map[string]bool{}
	}
	current := t.jobs[sessionID]
	if len(current) >= limit {
		return false
	}
	if current == nil {
		current = map[string]bool{}
		t.jobs[sessionID] = current
	}
	current[jobID] = true
	return true
}

func (t *runTracker) inFlight(sessionID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.jobs[sessionID])
}

// owns reports whether sessionID started jobID. status and cancel both gate on
// this before touching the runner, so a job id from one session can never be
// used to read or stop another session's command -- or the UI's, or another
// session entirely, none of which the runner itself distinguishes.
func (t *runTracker) owns(sessionID, jobID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.jobs[sessionID][jobID]
}

// ids lists the jobs sessionID currently has claimed, sorted for a
// deterministic refusal message.
func (t *runTracker) ids(sessionID string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	ids := make([]string, 0, len(t.jobs[sessionID]))
	for id := range t.jobs[sessionID] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (t *runTracker) release(sessionID, jobID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if current := t.jobs[sessionID]; current != nil {
		delete(current, jobID)
		if len(current) == 0 {
			delete(t.jobs, sessionID)
		}
	}
}

type runArgs struct {
	Action         string   `json:"action"`
	Executable     string   `json:"executable"`
	Arguments      []string `json:"arguments"`
	WorkingDir     string   `json:"working_dir"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	JobID          string   `json:"job_id"`
}

// executeRunTool answers workspace.run. It is session-scoped because both the
// project the command runs in and the cap on how many may run at once belong to
// the session, and the registry holds neither.
func (s *Service) executeRunTool(ctx context.Context, session Session, call providers.ToolCall,
	definition toolruntime.Definition) toolruntime.Receipt {
	started := time.Now()
	receipt := toolruntime.Receipt{ToolCallID: call.ID, Name: call.Name, Revision: definition.Revision,
		Effect: definition.Effect, Status: "failed"}
	finish := func() toolruntime.Receipt {
		receipt.DurationMS = time.Since(started).Milliseconds()
		return receipt
	}
	if s.runner == nil {
		receipt.Error = "command execution is not available in this build"
		return finish()
	}
	decoder := json.NewDecoder(strings.NewReader(call.Arguments))
	decoder.DisallowUnknownFields()
	var args runArgs
	if err := decoder.Decode(&args); err != nil {
		receipt.Error = "invalid arguments: " + err.Error()
		return finish()
	}
	switch strings.TrimSpace(args.Action) {
	case "start":
		s.startRun(ctx, session, args, &receipt)
	case "status":
		s.statusRun(ctx, session, args, &receipt)
	case "cancel":
		s.cancelRun(ctx, session, args, &receipt)
	default:
		receipt.Error = "action must be start, status or cancel"
	}
	return finish()
}

func (s *Service) startRun(ctx context.Context, session Session, args runArgs, receipt *toolruntime.Receipt) {
	// The same refusal the file tools give: a project with no code folder has
	// nothing to run a command in, and falling back to another directory would
	// run it somewhere the user never opened.
	if _, err := s.scopedTools(ctx, session); err != nil {
		receipt.Error = err.Error()
		return
	}
	// A job that finished without ever being polled to completion -- the turn
	// ended, the model moved on, compaction dropped the receipt -- must not
	// lock this session out of workspace.run for the rest of the process.
	// Sweeping here, before the cap is checked, is what makes "concurrent"
	// mean running rather than "started and never observed".
	s.sweepFinishedRuns(ctx, session.ID)
	if s.runs.inFlight(session.ID) >= runsPerSession {
		receipt.Error = runConcurrencyRefusal(s.runs.ids(session.ID))
		return
	}
	result, err := s.runner.StartRun(ctx, RunRequest{ProjectID: session.ProjectID, Actor: "agent:" + session.ID,
		Executable: args.Executable, Arguments: args.Arguments, WorkingDir: args.WorkingDir,
		TimeoutSeconds: args.TimeoutSeconds})
	if err != nil {
		receipt.Error = err.Error()
		return
	}
	if !s.runs.claim(session.ID, result.JobID, runsPerSession) {
		// Another start filled the last slot between the check above and this
		// claim. The job just started counts against nobody's cap now, so it
		// has to be stopped rather than left running untracked; if even that
		// fails, the model needs to know a process is now loose.
		refusal := runConcurrencyRefusal(s.runs.ids(session.ID))
		if cancelErr := s.runner.CancelRun(ctx, result.JobID); cancelErr != nil {
			receipt.Error = fmt.Sprintf("%s (job %s also could not be stopped: %s)", refusal, result.JobID, cancelErr.Error())
		} else {
			receipt.Error = refusal
		}
		return
	}
	receipt.Status = "succeeded"
	receipt.Output = fmt.Sprintf("started %s (job %s). Call workspace.run with action=status and this job_id to read the result.",
		args.Executable, result.JobID)
	receipt.Metadata = map[string]any{"job_id": result.JobID, "state": result.State, "executable": args.Executable}
}

// sweepFinishedRuns releases the slots of jobs this session started but has
// not been told about since: ones the runner reports Done(), and ones the
// runner can no longer resolve at all. Both are terminal from this tracker's
// point of view -- there is nothing left to wait for -- so holding the slot
// serves no purpose and only starves a session that stops polling. The cost
// is at most runsPerSession lookups, paid once per start.
func (s *Service) sweepFinishedRuns(ctx context.Context, sessionID string) {
	for _, jobID := range s.runs.ids(sessionID) {
		result, err := s.runner.LookupRun(ctx, jobID)
		if err != nil || result.Done() {
			s.runs.release(sessionID, jobID)
		}
	}
}

// runConcurrencyRefusal is shared by both places a start can be refused for
// being over the per-session cap: the pre-flight check and the loser of a
// claim race. It names the jobs already holding a slot. A session can reach
// this cap with no job_id left in view -- a compacted turn, a topic change --
// and the sweep only clears a slot once something is finished; naming the
// still-running ones is the only way back in for a model that lost them.
func runConcurrencyRefusal(inFlight []string) string {
	return fmt.Sprintf("%d commands are already running in this session (%s); wait for one with action=status or stop one with action=cancel",
		runsPerSession, strings.Join(inFlight, ", "))
}

// notTrackedRefusal is what status and cancel say for a job id this session
// did not start. It says nothing about whether the id exists at all -- under
// another session, another project, or the UI -- so it cannot be used to probe
// for jobs this session has no business seeing.
func notTrackedRefusal(jobID string) string {
	return fmt.Sprintf("job %s is not tracked by this session", jobID)
}

func (s *Service) statusRun(ctx context.Context, session Session, args runArgs, receipt *toolruntime.Receipt) {
	jobID := strings.TrimSpace(args.JobID)
	if jobID == "" {
		receipt.Error = "job_id is required for action=status"
		return
	}
	if !s.runs.owns(session.ID, jobID) {
		receipt.Error = notTrackedRefusal(jobID)
		return
	}
	poll := s.runStatusPoll
	if poll <= 0 {
		poll = defaultRunStatusPoll
	}
	deadline := time.Now().Add(poll)
	for {
		result, err := s.runner.LookupRun(ctx, jobID)
		if err != nil {
			// The runner no longer resolving a job it once accepted is itself
			// a terminal outcome for the slot: nothing is ever going to
			// report this one Done(), so there is nothing left to hold the
			// slot for.
			s.runs.release(session.ID, jobID)
			receipt.Error = err.Error()
			return
		}
		if result.Done() {
			s.runs.release(session.ID, jobID)
			receipt.Status = "succeeded"
			receipt.Output = runOutput(result)
			receipt.Metadata = map[string]any{"job_id": jobID, "state": result.State, "exit_code": result.ExitCode,
				"duration_ms": result.DurationMS, "truncated": result.Truncated, "artifact_id": result.ArtifactID}
			return
		}
		if time.Now().After(deadline) {
			receipt.Status = "succeeded"
			receipt.Output = fmt.Sprintf("still %s after %s. Call action=status with the same job_id again.",
				result.State, poll)
			receipt.Metadata = map[string]any{"job_id": jobID, "state": result.State, "running": true}
			return
		}
		select {
		case <-ctx.Done():
			// Deliberately not releasing here: the job itself is not done,
			// only this call is. The next sweep, on this session's next
			// start, resolves it either way.
			receipt.Error = ctx.Err().Error()
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (s *Service) cancelRun(ctx context.Context, session Session, args runArgs, receipt *toolruntime.Receipt) {
	jobID := strings.TrimSpace(args.JobID)
	if jobID == "" {
		receipt.Error = "job_id is required for action=cancel"
		return
	}
	if !s.runs.owns(session.ID, jobID) {
		receipt.Error = notTrackedRefusal(jobID)
		return
	}
	if err := s.runner.CancelRun(ctx, jobID); err != nil {
		// Whatever the reason CancelRun could not act on this job -- most
		// often that the runner no longer resolves it -- holding the slot
		// open has the same problem as an unresolvable status lookup: there
		// is nothing left to wait for.
		s.runs.release(session.ID, jobID)
		receipt.Error = err.Error()
		return
	}
	s.runs.release(session.ID, jobID)
	// CancelRun reports only whether the request was accepted, not how the
	// job actually ended -- a job that finished on its own in the moment
	// before cancellation reached it is not "canceled", whatever this call
	// assumed on the way in. Ask the runner what actually happened; "canceled"
	// is only the fallback for when even that ask fails.
	state := "canceled"
	if result, err := s.runner.LookupRun(ctx, jobID); err == nil {
		state = result.State
	}
	receipt.Status = "succeeded"
	receipt.Output = fmt.Sprintf("canceled job %s (state: %s)", jobID, state)
	receipt.Metadata = map[string]any{"job_id": jobID, "state": state}
}

// runOutput is what the model reads. The exit code leads, because that is the
// fact the model most often gets wrong when it only sees the tail of a log.
func runOutput(result RunResult) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s, exit code %d", result.State, result.ExitCode)
	if result.DurationMS > 0 {
		fmt.Fprintf(&builder, ", %dms", result.DurationMS)
	}
	if result.Error != "" {
		fmt.Fprintf(&builder, "\nrunner error: %s", result.Error)
	}
	if result.Truncated {
		fmt.Fprintf(&builder, "\noutput was truncated at the runner's ceiling; %d bytes shown", len(result.Output))
	}
	if result.Output != "" {
		builder.WriteString("\n\n")
		builder.WriteString(result.Output)
	}
	return builder.String()
}

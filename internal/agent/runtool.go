package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// runTracker remembers which jobs a session started, and whether each one
// still counts against the session's concurrency cap. Ownership and slot
// occupancy are two different facts once a job finishes: a session keeps
// being able to ask status or cancel about a job it started long after that
// job stops counting against the cap, because that is the only route back to
// a command's output once nobody polled it before its slot was reclaimed for
// a new one -- which is precisely what the sweep in startRun does. Entries are
// marked finished rather than removed, so this map grows for as long as a
// session keeps running commands; that is bounded by what the session
// actually does and is not worth adding eviction for.
type runTracker struct {
	mu sync.Mutex
	// jobs maps sessionID -> jobID -> stillActive. A present key is
	// ownership: this session started that job, whatever the bool says. The
	// bool is slot occupancy: true counts against the cap, false is a job
	// this tracker has already been told is finished.
	jobs map[string]map[string]bool
}

func activeCount(entries map[string]bool) int {
	count := 0
	for _, active := range entries {
		if active {
			count++
		}
	}
	return count
}

func (t *runTracker) claim(sessionID, jobID string, limit int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.jobs == nil {
		t.jobs = map[string]map[string]bool{}
	}
	current := t.jobs[sessionID]
	if activeCount(current) >= limit {
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
	return activeCount(t.jobs[sessionID])
}

// owns reports whether sessionID ever started jobID, active or already
// finished. status and cancel both gate on this before touching the runner,
// so a job id from one session can never be used to read or stop another
// session's command -- or the UI's, or another session entirely, none of
// which the runner itself distinguishes.
func (t *runTracker) owns(sessionID, jobID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.jobs[sessionID][jobID]
	return ok
}

// activeIDs lists sessionID's jobs that still count against the cap, sorted
// for a deterministic sweep order and refusal message. A finished job is
// still owned (see owns) but is not "in flight", so it is deliberately absent
// from this list.
func (t *runTracker) activeIDs(sessionID string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var ids []string
	for id, active := range t.jobs[sessionID] {
		if active {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// finish marks a job as no longer counting against the cap, without
// forgetting that sessionID started it -- status and cancel both need to keep
// answering for it afterwards, which is the whole reason this is not called
// release and does not delete the entry.
func (t *runTracker) finish(sessionID, jobID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if current := t.jobs[sessionID]; current != nil {
		if _, ok := current[jobID]; ok {
			current[jobID] = false
		}
	}
}

// isJobNotFound reports whether err means the runner has nothing left to say
// about this job at all, as opposed to a transient failure to say it right
// now. Only the former is a reason to free the slot: a context deadline or a
// busy connection pool does not mean the command stopped running, and
// treating it as if it did would quietly raise the effective cap above
// runsPerSession every time one of those happens. product.GetJob surfaces a
// missing row as sql.ErrNoRows, unwrapped, through both LookupRun and
// CancelRun, so this is real classification, not a guess: any other runner
// implementation that does not use this sentinel is treated as "still
// unresolved but maybe transient", which is the safe default either way.
func isJobNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
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
		receipt.Error = runConcurrencyRefusal(s.runs.activeIDs(session.ID))
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
		refusal := runConcurrencyRefusal(s.runs.activeIDs(session.ID))
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

// sweepFinishedRuns frees the slots of jobs this session started but has not
// been told about since: ones the runner reports Done(), and ones the runner
// no longer resolves at all. Both are terminal from the cap's point of view --
// there is nothing left to wait for -- so holding the slot serves no purpose
// and only starves a session that stops polling. It does not forget the
// session started them: finish marks them done, it does not erase them, so
// status and cancel can still answer for a job this sweep just reclaimed. The
// cost is at most runsPerSession lookups, paid once per start.
func (s *Service) sweepFinishedRuns(ctx context.Context, sessionID string) {
	for _, jobID := range s.runs.activeIDs(sessionID) {
		result, err := s.runner.LookupRun(ctx, jobID)
		if err != nil {
			if isJobNotFound(err) {
				s.runs.finish(sessionID, jobID)
			}
			continue
		}
		if result.Done() {
			s.runs.finish(sessionID, jobID)
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
			if isJobNotFound(err) {
				// Nothing is ever going to report this one Done(): the
				// record itself is gone, so there is nothing left to hold
				// the slot for. Ownership stays -- a repeat call gets the
				// same answer rather than a confusing "not tracked".
				s.runs.finish(session.ID, jobID)
			}
			receipt.Error = err.Error()
			return
		}
		if result.Done() {
			s.runs.finish(session.ID, jobID)
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
			// Deliberately not touching the slot here: the job itself is not
			// done, only this call is. The next sweep, on this session's next
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
		// The runner refusing an already-terminal job (the real CancelJob's
		// update affects nothing once a job has left queued/running) is not
		// the same as the runner having nothing left to say about the id at
		// all -- only the latter frees the slot; the former means the job is
		// still exactly as done as it already was.
		if isJobNotFound(err) {
			s.runs.finish(session.ID, jobID)
		}
		receipt.Error = err.Error()
		return
	}
	s.runs.finish(session.ID, jobID)
	// CancelRun succeeding means the request was accepted, not that the job
	// has stopped: the real runner only flags cancellation here and rewrites
	// state later, asynchronously, once the process actually exits (and after
	// its own wait delay). Report what happened -- a request -- and carry
	// whatever the runner currently says beside it, rather than presenting a
	// guessed outcome as fact.
	state := "unknown"
	if result, err := s.runner.LookupRun(ctx, jobID); err == nil {
		state = result.State
	}
	receipt.Status = "succeeded"
	receipt.Output = fmt.Sprintf("cancellation requested for job %s; last known state: %s", jobID, state)
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

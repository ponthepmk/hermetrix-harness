package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"hermetrix-harness/internal/providers"
	toolruntime "hermetrix-harness/internal/tools"
)

// runStatusPoll is how long one status call waits before answering with
// whatever it has. A model that asked for a build wants the result, not a busy
// loop, but a call that never returns is a hung turn: thirty seconds is long
// enough to catch most commands in one call and short enough to stay inside the
// turn's budget.
const runStatusPoll = 30 * time.Second

// runsPerSession is how many commands one session may have in flight. Two lets
// a build and a test run together; more than that is a model spraying work at
// the machine rather than waiting for an answer.
const runsPerSession = 2

// runTracker remembers which jobs a session started, which is the only way to
// enforce the per-session cap: the job table is shared with the UI and with
// every other session.
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
	if s.runs.inFlight(session.ID) >= runsPerSession {
		receipt.Error = fmt.Sprintf("%d commands are already running in this session; wait for one with action=status or stop one with action=cancel", runsPerSession)
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
		_ = s.runner.CancelRun(ctx, result.JobID)
		receipt.Error = fmt.Sprintf("%d commands are already running in this session; wait for one with action=status or stop one with action=cancel", runsPerSession)
		return
	}
	receipt.Status = "succeeded"
	receipt.Output = fmt.Sprintf("started %s (job %s). Call workspace.run with action=status and this job_id to read the result.",
		args.Executable, result.JobID)
	receipt.Metadata = map[string]any{"job_id": result.JobID, "state": result.State, "executable": args.Executable}
}

func (s *Service) statusRun(ctx context.Context, session Session, args runArgs, receipt *toolruntime.Receipt) {
	jobID := strings.TrimSpace(args.JobID)
	if jobID == "" {
		receipt.Error = "job_id is required for action=status"
		return
	}
	deadline := time.Now().Add(runStatusPoll)
	for {
		result, err := s.runner.LookupRun(ctx, jobID)
		if err != nil {
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
				result.State, runStatusPoll)
			receipt.Metadata = map[string]any{"job_id": jobID, "state": result.State, "running": true}
			return
		}
		select {
		case <-ctx.Done():
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
	if err := s.runner.CancelRun(ctx, jobID); err != nil {
		receipt.Error = err.Error()
		return
	}
	s.runs.release(session.ID, jobID)
	receipt.Status = "succeeded"
	receipt.Output = "canceled job " + jobID
	receipt.Metadata = map[string]any{"job_id": jobID, "state": "canceled"}
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

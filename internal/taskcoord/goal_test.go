package taskcoord

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

// Only model transport is a fixture. Source writes, command processes, SQLite,
// rollback, independent-review gates and completion receipts are real.
type goalAdapter struct {
	failures, proposals, plans, reviews int
	sawFailure, sawEscalation           bool
	blockProposal                       bool
	entered                             chan struct{}
	rejectReviews                       int
	sawReviewFailure                    bool
	sawAbsentPlan                       bool
	sawAbsentSelection                  bool
	sawAbsentProposal                   bool
}

func (a *goalAdapter) StreamChat(ctx context.Context, _ providers.Profile, _ string, r providers.ChatRequest, _ func(providers.Delta) error) (providers.Completion, error) {
	name := r.Tools[0].Function.Name
	var value any
	switch name {
	case "submit_plan":
		a.plans++
		var input worker.PlanTask
		_ = json.Unmarshal([]byte(r.Messages[1].Content), &input)
		for _, file := range input.RepositoryFiles {
			if file.Path == "sum.js" && file.Absent && file.SHA256 == "absent" && len(input.WritablePaths) > 0 {
				a.sawAbsentPlan = true
			}
		}
		if input.Escalation != nil && len(input.Escalation.FailureIDs) == 2 && len(input.Escalation.EvidenceRefs) > 0 && len(input.FailureEvidence) > 0 {
			a.sawEscalation = true
		}
		value = map[string]any{"reason": "implement checked arithmetic using repository evidence", "steps": []any{map[string]any{"key": "fix", "title": "Correct addition", "workspace_change": "Change sum.js", "instructions": "Correct sum.js while preserving test.js", "requirement_ids": []string{"AC-1"}, "dependencies": []string{}, "checks": []string{"node test.js"}, "effect_scope": []string{"provider.select_files", "provider.propose", "workspace.apply", "workspace.run", "provider.review"}}}}
	case "submit_file_selection":
		var selection worker.FileSelectionTask
		_ = json.Unmarshal([]byte(r.Messages[1].Content), &selection)
		for _, file := range selection.Candidates {
			if file.Path == "sum.js" && file.Absent && file.Bytes == 0 {
				a.sawAbsentSelection = true
			}
		}
		value = map[string]any{"files": []string{"sum.js", "test.js"}, "rationale": "implementation and immutable acceptance test"}
	case "submit_changes":
		a.proposals++
		if a.blockProposal {
			close(a.entered)
			<-ctx.Done()
			return providers.Completion{}, ctx.Err()
		}
		var input worker.Task
		_ = json.Unmarshal([]byte(r.Messages[1].Content), &input)
		for _, file := range input.AbsentFiles {
			if file == "sum.js" && input.Files[file] == "" {
				a.sawAbsentProposal = true
			}
		}
		if len(input.FailureEvidence) > 0 {
			a.sawFailure = true
		}
		for _, e := range input.FailureEvidence {
			if e.CheckID == "provider.review" && strings.Contains(e.Output, "arithmetic documentation") && e.ArtifactSHA256 != "" {
				a.sawReviewFailure = true
			}
		}
		op := "+"
		if a.proposals <= a.failures {
			op = "*"
		}
		value = map[string]any{"summary": "correct arithmetic", "recommended_checks": []string{"node test.js"}, "changes": []any{map[string]any{"path": "sum.js", "content": "module.exports=(a,b)=>a" + op + "b;\n"}}}
		if a.proposals > 1 && a.proposals <= a.failures {
			// Distinct proposed source can still reproduce the same behavioral
			// failure, which must trigger the existing repeated-failure escalation.
			value.(map[string]any)["changes"].([]any)[0].(map[string]any)["content"] = "// distinct failing attempt\nmodule.exports=(a,b)=>a" + op + "b;\n"
		}
		if a.sawReviewFailure {
			value = map[string]any{"summary": "address actual reviewer finding", "recommended_checks": []string{"node test.js"}, "changes": []any{map[string]any{"path": "sum.js", "content": "// arithmetic documentation: returns the sum\nmodule.exports=(a,b)=>a+b;\n"}}}
		}
		if draft, ok := input.RepairDrafts["sum.js"]; ok {
			change := value.(map[string]any)["changes"].([]any)[0].(map[string]any)
			content := change["content"].(string)
			delete(change, "content")
			if content == draft.Content {
				change["retain_draft"] = true
			} else {
				change["edits"] = []worker.TextEdit{{Old: draft.Content, New: content}}
			}
		}
	case "submit_review":
		a.reviews++
		value = map[string]any{"verdict": "approve", "rationale": "actual unchanged test passes against applied source", "findings": []string{}}
		if a.reviews <= a.rejectReviews {
			value = map[string]any{"verdict": "reject", "rationale": "arithmetic documentation is required", "findings": []string{"include arithmetic documentation before acceptance"}}
		}
	}
	body, _ := json.Marshal(value)
	return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: name, Arguments: string(body)}}}, nil
}

func newGoalFixture(t *testing.T, a *goalAdapter, contract taskengine.GoalContract) (*Service, *taskengine.Service, taskengine.Task, StartGoalInput, string) {
	c, ts, task, input, root, _ := newGoalFixtureWithStore(t, a, contract)
	return c, ts, task, input, root
}

func newGoalFixtureWithStore(t *testing.T, a *goalAdapter, contract taskengine.GoalContract) (*Service, *taskengine.Service, taskengine.Task, StartGoalInput, string, *store.Store) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := product.NewService(db, skills.NewService(db))
	t.Cleanup(p.Close)
	root := t.TempDir()
	for path, body := range map[string]string{"sum.js": "module.exports=(a,b)=>a-b;\n", "test.js": "const assert=require('node:assert/strict');assert.equal(require('./sum')(2,3),5);assert.equal(require('./sum')(-4,7),3);console.log('acceptance passed');\n"} {
		if err = os.WriteFile(filepath.Join(root, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	project, err := p.EnsureWorkspaceProject(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	ps := providers.NewService(db, a)
	w, err := ps.Save(ctx, providers.SaveInput{Name: "goal fixture writer", BaseURL: "http://127.0.0.1:8088/v1", Model: "fixture", ContextWindow: 98304, MaxOutputTokens: 8192})
	if err != nil {
		t.Fatal(err)
	}
	r, err := ps.Save(ctx, providers.SaveInput{Name: "goal fixture reviewer", BaseURL: "http://127.0.0.1:8088/v1", Model: "fixture", ContextWindow: 32768, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if len(contract.WritablePaths) == 0 {
		contract.WritablePaths = []string{"sum.js"}
	}
	ts := taskengine.NewService(db)
	task, err := ts.Create(ctx, taskengine.CreateTaskInput{ProjectID: project.ID, Title: "Correct sum", Objective: "addition returns the correct result", OriginalRequest: "fix arithmetic and verify real command output", Actor: "owner", Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "positive and negative addition pass the unchanged acceptance test"}}, Contract: contract})
	if err != nil {
		t.Fatal(err)
	}
	return New(ts, p, ps), ts, task, StartGoalInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Actor: "owner", ProviderID: w.ID, ReviewerProviderID: r.ID, ApplyChanges: true, MaxActions: 60, MaxAttempts: 5, MaxPlans: 3, MaxSeconds: 120}, root, db
}

func advanceGoalUntilIdle(t *testing.T, c *Service, ts *taskengine.Service, g taskengine.GoalRun) taskengine.GoalRun {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for g.State == taskengine.GoalActive && time.Now().Before(deadline) {
		_, err := c.AdvanceGoal(context.Background(), g.ID)
		var loadErr error
		g, loadErr = ts.Goal(context.Background(), g.ID)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if err != nil && g.State != taskengine.GoalActive {
			t.Fatalf("action failed: state=%s reason=%s err=%v", g.State, g.Reason, err)
		}
		if g.State == taskengine.GoalActive {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return g
}

func TestGoalRunnerWritesRunsRollsBackReplansAndCompletes(t *testing.T) {
	a := &goalAdapter{failures: 2}
	c, ts, task, input, root := newGoalFixture(t, a, taskengine.GoalContract{})
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalCompleted || a.proposals != 3 || a.plans != 2 || a.reviews != 1 || !a.sawFailure || !a.sawEscalation {
		t.Fatalf("goal=%+v adapter=%+v", g, a)
	}
	body, err := os.ReadFile(filepath.Join(root, "sum.js"))
	if err != nil || !strings.Contains(string(body), "a+b") {
		t.Fatalf("actual file=%s err=%v", body, err)
	}
	task, err = ts.Get(context.Background(), task.ID)
	if err != nil || task.State != taskengine.StateCompleted {
		t.Fatalf("task=%+v err=%v", task, err)
	}
	rows, err := ts.GoalActions(context.Background(), g.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	rollbacks := 0
	for _, row := range rows {
		if row.Receipt["rolled_back"] == true {
			rollbacks++
		}
		if row.State != "observed" {
			t.Fatalf("missing durable receipt: %+v", row)
		}
	}
	if rollbacks != 2 {
		t.Fatalf("rollback receipts=%d", rollbacks)
	}
}

func TestGoalRunnerFeedsObservedReviewerFindingIntoCorrection(t *testing.T) {
	a := &goalAdapter{rejectReviews: 1}
	c, ts, _, input, root := newGoalFixture(t, a, taskengine.GoalContract{})
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	body, _ := os.ReadFile(filepath.Join(root, "sum.js"))
	if g.State != taskengine.GoalCompleted || !a.sawReviewFailure || a.proposals != 2 || a.reviews != 2 || !strings.Contains(string(body), "arithmetic documentation") {
		t.Fatalf("goal=%+v adapter=%+v source=%s", g, a, body)
	}
}

func TestGoalRunnerWaitsForExactUserAcceptance(t *testing.T) {
	a := &goalAdapter{}
	c, ts, task, input, _ := newGoalFixture(t, a, taskengine.GoalContract{RequireUserAcceptance: true})
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalWaiting || !strings.Contains(g.Reason, "user acceptance") {
		t.Fatalf("goal=%+v", g)
	}
	task, _ = ts.Get(context.Background(), task.ID)
	report, err := ts.EvaluateCompletion(context.Background(), task.ID, task.Revision)
	if err != nil || !report.AwaitingUserAcceptance || task.State == taskengine.StateCompleted {
		t.Fatalf("report=%+v task=%+v err=%v", report, task, err)
	}
	if _, err = ts.RecordUserAcceptance(context.Background(), task.ID, task.Revision, report.SubjectHash, "owner"); err != nil {
		t.Fatal(err)
	}
	if g, err = ts.ResumeGoal(context.Background(), g.ID, task.Revision); err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalCompleted || a.proposals != 1 || a.reviews != 1 {
		t.Fatalf("resume replayed effects: %+v adapter=%+v", g, a)
	}
}

func TestGoalRunnerHonorsPlanReviewAndManualWriteMode(t *testing.T) {
	a := &goalAdapter{}
	c, ts, task, input, root := newGoalFixture(t, a, taskengine.GoalContract{RequirePlanReview: true})
	input.ApplyChanges = false
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalWaiting || a.proposals != 0 {
		t.Fatalf("executed before plan review: %+v adapter=%+v", g, a)
	}
	task, _ = ts.Get(context.Background(), task.ID)
	task, err = ts.ReviewPlan(context.Background(), task.ID, task.Revision, task.Plan.SubjectHash, "owner", "approved", "matches scope")
	if err != nil {
		t.Fatal(err)
	}
	g, err = ts.ResumeGoal(context.Background(), g.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalWaiting || a.proposals != 1 || a.reviews != 0 {
		t.Fatalf("manual mode=%+v adapter=%+v", g, a)
	}
	body, _ := os.ReadFile(filepath.Join(root, "sum.js"))
	if string(body) != "module.exports=(a,b)=>a-b;\n" {
		t.Fatalf("manual mode wrote source: %s", body)
	}
}

func TestGoalRunnerStopCancelsDispatchedProviderWithoutWritingOrReplaying(t *testing.T) {
	a := &goalAdapter{blockProposal: true, entered: make(chan struct{})}
	c, ts, _, input, root := newGoalFixture(t, a, taskengine.GoalContract{})
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err = c.AdvanceGoal(context.Background(), g.ID); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = c.AdvanceGoal(context.Background(), g.ID) }()
	select {
	case <-a.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not dispatch")
	}
	if _, err = c.StopGoal(context.Background(), g.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop failed to cancel dispatched provider")
	}
	g, err = ts.Goal(context.Background(), g.ID)
	if err != nil || g.State != taskengine.GoalStopped {
		t.Fatalf("goal=%+v err=%v", g, err)
	}
	if a.proposals != 1 {
		t.Fatalf("provider calls=%d", a.proposals)
	}
	body, _ := os.ReadFile(filepath.Join(root, "sum.js"))
	if string(body) != "module.exports=(a,b)=>a-b;\n" {
		t.Fatalf("source changed after Stop: %s", body)
	}
	x, err := ts.Execution(context.Background(), g.TaskID)
	if err != nil || x.Task.State != taskengine.StatePaused || x.Run.State != taskengine.RunPaused {
		t.Fatalf("execution not paused: %+v err=%v", x, err)
	}
}

func TestGoalSchedulerWorksWithoutAClientAndJoinsWorkers(t *testing.T) {
	a := &goalAdapter{}
	c, ts, _, input, _ := newGoalFixture(t, a, taskengine.GoalContract{})
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); c.ServeGoals(ctx) }()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()
wait:
	for {
		select {
		case <-deadline.C:
			t.Fatal("server-owned goal did not complete")
		case <-poll.C:
			g, err = ts.Goal(context.Background(), g.ID)
			if err != nil {
				t.Fatal(err)
			}
			if g.State == taskengine.GoalCompleted {
				break wait
			}
			if g.State != taskengine.GoalActive {
				t.Fatalf("goal stopped: %+v", g)
			}
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("goal workers did not join")
	}
	if a.proposals != 1 || a.plans != 1 || a.reviews != 1 {
		t.Fatalf("duplicate effect dispatch: %+v", a)
	}
}

func TestGoalRunnerActionBudgetPausesExecutionWithoutCallingItComplete(t *testing.T) {
	a := &goalAdapter{}
	c, ts, _, input, _ := newGoalFixture(t, a, taskengine.GoalContract{})
	input.MaxActions = 3
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err = c.AdvanceGoal(context.Background(), g.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = c.AdvanceGoal(context.Background(), g.ID); err == nil {
		t.Fatal("exhausted budget dispatched another action")
	}
	g, _ = ts.Goal(context.Background(), g.ID)
	x, err := ts.Execution(context.Background(), g.TaskID)
	if err != nil || g.State != taskengine.GoalBudgetExhausted || x.Task.State != taskengine.StatePaused || a.proposals != 0 {
		t.Fatalf("goal=%+v task=%+v adapter=%+v err=%v", g, x.Task, a, err)
	}
}

func TestGoalStopTerminatesActualManagedCommandAndRetainsUncertainEffect(t *testing.T) {
	c, ts, _, input, root := newGoalFixture(t, &goalAdapter{}, taskengine.GoalContract{})
	if err := os.WriteFile(filepath.Join(root, "test.js"), []byte("console.log('owned long check');setInterval(()=>{},1000);\n"), 0600); err != nil {
		t.Fatal(err)
	}
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err = c.AdvanceGoal(context.Background(), g.ID); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = c.AdvanceGoal(context.Background(), g.ID) }()
	var job product.Job
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := c.products.ListJobs(context.Background(), 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) > 0 && jobs[0].State == "running" {
			job = jobs[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.ID == "" {
		t.Fatal("managed command did not start")
	}
	if _, err = c.StopGoal(context.Background(), g.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("goal Stop did not interrupt verification")
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err = c.products.GetJob(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.State != "running" && job.State != "queued" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.State != "canceled" || job.Result["process_group_terminated_on_cancel"] != true {
		t.Fatalf("actual process cancellation was not proven: %+v", job)
	}
	x, err := ts.Execution(context.Background(), g.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	uncertain := false
	for _, e := range x.Effects {
		if e.Action == "workspace.run" && e.State == taskengine.EffectUncertain {
			uncertain = true
		}
	}
	if !uncertain || x.Task.State != taskengine.StatePaused {
		t.Fatalf("interrupted effect was lost or automatically repeated: %+v", x)
	}
}

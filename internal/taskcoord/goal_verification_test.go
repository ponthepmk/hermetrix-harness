package taskcoord

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

const heldAcceptance = `const fs=require('node:fs');fs.appendFileSync('starts','x');setInterval(()=>{if(fs.existsSync('release')){require('node:assert/strict').equal(require('./sum')(2,3),5);console.log('acceptance passed');process.exit(0)}},25);`

func TestGoalStopCancelsOwnJobAfterConcurrentInterruption(t *testing.T) {
	c, ts, _, input, root := newGoalFixture(t, &goalAdapter{}, taskengine.GoalContract{})
	g, job := startHeldGoal(t, c, input, root)
	ctx := context.Background()
	// Force the worker's interruption to commit before the Stop caller reads
	// effects. The command is still live, but its effect is now uncertain.
	if _, err := ts.StopGoal(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if err := ts.InterruptGoalExecution(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.StopGoal(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	current, err := c.products.GetJob(ctx, job.ID)
	if err != nil || !current.CancelRequested {
		t.Fatalf("Stop skipped its live uncertain operation: %+v err=%v", current, err)
	}
	current = waitCanceledVerification(t, c, job.ID)
	if current.State != "canceled" || current.Result["process_group_terminated_on_cancel"] != true {
		t.Fatalf("actual process cancellation was not proven: %+v", current)
	}
}

func startHeldGoal(t *testing.T, c *Service, input StartGoalInput, root string) (taskengine.GoalRun, product.Job) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "test.js"), []byte(heldAcceptance), 0600); err != nil {
		t.Fatal(err)
	}
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		if _, err = c.AdvanceGoal(context.Background(), g.ID); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := c.products.ListJobs(context.Background(), 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) == 1 && jobs[0].State == "running" {
			if _, err = os.Stat(filepath.Join(root, "starts")); err == nil {
				g, err = c.tasks.Goal(context.Background(), g.ID)
				if err != nil {
					t.Fatal(err)
				}
				return g, jobs[0]
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("actual held Node check did not start")
	return g, product.Job{}
}

func waitCanceledVerification(t *testing.T, c *Service, id string) product.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := c.products.GetJob(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State != "queued" && job.State != "running" {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("owned verification process did not settle")
	return product.Job{}
}

func TestGoalAsyncVerificationPollsWithoutBudgetOrRepeatedCommands(t *testing.T) {
	c, ts, _, input, root := newGoalFixture(t, &goalAdapter{}, taskengine.GoalContract{})
	input.VerificationTimeoutSeconds = 900
	g, job := startHeldGoal(t, c, input, root)
	if job.Payload["timeout_seconds"] != float64(900) {
		t.Fatalf("long frozen check=%+v", job.Payload)
	}
	if g.ClaimToken != "" {
		t.Fatal("worker retained a claim while waiting for process")
	}
	before := g.ActionsUsed
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = c.AdvanceGoal(context.Background(), g.ID) }()
	}
	wg.Wait()
	g, err := ts.Goal(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := c.products.ListJobs(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if g.ActionsUsed != before || g.AttemptsUsed != 1 || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("poll consumed budget or relaunched: goal=%+v jobs=%+v", g, jobs)
	}
	x, err := ts.Execution(context.Background(), g.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.VerifyFrozen(context.Background(), taskengine.RunAuthority{RunID: x.Run.ID, LeaseToken: x.Run.LeaseToken}, x.Proposal.ID, "owner"); err == nil {
		t.Fatal("manual verification duplicated async command")
	}
	// A second task's planning can run while this goal's check remains live.
	second, err := ts.Create(context.Background(), taskengine.CreateTaskInput{ProjectID: x.Task.ProjectID, Title: "second goal", Objective: "independent planning", OriginalRequest: "plan another task", Actor: "owner", Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "independent acceptance"}}, Contract: taskengine.GoalContract{WritablePaths: []string{"sum.js"}}})
	if err != nil {
		t.Fatal(err)
	}
	input.TaskID = second.ID
	input.ExpectedTaskRevision = second.Revision
	other, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.AdvanceGoal(context.Background(), other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = c.StopGoal(context.Background(), other.ID); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalCompleted {
		t.Fatalf("goal=%+v", g)
	}
	starts, err := os.ReadFile(filepath.Join(root, "starts"))
	if err != nil || string(starts) != "x" {
		t.Fatalf("command replayed=%s err=%v", starts, err)
	}
	v, err := ts.GoalVerification(context.Background(), g.ID, x.Proposal.ID)
	if err != nil || v.State != "completed" {
		t.Fatalf("checkpoint=%+v err=%v", v, err)
	}
}

func TestGoalAsyncVerificationAdoptsLostJobCheckpointWithoutReplay(t *testing.T) {
	c, ts, _, input, root, db := newGoalFixtureWithStore(t, &goalAdapter{}, taskengine.GoalContract{})
	g, job := startHeldGoal(t, c, input, root)
	x, err := ts.Execution(context.Background(), g.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	v, err := ts.GoalVerification(context.Background(), g.ID, x.Proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := decodeGoalVerification(v)
	if err != nil {
		t.Fatal(err)
	}
	p.JobID = ""
	body, _ := json.Marshal(p)
	if _, err = db.DB.Exec(`UPDATE task_goal_verifications SET progress_json=? WHERE proposal_id=?`, string(body), x.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = c.AdvanceGoal(context.Background(), g.ID); err != nil {
		t.Fatal(err)
	}
	v, err = ts.GoalVerification(context.Background(), g.ID, x.Proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err = decodeGoalVerification(v)
	if err != nil || p.JobID != job.ID {
		t.Fatalf("lost acknowledgement was not recovered: %+v err=%v", p, err)
	}
	jobs, _ := c.products.ListJobs(context.Background(), 10)
	if len(jobs) != 1 {
		t.Fatalf("replayed job: %+v", jobs)
	}
	if err = os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if g = advanceGoalUntilIdle(t, c, ts, g); g.State != taskengine.GoalCompleted {
		t.Fatalf("goal=%+v", g)
	}
}

func TestGoalAsyncVerificationDeadlineAndRootChangeCancelOwnedJob(t *testing.T) {
	for _, mode := range []string{"deadline", "root"} {
		t.Run(mode, func(t *testing.T) {
			c, ts, task, input, root, db := newGoalFixtureWithStore(t, &goalAdapter{}, taskengine.GoalContract{})
			g, job := startHeldGoal(t, c, input, root)
			if mode == "deadline" {
				if _, err := db.DB.Exec(`UPDATE task_goal_runs SET deadline=? WHERE id=?`, time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), g.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := c.products.SaveProject(context.Background(), product.ProjectInput{ID: task.ProjectID, Name: "moved", RootPath: t.TempDir()}); err != nil {
					t.Fatal(err)
				}
			}
			_, _ = c.AdvanceGoal(context.Background(), g.ID)
			canceled := waitCanceledVerification(t, c, job.ID)
			if canceled.State != "canceled" || canceled.Result["process_group_terminated_on_cancel"] != true {
				t.Fatalf("cancellation not proven: %+v", canceled)
			}
			g, err := ts.Goal(context.Background(), g.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := taskengine.GoalWaiting
			if mode == "deadline" {
				want = taskengine.GoalBudgetExhausted
			}
			if g.State != want {
				t.Fatalf("goal=%+v", g)
			}
			x, err := ts.Execution(context.Background(), g.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			uncertain := false
			for _, e := range x.Effects {
				if e.OperationID == job.Payload["operation_id"] && e.State == taskengine.EffectUncertain {
					uncertain = true
				}
			}
			if !uncertain {
				t.Fatal("canceled operation was treated as complete")
			}
		})
	}
}

func TestGoalAsyncVerificationReopenRetainsJobAndNeverReplays(t *testing.T) {
	c, ts, _, input, root, db := newGoalFixtureWithStore(t, &goalAdapter{}, taskengine.GoalContract{})
	g, job := startHeldGoal(t, c, input, root)
	x, err := ts.Execution(context.Background(), g.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	dataRoot := db.Root
	c.products.Close()
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(context.Background(), dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	products := product.NewService(reopened, skills.NewService(reopened))
	defer products.Close()
	tasks := taskengine.NewService(reopened)
	if _, err = products.RecoverInterruptedJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = tasks.RecoverGoals(context.Background()); err != nil {
		t.Fatal(err)
	}
	v, err := tasks.GoalVerification(context.Background(), g.ID, x.Proposal.ID)
	if err != nil || v.State != "interrupted" {
		t.Fatalf("recovery=%+v err=%v", v, err)
	}
	g, err = tasks.Goal(context.Background(), g.ID)
	if err != nil || g.State != taskengine.GoalWaiting || g.ClaimToken != "" {
		t.Fatalf("reopened goal=%+v err=%v", g, err)
	}
	rebuilt := New(tasks, products, providers.NewService(reopened, &goalAdapter{}))
	if _, err = tasks.ResumeGoal(context.Background(), g.ID, x.Task.Revision); err != nil {
		t.Fatal(err)
	}
	_, _ = rebuilt.AdvanceGoal(context.Background(), g.ID)
	g, err = tasks.Goal(context.Background(), g.ID)
	if err != nil || g.State != taskengine.GoalWaiting {
		t.Fatalf("interrupted check resumed automatically: %+v err=%v", g, err)
	}
	jobs, err := products.ListJobs(context.Background(), 10)
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("reopened jobs=%+v err=%v", jobs, err)
	}
	starts, err := os.ReadFile(filepath.Join(root, "starts"))
	if err != nil || strings.TrimSpace(string(starts)) != "x" {
		t.Fatalf("process repeated after reopen: %s err=%v", starts, err)
	}
}

func TestGoalSchema77UpgradePreservesExistingGoalBudget(t *testing.T) {
	c, ts, _, input, _, db := newGoalFixtureWithStore(t, &goalAdapter{}, taskengine.GoalContract{})
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.AdvanceGoal(context.Background(), g.ID); err != nil {
		t.Fatal(err)
	}
	g, err = ts.Goal(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.products.Close()
	for _, query := range []string{`DROP TABLE task_goal_verifications`, `ALTER TABLE task_goal_runs DROP COLUMN poll_after`, `PRAGMA user_version=77`} {
		if _, err = db.DB.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	dataRoot := db.Root
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(context.Background(), dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	tasks := taskengine.NewService(reopened)
	got, err := tasks.Goal(context.Background(), g.ID)
	if err != nil || got.ActionsUsed != g.ActionsUsed || got.PlansUsed != g.PlansUsed || !got.Deadline.Equal(g.Deadline) {
		t.Fatalf("upgraded goal=%+v before=%+v err=%v", got, g, err)
	}
	if _, err = tasks.ReadyGoals(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	var violations int
	if err = reopened.DB.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("foreign key violations=%d err=%v", violations, err)
	}
}

func TestGoalAsyncVerificationWaitsForCapacityWithoutBudgetAndCancelsOnlyOwnJob(t *testing.T) {
	c, ts, task, input, root := newGoalFixture(t, &goalAdapter{}, taskengine.GoalContract{})
	if err := os.WriteFile(filepath.Join(root, "test.js"), []byte(heldAcceptance), 0600); err != nil {
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
	var reserved []product.Job
	for _, id := range []string{"reserve-1", "reserve-2"} {
		job, err := c.products.StartVerificationCommand(context.Background(), product.CommandInput{ProjectID: task.ProjectID, ExpectedRoot: g.Config.ProjectRoot, OperationID: id, Actor: "another owner action", Executable: "node", Arguments: []string{"-e", "setInterval(()=>{},1000)"}, WorkingDir: ".", TimeoutSeconds: 60})
		if err != nil {
			t.Fatal(err)
		}
		reserved = append(reserved, job)
	}
	if _, err = c.AdvanceGoal(context.Background(), g.ID); err != nil {
		t.Fatal(err)
	}
	g, err = ts.Goal(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	before := g.ActionsUsed
	for i := 0; i < 8; i++ {
		if _, err = c.AdvanceGoal(context.Background(), g.ID); err != nil {
			t.Fatal(err)
		}
	}
	g, err = ts.Goal(context.Background(), g.ID)
	if err != nil || g.ActionsUsed != before {
		t.Fatalf("capacity polling charged actions: %+v err=%v", g, err)
	}
	jobs, err := c.products.ListJobs(context.Background(), 10)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("capacity exceeded: %+v err=%v", jobs, err)
	}
	if _, err = c.products.CancelJob(context.Background(), reserved[0].ID); err != nil {
		t.Fatal(err)
	}
	waitCanceledVerification(t, c, reserved[0].ID)
	if _, err = c.AdvanceGoal(context.Background(), g.ID); err != nil {
		t.Fatal(err)
	}
	x, err := ts.Execution(context.Background(), g.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	v, err := ts.GoalVerification(context.Background(), g.ID, x.Proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := decodeGoalVerification(v)
	if err != nil || p.JobID == "" {
		t.Fatalf("queued command not dispatched after capacity release: %+v err=%v", p, err)
	}
	if _, err = c.StopGoal(context.Background(), g.ID); err != nil {
		t.Fatal(err)
	}
	if job := waitCanceledVerification(t, c, p.JobID); job.State != "canceled" {
		t.Fatalf("own command remained: %+v", job)
	}
	other, err := c.products.GetJob(context.Background(), reserved[1].ID)
	if err != nil || (other.State != "queued" && other.State != "running") {
		t.Fatalf("goal canceled another action's process: %+v err=%v", other, err)
	}
	if _, err = c.products.CancelJob(context.Background(), reserved[1].ID); err != nil {
		t.Fatal(err)
	}
}

// This intentionally takes over two minutes: a short sleep or mocked clock
// cannot prove that the real process survives the former frozen-check limit.
func TestGoalAsyncVerificationOutlivesLegacyDeadline(t *testing.T) {
	c, ts, _, input, root := newGoalFixture(t, &goalAdapter{}, taskengine.GoalContract{})
	input.VerificationTimeoutSeconds = 150
	input.MaxSeconds = 180
	g, job := startHeldGoal(t, c, input, root)
	before := g.ActionsUsed
	deadline := time.Now().Add(121 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := c.AdvanceGoal(context.Background(), g.ID); err != nil {
			t.Fatal(err)
		}
		observed, err := c.products.GetJob(context.Background(), job.ID)
		if err != nil || observed.State != "running" {
			t.Fatalf("long check terminated before owner deadline: %+v err=%v", observed, err)
		}
		time.Sleep(time.Second)
	}
	g, err := ts.Goal(context.Background(), g.ID)
	if err != nil || g.State != taskengine.GoalActive || g.ActionsUsed != before {
		t.Fatalf("long observation consumed budget or completed early: %+v err=%v", g, err)
	}
	t.Logf("same Node job %s observed running for over 120 seconds with %d unchanged actions", job.ID, before)
	if err = os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalCompleted {
		t.Fatalf("long workflow did not complete: %+v", g)
	}
	starts, err := os.ReadFile(filepath.Join(root, "starts"))
	if err != nil || string(starts) != "x" {
		t.Fatalf("long command replayed: %s err=%v", starts, err)
	}
}

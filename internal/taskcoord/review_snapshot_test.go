package taskcoord

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
)

type heldReviewAdapter struct {
	base             goalAdapter
	entered, release chan struct{}
}

func (a *heldReviewAdapter) StreamChat(ctx context.Context, p providers.Profile, key string, r providers.ChatRequest, emit func(providers.Delta) error) (providers.Completion, error) {
	if r.Tools[0].Function.Name == "submit_review" {
		close(a.entered)
		select {
		case <-a.release:
		case <-ctx.Done():
			return providers.Completion{}, ctx.Err()
		}
	}
	return a.base.StreamChat(ctx, p, key, r, emit)
}

func TestGoalRejectsSourceOrRootChangedDuringReview(t *testing.T) {
	for _, mode := range []string{"source", "root", "other_output"} {
		t.Run(mode, func(t *testing.T) {
			c, ts, task, input, root, db := newGoalFixtureWithStore(t, &goalAdapter{}, taskengine.GoalContract{WritablePaths: []string{"sum.js", "previous.js"}})
			if err := os.WriteFile(filepath.Join(root, "previous.js"), []byte("// earlier output\n"), 0600); err != nil {
				t.Fatal(err)
			}
			adapter := &heldReviewAdapter{entered: make(chan struct{}), release: make(chan struct{})}
			c.providers = providers.NewService(db, adapter)
			g, err := c.StartGoal(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			ready := false
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				x, err := ts.Execution(context.Background(), task.ID)
				if err != nil {
					t.Fatal(err)
				}
				if x.Proposal != nil && x.Proposal.State == taskengine.ProposalAwaitingReview {
					ready = true
					break
				}
				if _, err = c.AdvanceGoal(context.Background(), g.ID); err != nil {
					t.Fatal(err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !ready {
				t.Fatal("real check did not become ready for review")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := c.AdvanceGoal(ctx, g.ID); done <- err }()
			select {
			case <-adapter.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("review provider not entered")
			}
			changedPath := "sum.js"
			changedBody := "// owner changed this after verification\n"
			if mode != "root" {
				if mode == "other_output" {
					changedPath = "previous.js"
				}
				if err = os.WriteFile(filepath.Join(root, changedPath), []byte(changedBody), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err = c.products.SaveProject(context.Background(), product.ProjectInput{ID: task.ProjectID, Name: "moved during review", RootPath: t.TempDir()}); err != nil {
					t.Fatal(err)
				}
			}
			close(adapter.release)
			select {
			case err = <-done:
				if err == nil {
					t.Fatal("stale reviewer response did not report an error")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("review did not settle")
			}
			current, err := ts.Get(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			g, err = ts.Goal(context.Background(), g.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.State == taskengine.StateCompleted || g.State == taskengine.GoalCompleted {
				t.Fatalf("STALE REVIEW ACCEPTED: changed %s after real check and reviewer snapshot; task=%s goal=%s", mode, current.State, g.State)
			}
			if g.State != taskengine.GoalWaiting || g.ClaimToken != "" {
				t.Fatalf("goal must release its claim and wait: %+v", g)
			}
			x, err := ts.Execution(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = ts.LatestPostCodeReview(context.Background(), x.Proposal.ID); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("stale reviewer response was accepted: %v", err)
			}
			effects, err := ts.AttemptEffects(context.Background(), x.Attempt.ID)
			if err != nil {
				t.Fatal(err)
			}
			observed := false
			for _, effect := range effects {
				if effect.Action == "provider.review" && effect.State == taskengine.EffectObserved {
					observed = true
				}
			}
			if !observed || adapter.base.reviews != 1 {
				t.Fatalf("review must be observed once: effects=%+v reviews=%d", effects, adapter.base.reviews)
			}
			if mode != "root" {
				body, err := os.ReadFile(filepath.Join(root, changedPath))
				if err != nil || string(body) != changedBody {
					t.Fatalf("owner bytes were overwritten: %q, %v", body, err)
				}
			}
		})
	}
}

func TestGoalRejectsSourceChangedDuringAsyncVerification(t *testing.T) {
	c, ts, task, input, root := newGoalFixture(t, &goalAdapter{}, taskengine.GoalContract{})
	g, job := startHeldGoal(t, c, input, root)
	ownerBytes := "module.exports=(a,b)=>a+b; // owner's later edit\n"
	if err := os.WriteFile(filepath.Join(root, "sum.js"), []byte(ownerBytes), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	job = waitCanceledVerification(t, c, job.ID)
	if job.State != "completed" || job.Result["exit_code"] != float64(0) {
		t.Fatalf("actual check must pass against newer bytes to expose stale evidence: %+v", job)
	}
	if _, err := c.AdvanceGoal(context.Background(), g.ID); err == nil {
		t.Fatal("passing command result accepted a changed source snapshot")
	}
	g, err := ts.Goal(context.Background(), g.ID)
	if err != nil || g.State != taskengine.GoalWaiting || g.ClaimToken != "" {
		t.Fatalf("goal=%+v err=%v", g, err)
	}
	current, err := ts.Get(context.Background(), task.ID)
	if err != nil || current.State == taskengine.StateCompleted {
		t.Fatalf("task=%+v err=%v", current, err)
	}
	body, err := os.ReadFile(filepath.Join(root, "sum.js"))
	if err != nil || string(body) != ownerBytes {
		t.Fatalf("later owner edit was rolled back: %q err=%v", body, err)
	}
	starts, err := os.ReadFile(filepath.Join(root, "starts"))
	if err != nil || string(starts) != "x" {
		t.Fatalf("verification was replayed: %q err=%v", starts, err)
	}
}

func TestGoalFinishRechecksSourceAfterTaskWasCompletedSeparately(t *testing.T) {
	c, ts, task, input, root := newGoalFixture(t, &goalAdapter{}, taskengine.GoalContract{})
	ctx := context.Background()
	g, err := c.StartGoal(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	var x taskengine.ExecutionSnapshot
	for time.Now().Before(deadline) {
		x, err = ts.Execution(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if x.Proposal != nil && x.Proposal.State == taskengine.ProposalAwaitingReview {
			break
		}
		if _, err = c.AdvanceGoal(ctx, g.ID); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if x.Proposal == nil || x.Proposal.State != taskengine.ProposalAwaitingReview {
		t.Fatal("real verification did not become ready")
	}
	out, err := c.Review(ctx, taskengine.RunAuthority{RunID: x.Run.ID, LeaseToken: x.Run.LeaseToken}, x.Proposal.ID, input.ReviewerProviderID)
	if err != nil || out.Task.State != taskengine.StateCompleted {
		t.Fatalf("separate task review=%+v err=%v", out.Task, err)
	}
	if _, err = ts.WaitGoal(ctx, g.ID, "separate task review completed"); err != nil {
		t.Fatal(err)
	}
	if _, err = ts.ResumeGoal(ctx, g.ID, out.Task.Revision); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "sum.js"), []byte("// owner changed completed task output\n"), 0600); err != nil {
		t.Fatal(err)
	}
	g, err = c.AdvanceGoal(ctx, g.ID)
	if err == nil || g.State != taskengine.GoalWaiting || g.ClaimToken != "" {
		t.Fatalf("final goal fence accepted stale task completion: %+v err=%v", g, err)
	}
}

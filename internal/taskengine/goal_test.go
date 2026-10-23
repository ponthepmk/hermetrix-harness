package taskengine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/store"
)

func goalConfig() GoalConfig {
	return GoalConfig{ProviderID: "writer", ProviderRevision: "writer-r1", ReviewerProviderID: "reviewer", ReviewerProviderRevision: "review-r1",
		MaxActions: 10, MaxAttempts: 2, MaxPlans: 2, MaxSeconds: 60}
}

func TestGoalSurvivesDatabaseCloseAndReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(db)
	task := createPlannedTask(t, s)
	g, err := s.CreateGoal(ctx, task.ID, task.Revision, "owner", goalConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.ClaimGoalAction(ctx, g.ID, "plan"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s = NewService(db)
	if err = s.RecoverGoals(ctx); err != nil {
		t.Fatal(err)
	}
	g, err = s.Goal(ctx, g.ID)
	if err != nil || g.State != GoalWaiting || g.ActionsUsed != 1 || g.PlansUsed != 1 {
		t.Fatalf("reopened goal=%+v err=%v", g, err)
	}
	actions, err := s.GoalActions(ctx, g.ID, 10)
	if err != nil || len(actions) != 1 || actions[0].State != "interrupted" {
		t.Fatalf("reopened actions=%+v err=%v", actions, err)
	}
}

func TestGoalClaimsAreExclusiveAndCrashRecoveryNeverReplays(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, s)
	g, err := s.CreateGoal(ctx, task.ID, task.Revision, "owner", goalConfig())
	if err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.ClaimGoalAction(ctx, g.ID, "attempt"); err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("claims=%d", wins.Load())
	}
	if err = s.RecoverGoals(ctx); err != nil {
		t.Fatal(err)
	}
	g, err = s.Goal(ctx, g.ID)
	if err != nil || g.State != GoalWaiting || g.ActionsUsed != 1 || g.AttemptsUsed != 1 || g.ClaimToken != "" {
		t.Fatalf("goal=%+v err=%v", g, err)
	}
	actions, err := s.GoalActions(ctx, g.ID, 10)
	if err != nil || len(actions) != 1 || actions[0].State != "interrupted" {
		t.Fatalf("actions=%+v err=%v", actions, err)
	}
	if _, _, err = s.ClaimGoalAction(ctx, g.ID, "attempt"); err == nil {
		t.Fatal("interrupted goal replayed without explicit resume")
	}
	if _, err = s.ResumeGoal(ctx, g.ID, task.Revision); err != nil {
		t.Fatal(err)
	}
	claimed, a, err := s.ClaimGoalAction(ctx, g.ID, "attempt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishGoalAction(ctx, g.ID, claimed.ClaimToken, GoalActive, "", map[string]any{"receipt": "observed"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishGoalAction(ctx, g.ID, a.ID, GoalActive, "", nil); err == nil {
		t.Fatal("finished action receipt replayed")
	}
	if _, _, err = s.ClaimGoalAction(ctx, g.ID, "attempt"); err == nil {
		t.Fatal("attempt budget reset on resume")
	}
	g, _ = s.Goal(ctx, g.ID)
	if g.State != GoalBudgetExhausted {
		t.Fatalf("budget state=%+v", g)
	}
}

func TestGoalScopeOwnerStopAndCompletionGates(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, s)
	c := goalConfig()
	c.ApplyChanges = true
	if _, err := s.CreateGoal(ctx, task.ID, task.Revision, "owner", c); err == nil {
		t.Fatal("automatic writes without frozen writable paths")
	}
	c.ApplyChanges = false
	g, err := s.CreateGoal(ctx, task.ID, task.Revision, "owner", c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateGoal(ctx, task.ID, task.Revision, "owner", c); err == nil {
		t.Fatal("duplicate active goal")
	}
	if _, err = s.Goal(identity.WithPrincipal(ctx, "foreign"), g.ID); err == nil {
		t.Fatal("foreign principal read a goal")
	}
	claimed, _, err := s.ClaimGoalAction(ctx, g.ID, "finish")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishGoalAction(ctx, g.ID, claimed.ClaimToken, GoalCompleted, "model says done", nil); err == nil {
		t.Fatal("goal closed without validated task completion")
	}
	g, err = s.Goal(ctx, g.ID)
	if err != nil || g.State != GoalWaiting || g.ClaimToken != "" {
		t.Fatalf("invalid completion must release its claim for inspection: %+v err=%v", g, err)
	}
	g, err = s.StopGoal(ctx, g.ID)
	if err != nil || g.State != GoalStopped {
		t.Fatalf("stop=%+v err=%v", g, err)
	}
	if _, err = s.FinishGoalAction(ctx, g.ID, claimed.ClaimToken, GoalActive, "", nil); err == nil {
		t.Fatal("released stale claim finalized a stopped goal")
	}
	if _, err = s.ResumeGoal(ctx, g.ID, task.Revision); err == nil {
		t.Fatal("stopped goal resumed")
	}
}

func TestGoalRejectsStaleRevisionAndChangedRequirements(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, s)
	g, err := s.CreateGoal(ctx, task.ID, task.Revision, "owner", goalConfig())
	if err != nil {
		t.Fatal(err)
	}
	revised, err := s.ReviseRequirements(ctx, ReviseRequirementsInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Criteria: task.Requirement.Criteria, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.ClaimGoalAction(ctx, g.ID, "plan"); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("claim after revision=%v", err)
	}
	if _, err = s.WaitGoal(ctx, g.ID, "changed"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResumeGoal(ctx, g.ID, revised.Revision); err == nil {
		t.Fatal("new requirements reused old automatic-write consent")
	}
}

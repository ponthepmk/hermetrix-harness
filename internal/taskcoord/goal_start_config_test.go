package taskcoord

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/taskengine"
)

func TestGoalStartNamedDefaultsAndConflict(t *testing.T) {
	var a goalAdapter
	c, ts, task, input, _ := newGoalFixture(t, &a, taskengine.GoalContract{})

	input.GoalID = "goal-1"
	input.MaxActions = 0
	input.MaxAttempts = 0
	input.MaxPlans = 0
	input.MaxSeconds = 0
	input.VerificationTimeoutSeconds = 0
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if g.Config.MaxActions != 100 || g.Config.MaxAttempts != 8 || g.Config.MaxPlans != 3 || g.Config.MaxSeconds != 3600 || g.Config.VerificationTimeoutSeconds != 120 {
		t.Fatalf("defaults: %+v", g.Config)
	}

	input2 := input
	input2.MaxActions = 100
	input2.MaxAttempts = 8
	input2.MaxPlans = 3
	input2.MaxSeconds = 3600
	input2.VerificationTimeoutSeconds = 120
	g2, err := c.StartGoal(context.Background(), input2)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !reflect.DeepEqual(g, g2) {
		t.Fatalf("replay mismatch: %+v %+v", g, g2)
	}

	input.MaxActions = 101
	if run, err := c.StartGoal(context.Background(), input); !errors.Is(err, taskengine.ErrStaleRevision) || run != (taskengine.GoalRun{}) {
		t.Fatalf("conflict: run=%v err=%v", run, err)
	}
	if stored, err := ts.Goal(context.Background(), "goal-1"); err != nil || !reflect.DeepEqual(stored, g) {
		t.Fatalf("stored goal: err=%v g=%v", err, g)
	}
	if goals, err := ts.GoalsForTask(context.Background(), task.ID); err != nil || len(goals) != 1 {
		t.Fatalf("goals for task: err=%v len=%d", err, len(goals))
	}

	ctx := identity.WithPrincipal(context.Background(), "foreign")
	if run2, err := c.StartGoal(ctx, input2); err == nil || run2 != (taskengine.GoalRun{}) {
		t.Fatalf("foreign: run=%v err=%v", run2, err)
	}
	if stored, err := ts.Goal(context.Background(), "goal-1"); err != nil || !reflect.DeepEqual(stored, g) {
		t.Fatalf("stored after foreign: err=%v g=%v", err, g)
	}
	if goals, err := ts.GoalsForTask(context.Background(), task.ID); err != nil || len(goals) != 1 {
		t.Fatalf("goals after foreign: err=%v len=%d", err, len(goals))
	}
	if a.plans != 0 || a.proposals != 0 || a.reviews != 0 {
		t.Fatalf("adapter counts: plans=%d proposals=%d reviews=%d", a.plans, a.proposals, a.reviews)
	}
}

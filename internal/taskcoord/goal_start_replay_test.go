package taskcoord

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"hermetrix-harness/internal/taskengine"
)

func TestGoalStartNamedReplayAcrossCompletion(t *testing.T) {
	a := &goalAdapter{failures: 1}
	c, ts, task, input, root := newGoalFixture(t, a, taskengine.GoalContract{WritablePaths: []string{"sum.js"}})
	input.GoalID = "goal-replay-1"
	orig, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var replayed StartGoalInput
	if err := json.Unmarshal(orig, &replayed); err != nil {
		t.Fatal(err)
	}
	initial, err := c.StartGoal(context.Background(), replayed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AdvanceGoal(context.Background(), replayed.GoalID); err != nil {
		t.Fatal(err)
	}
	current, err := ts.Goal(context.Background(), replayed.GoalID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ActionsUsed == 0 {
		t.Fatalf("expected action budget consumed, got 0")
	}
	observedTask, err := ts.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if observedTask.Revision <= task.Revision {
		t.Fatalf("expected task revision advanced, got %d", observedTask.Revision)
	}
	actions, err := ts.GoalActions(context.Background(), replayed.GoalID, 100)
	if err != nil {
		t.Fatal(err)
	}
	snapActions := actions
	snapProposals, snapPlans, snapReviews := a.proposals, a.plans, a.reviews
	result, err := c.StartGoal(context.Background(), replayed)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, current) {
		t.Fatalf("first replay result mismatch: got %v, want %v", result, current)
	}
	replayedCurrent, err := ts.Goal(context.Background(), replayed.GoalID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current, replayedCurrent) {
		t.Fatalf("replay changed current goal")
	}
	if a.proposals != snapProposals || a.plans != snapPlans || a.reviews != snapReviews {
		t.Fatalf("counters changed on replay: proposals=%d plans=%d reviews=%d", a.proposals, a.plans, a.reviews)
	}
	replayedActions, err := ts.GoalActions(context.Background(), replayed.GoalID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapActions, replayedActions) {
		t.Fatalf("actions changed on replay")
	}
	completed := advanceGoalUntilIdle(t, c, ts, current)
	if completed.State != taskengine.GoalCompleted {
		t.Fatalf("expected GoalCompleted, got %s", completed.State)
	}
	observedTask, err = ts.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if observedTask.State != taskengine.StateCompleted {
		t.Fatalf("expected task state completed, got %s", observedTask.State)
	}
	sum, err := os.ReadFile(filepath.Join(root, "sum.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sum), "a+b") {
		t.Fatalf("sum.js missing a+b")
	}
	if a.proposals != 2 || !a.sawFailure {
		t.Fatalf("expected proposals=2 and sawFailure, got proposals=%d sawFailure=%v", a.proposals, a.sawFailure)
	}
	completedActions, err := ts.GoalActions(context.Background(), replayed.GoalID, 100)
	if err != nil {
		t.Fatal(err)
	}
	completedProposals, completedPlans, completedReviews := a.proposals, a.plans, a.reviews
	result, err = c.StartGoal(context.Background(), replayed)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, completed) {
		t.Fatalf("terminal replay result mismatch: got %v, want %v", result, completed)
	}
	final, err := ts.Goal(context.Background(), replayed.GoalID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(completed, final) {
		t.Fatalf("replay changed completed goal")
	}
	if a.proposals != completedProposals || a.plans != completedPlans || a.reviews != completedReviews {
		t.Fatalf("counters changed on completed replay")
	}
	finalActions, err := ts.GoalActions(context.Background(), replayed.GoalID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(completedActions, finalActions) {
		t.Fatalf("actions changed on completed replay")
	}
	goals, err := ts.GoalsForTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(goals) != 1 {
		t.Fatalf("expected exactly 1 goal for task, got %d", len(goals))
	}
	if final.Deadline != initial.Deadline || final.CreatedAt != initial.CreatedAt {
		t.Fatalf("deadline or CreatedAt changed")
	}
}

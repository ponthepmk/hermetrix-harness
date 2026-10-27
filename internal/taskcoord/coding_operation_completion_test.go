package taskcoord

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/taskengine"
)

func TestCodingOperationCompletes(t *testing.T) {
	f := newCodingOperationFixture(t, &goalAdapter{failures: 1})

	sumPath := filepath.Join(f.root, "sum.js")
	testPath := filepath.Join(f.root, "test.js")

	savedTest, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("read test.js: %v", err)
	}

	result, err := f.operations.Start(context.Background(), f.input)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if result.Goal == nil {
		t.Fatal("goal is nil")
	}

	deadline := time.Now().Add(30 * time.Second)
	for result.Goal.State == taskengine.GoalActive {
		if time.Now().After(deadline) {
			t.Fatalf("goal did not complete within 30s: state=%s", result.Goal.State)
		}
		next, err := f.operations.Advance(context.Background(), f.input.OperationID)
		if err != nil {
			if !strings.Contains(err.Error(), "mandatory check node test.js failed") {
				t.Fatalf("advance: %v", err)
			}
			current, getErr := f.operations.Get(context.Background(), f.input.OperationID)
			if getErr != nil {
				t.Fatalf("get current operation: %v", getErr)
			}
			if current.Goal == nil {
				t.Fatalf("current operation goal is nil")
			}
			if current.Goal.State != taskengine.GoalActive {
				t.Fatalf("current operation goal state: %v", current.Goal.State)
			}
			actions, actionsErr := f.tasks.GoalActions(context.Background(), current.Goal.ID, 1)
			if actionsErr != nil {
				t.Fatalf("goal actions: %v", actionsErr)
			}
			if len(actions) != 1 {
				t.Fatalf("expected 1 goal action, got %d", len(actions))
			}
			if actions[0].Receipt["rolled_back"] != true {
				t.Fatalf("expected rolled_back receipt to be true")
			}
			next = current
		}
		if next.Goal == nil {
			t.Fatal("goal is nil after advance")
		}
		result = next

		time.Sleep(10 * time.Millisecond)
	}

	if result.Goal.State != taskengine.GoalCompleted {
		t.Fatalf("goal state = %s, want %s", result.Goal.State, taskengine.GoalCompleted)
	}
	if result.Task.State != taskengine.StateCompleted {
		t.Fatalf("task state = %s, want %s", result.Task.State, taskengine.StateCompleted)
	}

	sum, err := os.ReadFile(sumPath)
	if err != nil {
		t.Fatalf("read sum.js: %v", err)
	}
	if !strings.Contains(string(sum), "a+b") {
		t.Fatalf("sum.js does not contain a+b: %s", sum)
	}

	currentTest, readErr := os.ReadFile(testPath)
	if readErr != nil {
		t.Fatalf("failed to read test.js: %v", readErr)
	}
	if !bytes.Equal(savedTest, currentTest) {
		t.Fatal("test.js was modified")
	}

	if f.adapter.proposals != 2 {
		t.Fatalf("proposals = %d, want 2", f.adapter.proposals)
	}
	if !f.adapter.sawFailure {
		t.Fatal("adapter did not see failure")
	}

	assertCodingOperationCounts(t, f.data, 2, 1, 1)

	// Terminal replay must be a no-op: it must return the same completed result
	// and must not mutate any task, goal, or effect counters.
	savedActions, err := f.tasks.GoalActions(context.Background(), result.Goal.ID, 100)
	if err != nil {
		t.Fatalf("f.tasks.GoalActions: %v", err)
	}
	savedCounters := [3]int{f.adapter.plans, f.adapter.proposals, f.adapter.reviews}
	if _, err := f.tasks.GoalActions(context.Background(), result.Goal.ID, 100); err != nil {
		t.Fatalf("terminal replay GoalActions: %v", err)
	}
	replay, err := f.operations.Start(context.Background(), f.input)
	if err != nil {
		t.Fatalf("terminal replay Start: %v", err)
	}
	if !reflect.DeepEqual(replay, result) {
		t.Fatalf("terminal replay Start did not return same completed result")
	}
	replayAdvance, err := f.operations.Advance(context.Background(), f.input.OperationID)
	if err != nil {
		t.Fatalf("terminal replay Advance: %v", err)
	}
	if !reflect.DeepEqual(replayAdvance, result) {
		t.Fatalf("terminal replay Advance did not return same completed result")
	}
	currentActions, err := f.tasks.GoalActions(context.Background(), result.Goal.ID, 100)
	if err != nil {
		t.Fatalf("terminal replay current GoalActions: %v", err)
	}
	if !reflect.DeepEqual(currentActions, savedActions) {
		t.Fatalf("terminal replay changed saved actions")
	}
	if [3]int{f.adapter.plans, f.adapter.proposals, f.adapter.reviews} != savedCounters {
		t.Fatalf("terminal replay changed adapter counters")
	}
	assertCodingOperationCounts(t, f.data, 2, 1, 1)
}

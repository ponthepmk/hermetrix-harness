package taskcoord

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
)

type proposalGoalFixture struct {
	Before string `json:"before"`
	After  string `json:"after"`
	Test   string `json:"test"`
	Marker string `json:"marker"`
}

func TestGoalAutomaticallyCuratesLargeSource(t *testing.T) {
	data, err := os.ReadFile("proposal_goal_fixture.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f proposalGoalFixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(f.Before) <= 128*1024 {
		t.Fatalf("Before length %d <= 128KiB", len(f.Before))
	}
	if f.Before == f.After {
		t.Fatal("Before == After")
	}
	base := &goalAdapter{}
	c, ts, task, input, root, db := newGoalFixtureWithStore(t, base, taskengine.GoalContract{})

	testJS, err := os.ReadFile(filepath.Join(root, "test.js"))
	if err != nil {
		t.Fatalf("read test.js: %v", err)
	}
	if string(testJS) != f.Test {
		t.Fatalf("test.js != frozen Test: %q", string(testJS))
	}

	if err := os.WriteFile(filepath.Join(root, "sum.js"), []byte(f.Before), 0600); err != nil {
		t.Fatalf("write sum.js0600: %v", err)
	}

	cmd := exec.Command("node", "test.js")
	cmd.Dir = root
	err = cmd.Run()
	if err == nil {
		t.Fatal("expected node test.js to fail")
	}
	if _, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}

	a := &boundedGoalAdapter{Base: base}
	c.providers = providers.NewService(db, a)
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatalf("StartGoal: %v", err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalCompleted {
		t.Fatalf("goal state %s", g.State)
	}
	loaded, err := ts.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Get task: %v", err)
	}
	if loaded.State != taskengine.StateCompleted {
		t.Fatalf("task state %s", loaded.State)
	}

	sumJS, err := os.ReadFile(filepath.Join(root, "sum.js"))
	if err != nil {
		t.Fatalf("read sum.js: %v", err)
	}
	if string(sumJS) != f.After {
		t.Fatalf("sum.js != frozen After")
	}
	testJS2, err := os.ReadFile(filepath.Join(root, "test.js"))
	if err != nil {
		t.Fatalf("read test.js: %v", err)
	}
	if string(testJS2) != f.Test {
		t.Fatalf("test.js != frozen Test")
	}

	assertBoundedGoalCapture(t, a, f.Before, f.After, f.Test, f.Marker)

	actions, err := ts.GoalActions(context.Background(), g.ID, 100)
	if err != nil {
		t.Fatalf("GoalActions: %v", err)
	}
	if len(actions) == 0 {
		t.Fatal("no goal actions")
	}
	for _, act := range actions {
		if act.State != "observed" {
			t.Fatalf("action state %s", act.State)
		}
	}
}

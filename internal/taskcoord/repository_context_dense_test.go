package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

func TestRepositoryContextDensePlanner(t *testing.T) {
	c, _, task, _, root, _ := newGoalFixtureWithStore(t, &goalAdapter{}, taskengine.GoalContract{})
	for i := 0; i < 1200; i++ {
		name := fmt.Sprintf("unrelated_%04d_%s.go", i, strings.Repeat("x", 16))
		if err := os.WriteFile(filepath.Join(root, name), []byte("//unrelated\n"), 0o644); err != nil {
			t.Fatalf("write dense file %s: %v", name, err)
		}
	}
	ctx := context.Background()
	input := &worker.PlanTask{
		TaskID:          task.ID,
		Objective:       task.Objective,
		OriginalRequest: task.OriginalRequest,
		Criteria:        []string{"AC-1"},
		WritablePaths:   task.Requirement.Contract.WritablePaths,
	}
	if _, err := c.plannerRepositoryContext(ctx, task, input, 12800); err != nil {
		t.Fatalf("plannerRepositoryContext: %v", err)
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal planner input: %v", err)
	}
	if len(body) > 12800-2048 {
		t.Fatalf("planner body %d exceeds byte budget %d", len(body), 12800-2048)
	}
	if !input.RepositoryFilesTruncated {
		t.Fatalf("expected planner RepositoryFilesTruncated true")
	}
	if input.RepositoryFiles[0].Path != "sum.js" {
		t.Fatalf("expected sum.js first, got %s", input.RepositoryFiles[0].Path)
	}
	found := false
	for _, f := range input.RepositoryFiles {
		if f.Path == "test.js" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("test.js candidate not retained")
	}
}

func TestRepositoryContextDenseSelection(t *testing.T) {
	c, _, task, _, root, _ := newGoalFixtureWithStore(t, &goalAdapter{}, taskengine.GoalContract{})
	for i := 0; i < 1200; i++ {
		name := fmt.Sprintf("unrelated_%04d_%s.go", i, strings.Repeat("x", 16))
		if err := os.WriteFile(filepath.Join(root, name), []byte("//unrelated\n"), 0o644); err != nil {
			t.Fatalf("write dense file %s: %v", name, err)
		}
	}
	ctx := context.Background()
	selInput := &worker.FileSelectionTask{
		Objective:          task.Objective,
		StepTitle:          task.Title,
		StepInstructions:   task.OriginalRequest,
		AcceptanceCriteria: []string{"AC-1"},
	}
	if _, err := c.selectionRepositoryContext(ctx, task.ProjectID, []string{"sum.js"}, []string{"node test.js"}, selInput, 12800); err != nil {
		t.Fatalf("selectionRepositoryContext: %v", err)
	}
	body, err := json.Marshal(selInput)
	if err != nil {
		t.Fatalf("marshal selection input: %v", err)
	}
	if len(body) > 12800-2048 {
		t.Fatalf("selection body %d exceeds byte budget %d", len(body), 12800-2048)
	}
	if !selInput.CandidatesTruncated {
		t.Fatalf("expected selection CandidatesTruncated true")
	}
	if selInput.Candidates[0].Path != "sum.js" {
		t.Fatalf("expected sum.js first, got %s", selInput.Candidates[0].Path)
	}
	found := false
	for _, cnd := range selInput.Candidates {
		if cnd.Path == "test.js" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("test.js candidate not retained")
	}
}

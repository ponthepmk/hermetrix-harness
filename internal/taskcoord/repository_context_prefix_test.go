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

func TestPlannerManifestPreservesNamedPackages(t *testing.T) {
	prefixes := []string{"internal/context", "internal/agent", "internal/taskcoord", "internal/worker", "internal/projectbrain", "internal/providers"}
	c, _, task, _, root, _ := newGoalFixtureWithStore(t, &goalAdapter{}, taskengine.GoalContract{})
	for _, p := range prefixes {
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(filepath.Join(root, p, "core.go"), []byte("//core\n"), 0o644); err != nil {
			t.Fatalf("write %s/core.go: %v", p, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	for i := 0; i < 500; i++ {
		name := fmt.Sprintf("context_review_goal_source_%04d.go", i)
		if err := os.WriteFile(filepath.Join(root, "docs", name), []byte("//decoy\n"), 0o644); err != nil {
			t.Fatalf("write docs/%s: %v", name, err)
		}
	}
	task.Title = "context review goal source"
	task.Objective = "context review goal source"
	task.OriginalRequest = "Analyze " + strings.Join(prefixes, ",") + ", context review goal source"
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
	if len(body) > 10752 {
		t.Fatalf("planner body %d exceeds byte budget %d", len(body), 10752)
	}
	if !input.RepositoryFilesTruncated {
		t.Fatalf("expected planner RepositoryFilesTruncated true")
	}
	if input.RepositoryFiles[0].Path != "sum.js" {
		t.Fatalf("expected sum.js first, got %s", input.RepositoryFiles[0].Path)
	}
	for _, p := range prefixes {
		found := false
		for _, f := range input.RepositoryFiles {
			if f.Path == filepath.ToSlash(filepath.Join(p, "core.go")) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %s/core.go retained", p)
		}
	}
}

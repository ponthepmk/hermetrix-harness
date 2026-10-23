package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

type manifestRecordingAdapter struct {
	base                      goalAdapter
	plan                      worker.PlanTask
	selection                 worker.FileSelectionTask
	planBytes, selectionBytes int
}

func (a *manifestRecordingAdapter) StreamChat(ctx context.Context, p providers.Profile, key string, r providers.ChatRequest, emit func(providers.Delta) error) (providers.Completion, error) {
	switch r.Tools[0].Function.Name {
	case "submit_plan":
		a.planBytes = len(r.Messages[1].Content)
		if err := json.Unmarshal([]byte(r.Messages[1].Content), &a.plan); err != nil {
			return providers.Completion{}, err
		}
	case "submit_file_selection":
		a.selectionBytes = len(r.Messages[1].Content)
		if err := json.Unmarshal([]byte(r.Messages[1].Content), &a.selection); err != nil {
			return providers.Completion{}, err
		}
	}
	return a.base.StreamChat(ctx, p, key, r, emit)
}

func TestGoalRanksLateWritableFilesWithinActualProviderPayloadAndCompletes(t *testing.T) {
	c, ts, task, input, root, db := newGoalFixtureWithStore(t, &goalAdapter{}, taskengine.GoalContract{})
	recorder := &manifestRecordingAdapter{}
	c.providers = providers.NewService(db, recorder)
	// A 32768-token writer forces candidate fitting below the 128 KiB wire cap.
	profile, err := c.providers.Get(context.Background(), input.ProviderID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.providers.Save(context.Background(), providers.SaveInput{ID: profile.ID, Name: profile.Name, BaseURL: profile.BaseURL, Model: profile.Model, ContextWindow: 32768, MaxOutputTokens: 8192})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2200; i++ {
		name := "a_" + strings.Repeat("f", 90) + fmt.Sprintf("_%04d.go", i)
		if err = os.WriteFile(filepath.Join(root, name), []byte("// unrelated source\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalCompleted {
		t.Fatalf("large-path goal=%+v", g)
	}
	if len(recorder.plan.RepositoryFiles) == 0 || recorder.plan.RepositoryFiles[0].Path != "sum.js" || !recorder.plan.RepositoryFilesTruncated || recorder.plan.RepositoryDiscoveryMode != "ranked_paths" || recorder.planBytes > 48<<10 {
		t.Fatalf("planner input size=%d files=%d truncated=%v", recorder.planBytes, len(recorder.plan.RepositoryFiles), recorder.plan.RepositoryFilesTruncated)
	}
	if len(recorder.selection.Candidates) == 0 || recorder.selection.Candidates[0].Path != "sum.js" || !recorder.selection.CandidatesTruncated || recorder.selection.RepositoryDiscoveryMode != "ranked_paths" || recorder.selectionBytes > 120<<10 {
		t.Fatalf("selector input size=%d files=%d truncated=%v", recorder.selectionBytes, len(recorder.selection.Candidates), recorder.selection.CandidatesTruncated)
	}
	sawTest := false
	for _, file := range recorder.selection.Candidates {
		if file.Path == "test.js" {
			sawTest = true
		}
	}
	if !sawTest || recorder.base.proposals != 1 || recorder.base.reviews != 1 {
		t.Fatal("test context or real coding lifecycle was lost")
	}
	actions, err := ts.GoalActions(context.Background(), g.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, action := range actions {
		if action.Action != "plan" {
			continue
		}
		id, _ := action.Receipt["artifact_id"].(string)
		artifact, _, err := c.products.GetArtifact(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		discovery, ok := artifact.Metadata["repository_context"].(map[string]any)
		if !ok || discovery["truncated"] != true {
			t.Fatalf("missing ranked discovery receipt: %+v", artifact.Metadata)
		}
		found = true
	}
	if !found {
		t.Fatal("planner discovery receipt missing")
	}
	actual, err := ts.Get(context.Background(), task.ID)
	if err != nil || actual.State != taskengine.StateCompleted {
		t.Fatalf("task=%+v err=%v", actual, err)
	}
	t.Logf("actual goal completed: planner %d bytes/%d files; selector %d bytes/%d candidates; writer context 32768", recorder.planBytes, len(recorder.plan.RepositoryFiles), recorder.selectionBytes, len(recorder.selection.Candidates))
}

func TestRepositoryContextRetainsAllPinsOrRefusesBeforeProvider(t *testing.T) {
	c, _, task, _, _, _ := newGoalFixtureWithStore(t, &goalAdapter{}, taskengine.GoalContract{})
	plan := worker.PlanTask{TaskID: task.ID, Objective: task.Objective, OriginalRequest: task.OriginalRequest, Criteria: []string{"AC-1"}, WritablePaths: []string{"sum.js"}}
	if _, err := c.plannerRepositoryContext(context.Background(), task, &plan, 2048); err == nil {
		t.Fatal("planner silently discarded its explicitly writable path")
	}
	selection := worker.FileSelectionTask{TaskID: task.ID, Objective: task.Objective, StepTitle: "fix", StepInstructions: "fix sum", AcceptanceCriteria: []string{"AC-1"}}
	if _, err := c.selectionRepositoryContext(context.Background(), task.ProjectID, []string{"sum.js"}, []string{"node test.js"}, &selection, 2048); err == nil {
		t.Fatal("selector silently discarded its explicitly writable path")
	}
}

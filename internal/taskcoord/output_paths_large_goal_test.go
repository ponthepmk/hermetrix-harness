package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

type largeGoalAdapter struct {
	base           *goalAdapter
	capturedScopes [][]string
	capturedFiles  []map[string]string
}

func (a *largeGoalAdapter) StreamChat(ctx context.Context, p providers.Profile, url string, r providers.ChatRequest, delta func(providers.Delta) error) (providers.Completion, error) {
	name := r.Tools[0].Function.Name
	switch name {
	case "submit_plan":
		a.base.plans++
		first := make([]string, 0, 17)
		first = append(first, "sum.js")
		for i := 0; i < 16; i++ {
			first = append(first, fmt.Sprintf("output%02d.txt", i))
		}
		second := make([]string, 0, 16)
		for i := 16; i < 32; i++ {
			second = append(second, fmt.Sprintf("output%02d.txt", i))
		}
		value := map[string]any{
			"reason": "create 33 outputs in two bounded dependency-ordered steps",
			"steps": []any{
				map[string]any{"key": "first", "title": "First batch", "workspace_change": "Write sum.js and output00 through output15", "instructions": "Create sum.js and first 16 output files", "requirement_ids": []string{"AC-1"}, "dependencies": []string{}, "checks": []string{"node test.js"}, "effect_scope": []string{"provider.select_files", "provider.propose", "workspace.apply", "workspace.run", "provider.review"}, "output_paths": first},
				map[string]any{"key": "second", "title": "Second batch", "workspace_change": "Write output16 through output31", "instructions": "Create remaining 16 output files", "requirement_ids": []string{"AC-1"}, "dependencies": []string{"first"}, "checks": []string{"node test.js"}, "effect_scope": []string{"provider.select_files", "provider.propose", "workspace.apply", "workspace.run", "provider.review"}, "output_paths": second},
			},
		}
		body, err := json.Marshal(value)
		if err != nil {
			return providers.Completion{}, err
		}
		return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: name, Arguments: string(body)}}}, nil
	case "submit_file_selection":
		body, err := json.Marshal(map[string]any{"files": []string{"test.js"}, "rationale": "immutable acceptance test only"})
		if err != nil {
			return providers.Completion{}, err
		}
		return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: name, Arguments: string(body)}}}, nil
	case "submit_changes":
		a.base.proposals++
		var task worker.Task
		if err := json.Unmarshal([]byte(r.Messages[1].Content), &task); err != nil {
			return providers.Completion{}, err
		}
		a.capturedScopes = append(a.capturedScopes, task.WritablePaths)
		files := make(map[string]string, len(task.Files))
		for k, v := range task.Files {
			files[k] = v
		}
		a.capturedFiles = append(a.capturedFiles, files)
		changes := make([]any, 0, len(task.WritablePaths))
		for _, path := range task.WritablePaths {
			content := "created:" + path + "\n"
			if path == "sum.js" {
				content = "module.exports=(a,b)=>a+b;\n"
			}
			changes = append(changes, map[string]any{"path": path, "content": content})
		}
		body, err := json.Marshal(map[string]any{"summary": "bounded multi-step output", "recommended_checks": []string{"node test.js"}, "changes": changes})
		if err != nil {
			return providers.Completion{}, err
		}
		return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: name, Arguments: string(body)}}}, nil
	case "submit_review":
		return a.base.StreamChat(ctx, p, url, r, delta)
	}
	return providers.Completion{}, fmt.Errorf("unexpected tool %s", name)
}

func TestLargeGoalOutputPathsEndToEnd(t *testing.T) {
	paths := make([]string, 0, 33)
	paths = append(paths, "sum.js")
	for i := 0; i < 32; i++ {
		paths = append(paths, fmt.Sprintf("output%02d.txt", i))
	}
	contract := taskengine.GoalContract{WritablePaths: paths}
	base := &goalAdapter{}
	adapter := &largeGoalAdapter{base: base}
	c, ts, task, input, root, db := newGoalFixtureWithStore(t, base, contract)
	c.providers = providers.NewService(db, adapter)
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalCompleted {
		t.Fatalf("state=%s reason=%s", g.State, g.Reason)
	}
	if base.plans != 1 {
		t.Fatalf("plans=%d want 1", base.plans)
	}
	if base.proposals != 2 {
		t.Fatalf("proposals=%d want 2", base.proposals)
	}
	if base.reviews != 2 {
		t.Fatalf("reviews=%d want 2", base.reviews)
	}
	if len(adapter.capturedScopes) != 2 {
		t.Fatalf("capturedScopes=%d want 2", len(adapter.capturedScopes))
	}
	wantFirst := make([]string, 0, 17)
	wantFirst = append(wantFirst, "sum.js")
	for i := 0; i < 16; i++ {
		wantFirst = append(wantFirst, fmt.Sprintf("output%02d.txt", i))
	}
	wantSecond := make([]string, 0, 16)
	for i := 16; i < 32; i++ {
		wantSecond = append(wantSecond, fmt.Sprintf("output%02d.txt", i))
	}
	assertScope(t, adapter.capturedScopes[0], wantFirst)
	assertScope(t, adapter.capturedScopes[1], wantSecond)
	for i, files := range adapter.capturedFiles {
		if len(files) > 32 {
			t.Fatalf("call %d Files has %d entries, want <=32", i, len(files))
		}
		if _, ok := files["test.js"]; !ok {
			t.Fatalf("call %d Files missing test.js readonly context", i)
		}
	}
	wantSum := "module.exports=(a,b)=>a+b;\n"
	data, err := os.ReadFile(filepath.Join(root, "sum.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != wantSum {
		t.Fatalf("sum.js=%q want %q", string(data), wantSum)
	}
	for i := 0; i < 32; i++ {
		p := fmt.Sprintf("output%02d.txt", i)
		want := "created:" + p + "\n"
		data, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if string(data) != want {
			t.Fatalf("%s=%q want %q", p, string(data), want)
		}
	}
	wantTest := "const assert=require('node:assert/strict');assert.equal(require('./sum')(2,3),5);assert.equal(require('./sum')(-4,7),3);console.log('acceptance passed');\n"
	data, err = os.ReadFile(filepath.Join(root, "test.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != wantTest {
		t.Fatal("test.js was modified")
	}
	task2, err := ts.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(task2.Plan.Steps) != 2 {
		t.Fatalf("plan steps=%d want 2", len(task2.Plan.Steps))
	}
	assertScope(t, task2.Plan.Steps[0].OutputPaths, wantFirst)
	assertScope(t, task2.Plan.Steps[1].OutputPaths, wantSecond)
}

func assertScope(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("scope len=%d want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scope[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

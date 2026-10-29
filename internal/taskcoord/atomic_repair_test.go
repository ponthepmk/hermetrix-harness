package taskcoord

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

// Only inference is a fixture. Actual Node requires both restored modules;
// SQLite, failed verification, rollback, guarded apply and review are real.
type atomicCompanionAdapter struct {
	base *goalAdapter
	used bool
}

func (a *atomicCompanionAdapter) StreamChat(ctx context.Context, p providers.Profile, key string, r providers.ChatRequest, emit func(providers.Delta) error) (providers.Completion, error) {
	name := r.Tools[0].Function.Name
	var value any
	switch name {
	case "submit_file_selection":
		value = map[string]any{"files": []string{"sum.js", "helper.js", "test.js"}, "rationale": "Both required modules and readonly behavioral check"}
	case "submit_changes":
		var input struct {
			worker.Task
			Targets map[string][]worker.RepairTarget `json:"repair_targets"`
		}
		if err := json.Unmarshal([]byte(r.Messages[1].Content), &input); err != nil {
			return providers.Completion{}, err
		}
		if len(input.RepairDrafts) == 0 {
			value = map[string]any{"changes": []any{
				map[string]any{"path": "sum.js", "content": "module.exports=(a,b)=>a*b;\n"},
				map[string]any{"path": "helper.js", "content": "module.exports={bias:0};\n"},
			}}
		} else {
			a.used = true
			for _, target := range input.Targets["sum.js"] {
				if target.Kind == "text_line" {
					value = map[string]any{"repair": map[string]any{"path": "sum.js", "target_id": target.ID, "new": "module.exports=(a,b)=>a+b;\n"}}
					break
				}
			}
		}
	default:
		return a.base.StreamChat(ctx, p, key, r, emit)
	}
	b, err := json.Marshal(value)
	return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: name, Arguments: string(b)}}}, err
}

func TestGoalAtomicRepairRestoresRequiredCompanionThroughRealChecks(t *testing.T) {
	ctx := context.Background()
	base := &goalAdapter{}
	c, ts, task, in, root, db := newGoalFixtureWithStore(t, base, taskengine.GoalContract{WritablePaths: []string{"sum.js", "helper.js"}})
	if err := os.Remove(filepath.Join(root, "sum.js")); err != nil {
		t.Fatal(err)
	}
	check := "const assert=require('node:assert/strict');\nconst add=require('./sum');const {bias}=require('./helper');\nassert.equal(add(2,3)+bias,5);assert.equal(add(-2,3)+bias,1);\n"
	if err := os.WriteFile(filepath.Join(root, "test.js"), []byte(check), 0600); err != nil {
		t.Fatal(err)
	}
	a := &atomicCompanionAdapter{base: base}
	c.providers = providers.NewService(db, a)
	g, err := c.StartGoal(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalCompleted || g.AttemptsUsed != 2 || !a.used || base.reviews != 1 {
		t.Fatalf("state=%s attempts=%d atomic=%t reviews=%d", g.State, g.AttemptsUsed, a.used, base.reviews)
	}
	x, err := ts.Execution(ctx, task.ID)
	if err != nil || x.Proposal == nil || x.Proposal.State != taskengine.ProposalVerified {
		t.Fatal("missing verified result", err)
	}
	loaded, err := c.loadProposalBundle(ctx, *x.Proposal)
	if err != nil || loaded.InputTask.RepairTargetVersion != worker.AtomicRepairTargetVersion || len(loaded.Bundle.Result.Changes) != 2 {
		t.Fatal("root repair did not preserve derivable two-file proposal", err)
	}
	for _, change := range loaded.Bundle.Result.Changes {
		if change.BeforeSHA256 != "absent" {
			t.Fatal("historical source replaced rolled-back absent preimage")
		}
		if change.Path == "helper.js" && (!change.Repair.Retain || change.Content != "module.exports={bias:0};\n") {
			t.Fatal("companion regenerated")
		}
	}
	actual, err := os.ReadFile(filepath.Join(root, "helper.js"))
	if err != nil || string(actual) != "module.exports={bias:0};\n" {
		t.Fatal("required actual companion not restored", err)
	}
	actual, err = os.ReadFile(filepath.Join(root, "test.js"))
	if err != nil || string(actual) != check {
		t.Fatal("readonly behavioral test changed", err)
	}
}

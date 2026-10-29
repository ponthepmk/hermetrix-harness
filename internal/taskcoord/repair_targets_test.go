package taskcoord

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

type namedGoalAdapter struct {
	base  *goalAdapter
	named bool
}

func (a *namedGoalAdapter) StreamChat(ctx context.Context, p providers.Profile, key string, r providers.ChatRequest, emit func(providers.Delta) error) (providers.Completion, error) {
	if r.Tools[0].Function.Name == "submit_changes" {
		var input struct {
			worker.Task
			Targets map[string][]worker.RepairTarget `json:"repair_targets"`
		}
		if err := json.Unmarshal([]byte(r.Messages[1].Content), &input); err != nil {
			return providers.Completion{}, err
		}
		if input.RepairTargetVersion == worker.RepairTargetVersion {
			for _, target := range input.Targets["sum.js"] {
				if target.Kind == "text_line" {
					a.named = true
					a.base.proposals++
					body, _ := json.Marshal(map[string]any{"changes": []any{map[string]any{"path": "sum.js", "target_edits": []worker.DraftTargetEdit{{TargetID: target.ID, New: "module.exports=(a,b)=>a+b;\n"}}}}, "recommended_checks": []string{"node test.js"}})
					return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_changes", Arguments: string(body)}}}, nil
				}
			}
		}
	}
	return a.base.StreamChat(ctx, p, key, r, emit)
}

// Model transport is a fixture; SQLite, real Node checks, apply, rollback,
// source/target/proposal bindings, post-review and reopen are actual.
func TestGoalNamedRepairTargetPersistsAndReopens(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "absent"}[missing], func(t *testing.T) {
			ctx := context.Background()
			base := &goalAdapter{failures: 1}
			c, ts, task, input, root, db := newGoalFixtureWithStore(t, base, taskengine.GoalContract{})
			adapter := &namedGoalAdapter{base: base}
			c.providers = providers.NewService(db, adapter)
			if missing {
				if err := os.Remove(filepath.Join(root, "sum.js")); err != nil {
					t.Fatal(err)
				}
			}
			g, err := c.StartGoal(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			g = advanceGoalUntilIdle(t, c, ts, g)
			if g.State != taskengine.GoalCompleted || !adapter.named || g.AttemptsUsed != 2 {
				t.Fatalf("state=%s named=%t attempts=%d", g.State, adapter.named, g.AttemptsUsed)
			}
			x, err := ts.Execution(ctx, task.ID)
			if err != nil || x.Proposal == nil || x.Proposal.State != taskengine.ProposalVerified {
				t.Fatal("verified proposal missing", err)
			}
			p := *x.Proposal
			loaded, err := c.loadProposalBundle(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.InputTask.RepairTargetVersion != worker.RepairTargetVersion || len(loaded.Bundle.Result.Changes[0].Repair.TargetEdits) != 1 {
				t.Fatal("recipe/proof not persisted")
			}
			want := worker.Hash("module.exports=(a,b)=>a-b;\n")
			if missing {
				want = "absent"
			}
			if loaded.Bundle.Result.Changes[0].BeforeSHA256 != want {
				t.Fatal("historical span replaced real preimage")
			}
			var sequence int
			var name, filename string
			if err = db.DB.QueryRowContext(ctx, "PRAGMA database_list").Scan(&sequence, &name, &filename); err != nil {
				t.Fatal(err)
			}
			c.products.Close()
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := store.Open(ctx, filepath.Dir(filename))
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			products := product.NewService(reopened, skills.NewService(reopened))
			defer products.Close()
			restored := New(taskengine.NewService(reopened), products, providers.NewService(reopened, base))
			p, err = restored.tasks.GetCodeProposal(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err = restored.loadProposalBundle(ctx, p)
			if err != nil || loaded.InputTask.RepairTargetVersion != worker.RepairTargetVersion || len(loaded.Bundle.Result.Changes[0].Repair.TargetEdits) != 1 {
				t.Fatal("named proof could not be rederived after reopen", err)
			}
		})
	}
}

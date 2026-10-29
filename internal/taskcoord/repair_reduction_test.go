package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

// Model transport is an explicit fixture; Node, SQLite, source writes,
// mandatory checks, failure/rollback, review gates and reopen are actual.
type reductionAdapter struct {
	base      *goalAdapter
	enabled   bool
	proposals int
	reversed  bool
}

func (a *reductionAdapter) StreamChat(ctx context.Context, p providers.Profile, key string, r providers.ChatRequest, emit func(providers.Delta) error) (providers.Completion, error) {
	name := r.Tools[0].Function.Name
	if name == "submit_plan" {
		c, e := a.base.StreamChat(ctx, p, key, r, emit)
		if e != nil {
			return c, e
		}
		if a.enabled {
			var body map[string]any
			if e = json.Unmarshal([]byte(c.ToolCalls[0].Arguments), &body); e != nil {
				return c, e
			}
			for _, v := range body["steps"].([]any) {
				step := v.(map[string]any)
				step["effect_scope"] = append(step["effect_scope"].([]any), reductionEffect)
				if a.base.plans > 1 {
					step["key"] = step["key"].(string) + "-replanned"
				}
			}
			b, _ := json.Marshal(body)
			c.ToolCalls[0].Arguments = string(b)
		}
		return c, nil
	}
	if name != "submit_changes" {
		return a.base.StreamChat(ctx, p, key, r, emit)
	}
	a.proposals++
	var input struct {
		worker.Task
		Targets map[string][]worker.RepairTarget `json:"repair_targets"`
	}
	if e := json.Unmarshal([]byte(r.Messages[1].Content), &input); e != nil {
		return providers.Completion{}, e
	}
	change := map[string]any{"path": "sum.js"}
	if a.proposals == 1 {
		change["content"] = "const bias=0;\nmodule.exports=(a,b)=>a*b+bias;\n"
	} else if a.proposals == 2 {
		ids := map[string]string{}
		for _, t := range input.Targets["sum.js"] {
			ids[t.Symbol] = t.ID
		}
		if ids["line 1"] == "" || ids["line 2"] == "" {
			return providers.Completion{}, fmt.Errorf("expected exact historical line targets")
		}
		change["target_edits"] = []worker.DraftTargetEdit{{TargetID: ids["line 2"], New: "module.exports=(a,b)=>a+b+bias;\n"}, {TargetID: ids["line 1"], New: "const bias=1;\n"}}
		if a.reversed {
			edits := change["target_edits"].([]worker.DraftTargetEdit)
			edits[0], edits[1] = edits[1], edits[0]
		}
	} else {
		if a.enabled {
			return providers.Completion{}, fmt.Errorf("unexpected third writer call instead of authorized host reduction")
		}
		change["edits"] = []worker.TextEdit{{Old: input.RepairDrafts["sum.js"].Content, New: "const bias=0;\nmodule.exports=(a,b)=>a+b+bias;\n"}}
	}
	b, _ := json.Marshal(map[string]any{"summary": "explicit model transport fixture", "changes": []any{change}})
	return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_changes", Arguments: string(b)}}}, nil
}

func TestGoalReducesCompleteNativeEditsWithOrdinaryGates(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		missing, enabled, rejectReview bool
		attempts                       int
		reversed                       bool
	}{{"existing", false, true, false, 3, false}, {"absent", true, true, false, 3, false}, {"old_plan_without_reduction", false, false, false, 3, false}, {"attempt_budget", false, true, false, 2, false}, {"post_review_rejection", false, true, true, 3, false}, {"failed_subset_advances_without_replay", false, true, false, 4, true}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			base := &goalAdapter{}
			if tc.rejectReview {
				base.rejectReviews = 1
			}
			c, ts, task, input, root, db := newGoalFixtureWithStore(t, base, taskengine.GoalContract{})
			a := &reductionAdapter{base: base, enabled: tc.enabled, reversed: tc.reversed}
			c.providers = providers.NewService(db, a)
			if tc.missing {
				if e := os.Remove(filepath.Join(root, "sum.js")); e != nil {
					t.Fatal(e)
				}
			}
			readonly, e := os.ReadFile(filepath.Join(root, "test.js"))
			if e != nil {
				t.Fatal(e)
			}
			input.MaxAttempts = tc.attempts
			input.MaxPlans = 2
			if tc.reversed {
				input.MaxPlans = 3
			}
			g, e := c.StartGoal(ctx, input)
			if e != nil {
				t.Fatal(e)
			}
			deadline := time.Now().Add(120 * time.Second)
			for g.State == taskengine.GoalActive && time.Now().Before(deadline) {
				_, ae := c.AdvanceGoal(ctx, g.ID)
				g, e = ts.Goal(ctx, g.ID)
				if e != nil {
					t.Fatal(e)
				}
				if ae != nil && g.State != taskengine.GoalActive && g.State != taskengine.GoalBudgetExhausted {
					t.Fatalf("goal=%s reason=%s error=%v", g.State, g.Reason, ae)
				}
			}
			if tc.attempts == 2 || tc.rejectReview {
				if g.State != taskengine.GoalBudgetExhausted || g.AttemptsUsed != tc.attempts || a.proposals != 2 {
					t.Fatal("reduction bypassed attempt or review gate", g)
				}
				if tc.rejectReview {
					if base.reviews != 1 {
						t.Fatal("review rejection gate was never reached")
					}
					actual, e := os.ReadFile(filepath.Join(root, "sum.js"))
					if e != nil || string(actual) != "module.exports=(a,b)=>a-b;\n" {
						t.Fatal("rejected host reduction was not rolled back", e)
					}
				}
				return
			}
			if g.State != taskengine.GoalCompleted || g.AttemptsUsed != tc.attempts {
				t.Fatalf("goal=%s reason=%s attempts=%d writercalls=%d", g.State, g.Reason, g.AttemptsUsed, a.proposals)
			}
			x, e := ts.Execution(ctx, task.ID)
			if e != nil || x.Proposal == nil || x.Proposal.State != taskengine.ProposalVerified {
				t.Fatal("missing verified result", e)
			}
			loaded, e := c.loadProposalBundle(ctx, *x.Proposal)
			if e != nil {
				t.Fatal(e)
			}
			if !tc.enabled {
				if loaded.Bundle.Reduction != nil || a.proposals != 3 {
					t.Fatal("unapproved effect used")
				}
				return
			}
			if a.proposals != 2 || loaded.Bundle.Reduction == nil || loaded.Bundle.Result.Usage.TotalTokens != 0 {
				t.Fatal("host derivation fabricated a model call or lacks proof")
			}
			if x.Attempt.PresetRevision != 0 {
				t.Fatal("host attempt falsely pinned an inference preset")
			}
			host := 0
			for _, effect := range x.Effects {
				if effect.Action == proposalEffect || effect.Action == selectionEffect {
					t.Fatal("host attempt fabricated provider effect")
				}
				if effect.Action == reductionEffect {
					host++
					if effect.State != taskengine.EffectObserved || effect.Receipt["provider_called"] != false {
						t.Fatal("host receipt invalid")
					}
				}
			}
			if host != 1 {
				t.Fatal("missing host effect")
			}
			actual, e := os.ReadFile(filepath.Join(root, "sum.js"))
			if e != nil || string(actual) != "const bias=0;\nmodule.exports=(a,b)=>a+b+bias;\n" {
				t.Fatal("wrong actual reduction", e)
			}
			after, e := os.ReadFile(filepath.Join(root, "test.js"))
			if e != nil || string(after) != string(readonly) {
				t.Fatal("mandatory test changed")
			}
			originalProof := loaded.Bundle.Reduction
			bad := *originalProof
			bad.SourceProposalID = x.Proposal.ID
			loaded.Bundle.Reduction = &bad
			if e = c.validateReduction(ctx, *x.Proposal, loaded); e == nil {
				t.Fatal("self-referential reduction accepted")
			}
			loaded.Bundle.Reduction = originalProof
			parent, e := ts.GetCodeProposal(ctx, originalProof.SourceProposalID)
			if e != nil {
				t.Fatal(e)
			}
			source, e := c.loadProposalBundle(ctx, parent)
			if e != nil {
				t.Fatal(e)
			}
			choices, e := worker.RepairSelections(*source.InputTask, source.Bundle.Result)
			if e != nil {
				t.Fatal(e)
			}
			bad = *originalProof
			bad.Selection = choices[0]
			if bad.Selection == originalProof.Selection {
				bad.Selection = choices[1]
			}
			altered := loaded.Bundle
			altered.Reduction = &bad
			body, _ := json.Marshal(altered)
			artifact, e := c.products.CreateArtifact(ctx, product.ArtifactInput{ProjectID: task.ProjectID, Name: "resigned-reduction.proposal.json", Kind: "code_proposal", MIMEType: "application/json", Content: string(body), Metadata: loaded.Artifact.Metadata})
			if e != nil {
				t.Fatal(e)
			}
			forged := *x.Proposal
			forged.ArtifactID = artifact.ID
			forged.ResultHash = artifact.Checksum
			if _, e = c.loadProposalBundle(ctx, forged); e == nil {
				t.Fatal("re-signed reduction ancestry selected a different native edit")
			}
			used, e := ts.UsedRepairReductionTargets(ctx, task.ID, originalProof.SourceProposalID)
			wantUsed := 1
			if tc.reversed {
				wantUsed = 2
			}
			if e != nil || len(used) != wantUsed {
				t.Fatal("durable reduction history missing", e)
			}
			var seq int
			var name, filename string
			if e = db.DB.QueryRowContext(ctx, "PRAGMA database_list").Scan(&seq, &name, &filename); e != nil {
				t.Fatal(e)
			}
			proposal := *x.Proposal
			c.products.Close()
			if e = db.Close(); e != nil {
				t.Fatal(e)
			}
			reopened, e := store.Open(ctx, filepath.Dir(filename))
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.Close()
			products := product.NewService(reopened, skills.NewService(reopened))
			defer products.Close()
			restored := New(taskengine.NewService(reopened), products, providers.NewService(reopened, base))
			if loaded, e = restored.loadProposalBundle(ctx, proposal); e != nil || loaded.Bundle.Reduction == nil || !strings.Contains(loaded.Bundle.Result.Summary, "Host single-edit reduction") {
				t.Fatal("reduction cannot rederive after reopen", e)
			}
		})
	}
}

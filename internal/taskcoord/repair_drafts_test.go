package taskcoord

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

type repairCaptureAdapter struct {
	base   *goalAdapter
	inputs []worker.Task
}

type retainFailedDraftAdapter struct {
	base     *goalAdapter
	retained bool
}

func (a *retainFailedDraftAdapter) StreamChat(ctx context.Context, p providers.Profile, key string, r providers.ChatRequest, emit func(providers.Delta) error) (providers.Completion, error) {
	if r.Tools[0].Function.Name == "submit_changes" && !a.retained {
		var input worker.Task
		if err := json.Unmarshal([]byte(r.Messages[1].Content), &input); err != nil {
			return providers.Completion{}, err
		}
		if len(input.RepairDrafts) > 0 {
			a.retained = true
			return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_changes", Arguments: `{"changes":[{"path":"sum.js","retain_draft":true}]}`}}}, nil
		}
	}
	return a.base.StreamChat(ctx, p, key, r, emit)
}

func TestGoalRetainedFailedDraftCannotApplyWithoutRepair(t *testing.T) {
	ctx := context.Background()
	base := &goalAdapter{failures: 1}
	c, ts, task, input, _, db := newGoalFixtureWithStore(t, base, taskengine.GoalContract{})
	a := &retainFailedDraftAdapter{base: base}
	c.providers = providers.NewService(db, a)
	g, err := c.StartGoal(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalCompleted || g.AttemptsUsed != 3 || !a.retained {
		t.Fatalf("state=%s attempts=%d retained=%t", g.State, g.AttemptsUsed, a.retained)
	}
	var rejected, applied int
	if err := db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_effect_intents WHERE task_id=? AND action='provider.propose' AND json_extract(receipt_json,'$.failure_code')='invalid_proposal'`, task.ID).Scan(&rejected); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_effect_intents WHERE task_id=? AND action='workspace.apply'`, task.ID).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if rejected != 1 || applied != 2 {
		t.Fatalf("rejected=%d applied=%d; retained draft must not apply", rejected, applied)
	}
}

func (a *repairCaptureAdapter) StreamChat(ctx context.Context, p providers.Profile, key string, r providers.ChatRequest, emit func(providers.Delta) error) (providers.Completion, error) {
	if r.Tools[0].Function.Name == "submit_changes" {
		var input worker.Task
		if err := json.Unmarshal([]byte(r.Messages[1].Content), &input); err != nil {
			return providers.Completion{}, err
		}
		a.inputs = append(a.inputs, input)
	}
	return a.base.StreamChat(ctx, p, key, r, emit)
}

// Only model transport is a fixture. Commands, rollback, artifact checksums,
// canonical inputs, approval/apply/review gates and SQLite reopen are real.
func TestGoalRepairDraftPersistsActualPreimagesAndProof(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "absent"}[missing], func(t *testing.T) {
			ctx := context.Background()
			base := &goalAdapter{failures: 1}
			c, ts, task, input, root, db := newGoalFixtureWithStore(t, base, taskengine.GoalContract{})
			capture := &repairCaptureAdapter{base: base}
			c.providers = providers.NewService(db, capture)
			before, err := os.ReadFile(filepath.Join(root, "sum.js"))
			if err != nil {
				t.Fatal(err)
			}
			if missing {
				if err = os.Remove(filepath.Join(root, "sum.js")); err != nil {
					t.Fatal(err)
				}
			}
			goal, err := c.StartGoal(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			goal = advanceGoalUntilIdle(t, c, ts, goal)
			if goal.State != taskengine.GoalCompleted || len(capture.inputs) != 2 || goal.AttemptsUsed != 2 {
				t.Fatalf("goal=%s inputs=%d attempts=%d", goal.State, len(capture.inputs), goal.AttemptsUsed)
			}
			second := capture.inputs[1]
			draft, ok := second.RepairDrafts["sum.js"]
			if !ok || draft.Content != "module.exports=(a,b)=>a*b;\n" {
				t.Fatal("verified failed source not supplied as a separate draft")
			}
			wantBefore := string(before)
			if missing {
				wantBefore = ""
			}
			if current, ok := second.Files["sum.js"]; !ok || current != wantBefore || contains(second.AbsentFiles, "sum.js") != missing {
				t.Fatal("rollback preimage was replaced with historical source")
			}
			rows, err := db.DB.QueryContext(ctx, "SELECT id FROM task_code_proposals WHERE task_id=?", task.ID)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, id)
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			var failed, passed taskengine.CodeProposal
			for _, id := range ids {
				p, e := ts.GetCodeProposal(ctx, id)
				if e != nil {
					t.Fatal(e)
				}
				if p.State == taskengine.ProposalVerifyFailed {
					failed = p
				}
				if p.State == taskengine.ProposalVerified {
					passed = p
				}
			}
			if len(ids) != 2 || failed.ID == "" || passed.ID == "" {
				t.Fatal("expected one failed and one verified proposal")
			}
			if draft.ProposalID != failed.ID || draft.SourceArtifactID != failed.ArtifactID || draft.SourceArtifactSHA256 != failed.ResultHash {
				t.Fatal("draft origin mismatch")
			}
			loaded, err := c.loadProposalBundle(ctx, passed)
			if err != nil {
				t.Fatal(err)
			}
			change := loaded.Bundle.Result.Changes[0]
			wantSHA := worker.Hash(string(before))
			if missing {
				wantSHA = "absent"
			}
			if change.BeforeSHA256 != wantSHA || change.Repair == nil || change.Repair.SourceSHA256 != worker.Hash(draft.Content) {
				t.Fatal("actual preimage/proof not persisted")
			}
			attempt, err := ts.Attempt(ctx, passed.AttemptID)
			if err != nil || attempt.Packet == nil {
				t.Fatal("origin packet missing", err)
			}
			baseTask := *loaded.InputTask
			baseTask.RepairDrafts = nil
			preset := inference.Preset{ContextMax: 65536}
			rootID, err := c.products.ProjectRootIdentity(ctx, task.ProjectID)
			if err != nil {
				t.Fatal(err)
			}
			for _, scenario := range []string{"changed-preimage", "readonly", "old-requirement", "moved-root", "too-small-context", "bad-log-checksum"} {
				trial := baseTask
				trial.Files = map[string]string{}
				for p, s := range baseTask.Files {
					trial.Files[p] = s
				}
				packet := *attempt.Packet
				candidateRoot := rootID
				budget := preset
				trial.FailureEvidence = append([]worker.FailureEvidence(nil), baseTask.FailureEvidence...)
				switch scenario {
				case "changed-preimage":
					trial.Files["sum.js"] = "external change"
					trial.AbsentFiles = nil
				case "readonly":
					trial.WritablePaths = nil
				case "old-requirement":
					packet.Requirement.ID = "different-requirement"
				case "moved-root":
					candidateRoot = "different-root"
				case "too-small-context":
					budget.ContextMax = 1
				case "bad-log-checksum":
					trial.FailureEvidence[0].ArtifactSHA256 = worker.Hash("wrong")
				}
				got, e := c.attachRepairDrafts(ctx, packet, trial, budget, candidateRoot)
				if scenario == "bad-log-checksum" {
					if e == nil {
						t.Fatal("accepted false log binding")
					}
					continue
				}
				if e != nil || len(got.RepairDrafts) != 0 {
					t.Fatalf("%s: draft accepted or unexpected error: %v", scenario, e)
				}
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
			p, err := restored.tasks.GetCodeProposal(ctx, passed.ID)
			if err != nil {
				t.Fatal(err)
			}
			reloaded, err := restored.loadProposalBundle(ctx, p)
			if err != nil || reloaded.Bundle.Result.Changes[0].Repair == nil || reloaded.InputTask.RepairDrafts["sum.js"].ProposalID != failed.ID {
				t.Fatal("repair provenance lost across reopen", err)
			}
		})
	}
}

func TestLegacyProposalCannotCarryRepairProof(t *testing.T) {
	value := worker.Result{Status: "proposed_unverified", ProposalID: worker.Hash("proposal"), Changes: []worker.Change{{Path: "a.go", Content: "new", Strategy: "replace_file", Repair: &worker.DraftEditProof{SourceSHA256: worker.Hash("draft"), Retain: true}}}}
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeProposalBundle(body); err == nil {
		t.Fatal("unbound legacy repair proof accepted")
	}
}

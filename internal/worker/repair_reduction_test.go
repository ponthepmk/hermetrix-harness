package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
)

func reductionFixture(t *testing.T) (Task, Result) {
	t.Helper()
	task := targetFixture(t)
	read, write := findTarget(t, task, "NewServer.ReadTimeout"), findTarget(t, task, "NewServer.WriteTimeout")
	body, _ := json.Marshal(map[string]any{"changes": []any{map[string]any{"path": "api/main.go", "target_edits": []DraftTargetEdit{{TargetID: write.ID, New: "10 * unit"}, {TargetID: read.ID, New: "99 * unit"}}}}})
	full, err := Run(context.Background(), &fake{arguments: string(body)}, providers.Profile{}, "", task)
	if err != nil {
		t.Fatal(err)
	}
	return task, full
}

func TestReductionRestoresMissingHistoricalCompanion(t *testing.T) {
	task, _ := reductionFixture(t)
	path := "api/companion.txt"
	task.Files[path] = ""
	task.WritablePaths = append(task.WritablePaths, path)
	task.AbsentFiles = append(task.AbsentFiles, path)
	draft := task.RepairDrafts["api/main.go"]
	draft.Content = "stable companion\n"
	draft.SourceSHA256 = Hash(draft.Content)
	task.RepairDrafts[path] = draft
	read, write, companion := findTarget(t, task, "NewServer.ReadTimeout"), findTarget(t, task, "NewServer.WriteTimeout"), RepairTarget{}
	catalog, _ := buildRepairTargets(task)
	companion = catalog[path][0]
	args, _ := json.Marshal(map[string]any{"changes": []any{
		map[string]any{"path": "api/main.go", "target_edits": []DraftTargetEdit{{TargetID: write.ID, New: "10 * unit"}, {TargetID: read.ID, New: "99 * unit"}}},
		map[string]any{"path": path, "target_edits": []DraftTargetEdit{{TargetID: companion.ID, New: "broken companion\n"}}},
	}})
	full, err := Run(context.Background(), &fake{arguments: string(args)}, providers.Profile{}, "", task)
	if err != nil {
		t.Fatal(err)
	}
	_, result, err := ReduceRepairResult(task, full, "next", RepairSelection{Path: "api/main.go", TargetID: write.ID})
	if err != nil || len(result.Changes) != 2 || result.Changes[1].Content != draft.Content || !result.Changes[1].Repair.Retain || result.Changes[1].BeforeSHA256 != "absent" {
		t.Fatal("reduction dropped or regenerated missing historical companion", err)
	}
}

func TestPlannerAllowsExplicitOptionalReductionWithoutDroppingRequiredEffects(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	body := `{"reason":"bounded implementation","steps":[{"key":"fix","title":"Fix","workspace_change":"Change implementation","instructions":"Implement AC-1","requirement_ids":["AC-1"],"dependencies":[],"checks":["go test ./..."],"effect_scope":["provider.select_files","provider.propose","workspace.apply","workspace.run","provider.review","proposal.reduce"]}]}`
	service := providers.NewService(db, reviewAdapter{completion: providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_plan", Arguments: body}}}})
	profile, err := service.Save(ctx, providers.SaveInput{Name: "planner reduction", BaseURL: "https://plan.example/v1", Model: "fixture", ContextWindow: 65536, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	result, err := PlanWithProviderService(ctx, service, profile, PlanTask{TaskID: "task-reduction", Objective: "fix", OriginalRequest: "fix it", Criteria: []string{"AC-1: passes"}}, Options{})
	if err != nil || len(result.Steps) != 1 || len(result.Steps[0].EffectScope) != 6 {
		t.Fatal("explicit local derivation scope rejected", err)
	}
}

func TestReductionPreservesOnlySelectedNativeEditAndUsage(t *testing.T) {
	task, full := reductionFixture(t)
	selections, err := RepairSelections(task, full)
	if err != nil || len(selections) != 2 {
		t.Fatal(selections, err)
	}
	before, _ := json.Marshal(full)
	for i, selection := range selections {
		input, result, err := ReduceRepairResult(task, full, "next-attempt", selection)
		if err != nil || result.Status != "proposed_unverified" || result.Usage.TotalTokens != 0 || input.ID != "next-attempt" {
			t.Fatal("reduction failed or fabricated inference", err)
		}
		want := "ReadTimeout: 5 * unit, WriteTimeout: 10 * unit"
		if i == 1 {
			want = "ReadTimeout: 99 * unit, WriteTimeout: 5 * unit"
		}
		if !strings.Contains(result.Changes[0].Content, want) || result.ProposalID == full.ProposalID || result.Changes[0].BeforeSHA256 != "absent" {
			t.Fatal("reduction invented source, identity or preimage")
		}
		if err := ValidateResult(input, result); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := json.Marshal(full)
	if string(before) != string(after) {
		t.Fatal("original native proposal mutated")
	}
}

func TestReductionRejectsTamperingAndUnsupportedSource(t *testing.T) {
	task, full := reductionFixture(t)
	if _, _, err := ReduceRepairResult(task, full, "next", RepairSelection{Path: "api/main.go", TargetID: Hash("invented")}); err == nil {
		t.Fatal("invented edit accepted")
	}
	full.Changes[0].Content += "tampered"
	full.ProposalID = proposalID(full.InputSHA256, full)
	if _, err := RepairSelections(task, full); err == nil {
		t.Fatal("re-signed source without valid proof accepted")
	}
	task, full = reductionFixture(t)
	full.Changes[0].Repair.TargetEdits = full.Changes[0].Repair.TargetEdits[:1]
	derived, err := materializeRepairTargetEdits(task, "api/main.go", full.Changes[0].Repair.TargetEdits)
	if err != nil {
		t.Fatal(err)
	}
	full.Changes[0] = derived
	full.ProposalID = proposalID(full.InputSHA256, full)
	if _, err := RepairSelections(task, full); err == nil {
		t.Fatal("single edit repeated as a reduction")
	}
}

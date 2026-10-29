package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"hermetrix-harness/internal/providers"
)

func atomicFixture(t *testing.T) Task {
	t.Helper()
	task := targetFixture(t)
	task.RepairTargetVersion = AtomicRepairTargetVersion
	task.Files["api/main_test.go"] = ""
	task.AbsentFiles = append(task.AbsentFiles, "api/main_test.go")
	task.WritablePaths = append(task.WritablePaths, "api/main_test.go")
	draft := task.RepairDrafts["api/main.go"]
	draft.Content = "package api\n// original companion; never regenerated\n"
	draft.SourceSHA256 = Hash(draft.Content)
	task.RepairDrafts["api/main_test.go"] = draft
	return task
}

func TestAtomicRepairRestoresCompanionAndRevalidatesProof(t *testing.T) {
	task := atomicFixture(t)
	before, _ := json.Marshal(task)
	target := findTarget(t, task, "NewServer.WriteTimeout")
	args, _ := json.Marshal(map[string]any{"repair": map[string]any{"path": "api/main.go", "target_id": target.ID, "new": "10 * unit"}})
	result, err := Run(context.Background(), &fake{arguments: string(args)}, providers.Profile{}, "", task)
	if err != nil || len(result.Changes) != 2 {
		t.Fatal("atomic repair did not include missing companion", err)
	}
	if !strings.Contains(result.Changes[0].Content, "ReadTimeout: 5 * unit, WriteTimeout: 10 * unit") ||
		result.Changes[1].Content != task.RepairDrafts["api/main_test.go"].Content || !result.Changes[1].Repair.Retain {
		t.Fatal("repair changed unrelated bytes or regenerated companion")
	}
	for _, change := range result.Changes {
		if change.BeforeSHA256 != "absent" {
			t.Fatal("historical source substituted for actual absent preimage")
		}
	}
	after, _ := json.Marshal(task)
	if string(before) != string(after) {
		t.Fatal("worker mutated task")
	}
	b, _ := json.Marshal(result)
	var restored Result
	_ = json.Unmarshal(b, &restored)
	if err = ValidateResult(task, restored); err != nil {
		t.Fatal("atomic proof could not survive persistence", err)
	}
	for _, mutate := range []func(*Result){
		func(r *Result) { r.Changes = r.Changes[:1] },
		func(r *Result) { r.Changes[1].Content += "forged" },
		func(r *Result) { r.Changes[0].Repair.TargetEdits[0].TargetID = Hash("forged") },
		func(r *Result) { r.Changes[0].BeforeSHA256 = Hash(task.RepairDrafts["api/main.go"].Content) },
	} {
		_ = json.Unmarshal(b, &restored)
		mutate(&restored)
		restored.ProposalID = proposalID(restored.InputSHA256, restored)
		if err = ValidateResult(task, restored); err == nil {
			t.Fatal("re-signed tampered atomic result accepted")
		}
	}
}

func TestAtomicRepairRejectsMixedMissingStaleAndReadonly(t *testing.T) {
	task := atomicFixture(t)
	target := findTarget(t, task, "NewServer.WriteTimeout")
	base := `{"path":"api/main.go","target_id":"` + target.ID + `","new":"10 * unit"}`
	for _, raw := range []string{
		`{"repair":null}`, `{"repair":[]}`,
		`{"repair":` + base + `,"changes":[]}`,
		`{"repair":` + base + `,"changes":null}`,
		`{"repair":` + strings.Replace(base, `,"new":"10 * unit"`, "", 1) + `}`,
		`{"repair":` + strings.Replace(base, `"10 * unit"`, "null", 1) + `}`,
		`{"repair":` + strings.Replace(base, `"api/main.go"`, `"readonly.go"`, 1) + `}`,
		`{"repair":` + strings.Replace(base, target.ID, Hash("stale"), 1) + `}`,
		`{"repair":` + strings.Replace(base, `"10 * unit"`, `"5 * unit"`, 1) + `}`,
		`{"repair":` + strings.Replace(base, `"10 * unit"`, `"func broken("`, 1) + `}`,
		`{"repair":` + strings.Replace(base, `"path":`, `"offset":5,"path":`, 1) + `}`,
	} {
		if _, err := Run(context.Background(), &fake{arguments: raw}, providers.Profile{}, "", task); err == nil {
			t.Fatal("invalid atomic submission accepted", raw)
		}
	}
	for _, version := range []int{0, RepairTargetVersion} {
		old := task
		old.RepairTargetVersion = version
		if _, err := Run(context.Background(), &fake{arguments: `{"repair":` + base + `}`}, providers.Profile{}, "", old); err == nil {
			t.Fatal("legacy recipe accepted atomic submission")
		}
		_, _, wire, err := FreezeTask(old)
		if err != nil {
			t.Fatal(err)
		}
		schema, _ := json.Marshal(proposalRequest(old, wire).Tools)
		if strings.Contains(string(schema), `"repair"`) {
			t.Fatal("legacy request acquired new root grammar")
		}
	}
}

func TestAtomicRecipeKeepsExplicitMultiEditRepairAndExistingPreimages(t *testing.T) {
	task := atomicFixture(t)
	read, write := findTarget(t, task, "NewServer.ReadTimeout"), findTarget(t, task, "NewServer.WriteTimeout")
	args, _ := json.Marshal(map[string]any{"changes": []any{
		map[string]any{"path": "api/main.go", "target_edits": []DraftTargetEdit{{TargetID: read.ID, New: "11 * unit"}, {TargetID: write.ID, New: "10 * unit"}}},
		map[string]any{"path": "api/main_test.go", "retain_draft": true},
	}})
	full, err := Run(context.Background(), &fake{arguments: string(args)}, providers.Profile{}, "", task)
	if err != nil || ValidateResult(task, full) != nil {
		t.Fatal("atomic recipe removed multi-edit capability", err)
	}
	if _, err = RepairSelections(task, full); err != nil {
		t.Fatal("existing reduction no longer accepts complete named repairs", err)
	}
	task.AbsentFiles = nil
	task.Files["api/main.go"] = "package api\n// actual previous source\n"
	task.Files["api/main_test.go"] = "package api\n// current companion must stay untouched\n"
	args, _ = json.Marshal(map[string]any{"repair": map[string]any{"path": "api/main.go", "target_id": write.ID, "new": "10 * unit"}})
	result, err := Run(context.Background(), &fake{arguments: string(args)}, providers.Profile{}, "", task)
	if err != nil || len(result.Changes) != 1 || result.Changes[0].BeforeSHA256 != Hash(task.Files["api/main.go"]) {
		t.Fatal("atomic repair overwrote existing companion or lost actual preimage", err)
	}
	_, _, wire, err := FreezeTask(task)
	if err != nil {
		t.Fatal(err)
	}
	request := proposalRequest(task, wire)
	estimate, err := EstimateProposalInput(task)
	if err != nil || estimate.Tokens != providers.EstimateRequestTokens(request) {
		t.Fatal("new grammar escaped dispatch input estimate", err)
	}
}

func TestAtomicRepairSchemaMatchesExclusiveRuntimeForms(t *testing.T) {
	task := atomicFixture(t)
	target := findTarget(t, task, "NewServer.WriteTimeout")
	schema, _ := json.Marshal(proposalSchemaForTask(task))
	document, err := jsonschema.UnmarshalJSON(strings.NewReader(string(schema)))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err = compiler.AddResource("urn:atomic-repair", document); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile("urn:atomic-repair")
	if err != nil {
		t.Fatal(err)
	}
	base := `{"path":"api/main.go","target_id":"` + target.ID + `","new":"10 * unit"}`
	for _, tc := range []struct {
		raw   string
		valid bool
	}{
		{`{"repair":` + base + `}`, true},
		{`{"changes":[{"path":"api/main.go","target_edits":[{"target_id":"` + target.ID + `","new":"10 * unit"}]},{"path":"api/main_test.go","retain_draft":true}]}`, true},
		{`{}`, false}, {`{"repair":null}`, false},
		{`{"repair":` + base + `,"changes":[]}`, false},
		{`{"repair":` + strings.Replace(base, `,"new":"10 * unit"`, "", 1) + `}`, false},
		{`{"repair":` + strings.Replace(base, `"10 * unit"`, "null", 1) + `}`, false},
	} {
		instance, err := jsonschema.UnmarshalJSON(strings.NewReader(tc.raw))
		if err != nil {
			t.Fatal(err)
		}
		if (compiled.Validate(instance) == nil) != tc.valid {
			t.Fatal("schema disagrees with source-form contract", tc.raw)
		}
		_, err = Run(context.Background(), &fake{arguments: tc.raw}, providers.Profile{}, "", task)
		if (err == nil) != tc.valid {
			t.Fatal("runtime disagrees with schema form contract", tc.raw, err)
		}
	}
}

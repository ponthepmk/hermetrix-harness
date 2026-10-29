package worker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"hermetrix-harness/internal/providers"
)

func TestRepairTargetRecipeIsFrozenAndOnWire(t *testing.T) {
	task := repairHappyTask(true)
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err = json.Unmarshal(b, &input); err != nil {
		t.Fatal(err)
	}
	input["repair_target_version"] = 1
	b, _ = json.Marshal(input)
	if err = json.Unmarshal(b, &task); err != nil {
		t.Fatal(err)
	}
	_, canonical, wire, err := FreezeTask(task)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{canonical, wire} {
		var saved map[string]any
		if err = json.Unmarshal(body, &saved); err != nil {
			t.Fatal(err)
		}
		if saved["repair_target_version"] != float64(1) {
			t.Fatal("repair-target derivation version was lost from frozen input")
		}
	}
	var sent map[string]json.RawMessage
	_ = json.Unmarshal(wire, &sent)
	if len(sent["repair_targets"]) == 0 {
		t.Fatal("host-derived target catalog missing from provider wire")
	}
}

func targetFixture(t *testing.T) Task {
	t.Helper()
	task := repairHappyTask(true)
	task.ID, task.Brief = "target-contract", "Repair the response writing timeout, preserving the read timeout"
	task.RepairTargetVersion = RepairTargetVersion
	draft := task.RepairDrafts["api/main.go"]
	draft.Content = "package api\nfunc NewServer() { _ = Config{ReadTimeout: 5 * unit, WriteTimeout: 5 * unit} }\n"
	draft.SourceSHA256 = Hash(draft.Content)
	task.RepairDrafts["api/main.go"] = draft
	return task
}

func findTarget(t *testing.T, task Task, symbol string) RepairTarget {
	t.Helper()
	catalog, err := buildRepairTargets(task)
	if err != nil {
		t.Fatal(err)
	}
	for _, targets := range catalog {
		for _, target := range targets {
			if target.Symbol == symbol {
				return target
			}
		}
	}
	t.Fatal("target not found", symbol)
	return RepairTarget{}
}

func TestRepairTargetNamedSymbolsDoNotConflateEqualValues(t *testing.T) {
	task := targetFixture(t)
	read, write := findTarget(t, task, "NewServer.ReadTimeout"), findTarget(t, task, "NewServer.WriteTimeout")
	if read.ID == write.ID || read.Start == write.Start || read.Kind != "go_keyed_value" || read.Preview != write.Preview {
		t.Fatal("syntax targets not distinct for equal values")
	}
	for _, target := range []RepairTarget{read, write} {
		change, err := materializeRepairTargetEdits(task, "api/main.go", []DraftTargetEdit{{TargetID: target.ID, New: "10 * unit"}})
		if err != nil {
			t.Fatal(err)
		}
		want := "ReadTimeout: 5 * unit, WriteTimeout: 10 * unit"
		if target.ID == read.ID {
			want = "ReadTimeout: 10 * unit, WriteTimeout: 5 * unit"
		}
		if !strings.Contains(change.Content, want) || change.BeforeSHA256 != "absent" || change.Strategy != "create_file" {
			t.Fatalf("wrong actual target/preimage: %+v", change)
		}
		if err = validateRepairDraftChange(task, change); err != nil {
			t.Fatal(err)
		}
	}
	other := targetFixture(t)
	draft := other.RepairDrafts["api/main.go"]
	draft.Content += "\n"
	draft.SourceSHA256 = Hash(draft.Content)
	other.RepairDrafts["api/main.go"] = draft
	if _, err := materializeRepairTargetEdits(other, "api/main.go", []DraftTargetEdit{{TargetID: write.ID, New: "10 * unit"}}); err == nil {
		t.Fatal("target ID reused across source snapshots")
	}
}

func TestRepairTargetWorkerRoundtripAndTamperRejection(t *testing.T) {
	task := targetFixture(t)
	target := findTarget(t, task, "NewServer.WriteTimeout")
	args, _ := json.Marshal(map[string]any{"changes": []any{map[string]any{"path": "api/main.go", "target_edits": []DraftTargetEdit{{TargetID: target.ID, New: "10 * unit"}}}}})
	adapter := &fake{arguments: string(args)}
	result, err := Run(context.Background(), adapter, providers.Profile{}, "", task)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateResult(task, result); err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Task
		Targets map[string][]RepairTarget `json:"repair_targets"`
	}
	if err = json.Unmarshal([]byte(adapter.request.Messages[1].Content), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Files["api/main.go"] != "" || len(wire.Targets["api/main.go"]) == 0 {
		t.Fatal("wire target catalog changed actual source")
	}
	copyBytes, _ := json.Marshal(result)
	var restored Result
	if err = json.Unmarshal(copyBytes, &restored); err != nil {
		t.Fatal(err)
	}
	if err = ValidateResult(task, restored); err != nil {
		t.Fatal(err)
	}
	review, _, err := BuildReviewFiles(task, restored)
	if err != nil || review["api/main.go"] != restored.Changes[0].Content {
		t.Fatal("review did not get actual derived source", err)
	}
	for _, change := range []func(*Change){func(c *Change) { c.Content += "forged" }, func(c *Change) { c.Repair.TargetEdits[0].TargetID = Hash("forged") }, func(c *Change) { c.Repair.Retain = true }, func(c *Change) { c.Repair.Edits = []TextEdit{{Old: "5", New: "10"}} }} {
		var bad Result
		_ = json.Unmarshal(copyBytes, &bad)
		change(&bad.Changes[0])
		bad.ProposalID = proposalID(bad.InputSHA256, bad)
		if err = ValidateResult(task, bad); err == nil {
			t.Fatal("re-signed target proof/content tampering accepted")
		}
	}
}

func TestRepairTargetRejectsWrongFormsAndSpans(t *testing.T) {
	task := targetFixture(t)
	target := findTarget(t, task, "NewServer.WriteTimeout")
	for _, edits := range [][]DraftTargetEdit{
		{{TargetID: Hash("unknown"), New: "10 * unit"}},
		{{TargetID: target.ID, New: "10 * unit"}, {TargetID: target.ID, New: "11 * unit"}},
		{{TargetID: target.ID, New: target.Preview}},
		{{TargetID: target.ID, New: "\xff"}},
	} {
		if _, err := materializeRepairTargetEdits(task, "api/main.go", edits); err == nil {
			t.Fatal("invalid/overlapping target edit accepted")
		}
	}
	for _, extra := range []map[string]any{
		{"content": "replacement"}, {"retain_draft": true}, {"edits": []TextEdit{{Old: "5", New: "10"}}},
	} {
		extra["path"] = "api/main.go"
		extra["target_edits"] = []DraftTargetEdit{{TargetID: target.ID, New: "10 * unit"}}
		args, _ := json.Marshal(map[string]any{"changes": []any{extra}})
		if _, err := Run(context.Background(), &fake{arguments: string(args)}, providers.Profile{}, "", task); err == nil {
			t.Fatal("mixed source forms accepted")
		}
	}
	for _, literal := range []string{"[]", "null", `[{"target_id":"` + target.ID + `","new":"10 * unit","offset":5}]`} {
		args := `{"changes":[{"path":"api/main.go","retain_draft":true,"target_edits":` + literal + `}]}`
		if _, err := Run(context.Background(), &fake{arguments: args}, providers.Profile{}, "", task); err == nil {
			t.Fatal("empty/null/unknown target form accepted")
		}
	}
	task.RepairTargetVersion = 0
	if _, err := materializeRepairTargetEdits(task, "api/main.go", []DraftTargetEdit{{TargetID: target.ID, New: "10 * unit"}}); err == nil {
		t.Fatal("old task accepted new derivation recipe")
	}
	task.RepairTargetVersion = 99
	adapter := &fake{}
	if _, err := Run(context.Background(), adapter, providers.Profile{}, "", task); err == nil || adapter.calls != 0 {
		t.Fatal("unknown recipe reached provider")
	}
}

func TestRepairTargetCatalogBudgetsAndGenericFallback(t *testing.T) {
	task := repairHappyTask(true)
	task.RepairTargetVersion = RepairTargetVersion
	draft := task.RepairDrafts["api/main.go"]
	draft.Content = "package api\nfunc F() { _ = T{" + strings.Repeat("Field: \"กกกกกกกก\",", 200) + "} }\n"
	draft.SourceSHA256 = Hash(draft.Content)
	task.RepairDrafts["api/main.go"] = draft
	catalog, err := buildRepairTargets(task)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(catalog)
	count := 0
	for _, targets := range catalog {
		count += len(targets)
		for i, target := range targets {
			if !utf8.ValidString(target.Preview) {
				t.Fatal("preview split UTF8")
			}
			for _, other := range targets[:i] {
				if target.Start < other.End && other.Start < target.End {
					t.Fatal("catalog offered overlapping named spans")
				}
			}
		}
	}
	if len(b) > maxRepairTargetWireBytes || count > maxRepairTargets {
		t.Fatal("catalog exceeded independent metadata bounds")
	}
	task = repairHappyTask(true)
	task.RepairTargetVersion = RepairTargetVersion
	catalog, err = buildRepairTargets(task)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, target := range catalog["api/main.go"] {
		if target.Kind == "text_line" {
			found = true
		}
	}
	if !found {
		t.Fatal("incomplete/non-Go source lost exact text fallback")
	}
	result := Result{Changes: []Change{{Repair: &DraftEditProof{SourceSHA256: Hash("draft"), TargetEdits: []DraftTargetEdit{{TargetID: Hash("target"), New: strings.Repeat("x", maxWorkerRawProposalBytes)}}}}}}
	if err = validateWorkerResultResources(result); err == nil {
		t.Fatal("target proof escaped raw budget")
	}
}

func TestRepairTargetDerivedIDsCannotLeakCredential(t *testing.T) {
	task := targetFixture(t)
	target := findTarget(t, task, "NewServer.WriteTimeout")
	adapter := &fake{}
	if _, err := Run(context.Background(), adapter, providers.Profile{}, target.ID, task); err == nil || adapter.calls != 0 || strings.Contains(err.Error(), target.ID) {
		t.Fatal("derived wire ID leaked credential or reached provider")
	}
	args, _ := json.Marshal(map[string]any{"changes": []any{map[string]any{"path": "api/main.go", "target_edits": []DraftTargetEdit{{TargetID: target.ID, New: "quoted\"credential"}}}}})
	adapter = &fake{arguments: string(args)}
	if _, err := Run(context.Background(), adapter, providers.Profile{}, "quoted\"credential", task); err == nil || adapter.calls != 1 {
		t.Fatal("target replacement credential retained in proof")
	}
}

func TestRepairTargetMissingCompanionCannotBeSilentlyDropped(t *testing.T) {
	task := targetFixture(t)
	target := findTarget(t, task, "NewServer.WriteTimeout")
	task.Files["api/test.go"] = ""
	task.AbsentFiles = append(task.AbsentFiles, "api/test.go")
	task.WritablePaths = append(task.WritablePaths, "api/test.go")
	draft := task.RepairDrafts["api/main.go"]
	draft.Content = "package api\n// original native test fixture\n"
	draft.SourceSHA256 = Hash(draft.Content)
	task.RepairDrafts["api/test.go"] = draft
	change := map[string]any{"path": "api/main.go", "target_edits": []DraftTargetEdit{{TargetID: target.ID, New: "10 * unit"}}}
	args, _ := json.Marshal(map[string]any{"changes": []any{change}, "summary": "Both files restored"})
	if _, err := Run(context.Background(), &fake{arguments: string(args)}, providers.Profile{}, "", task); err == nil {
		t.Fatal("summary substituted for required companion creation")
	}
	args, _ = json.Marshal(map[string]any{"changes": []any{change, map[string]any{"path": "api/test.go", "retain_draft": true}}})
	result, err := Run(context.Background(), &fake{arguments: string(args)}, providers.Profile{}, "", task)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateResult(task, result); err != nil {
		t.Fatal(err)
	}
	result.Changes = result.Changes[:1]
	result.ProposalID = proposalID(result.InputSHA256, result)
	if err = ValidateResult(task, result); err == nil {
		t.Fatal("re-signed persisted result dropped companion")
	}
}

func TestRepairTargetCannotIntroduceGoSyntaxFailure(t *testing.T) {
	task := targetFixture(t)
	draft := task.RepairDrafts["api/main.go"]
	draft.Content = "package api\n\ntype FakePing struct { Count int }\n"
	draft.SourceSHA256 = Hash(draft.Content)
	task.RepairDrafts["api/main.go"] = draft
	target := findTarget(t, task, "FakePing")
	// A real model response inserted an entire function into a TypeSpec, whose
	// span deliberately excludes the enclosing 'type' keyword.
	edits := []DraftTargetEdit{{TargetID: target.ID, New: "func TestReady() {}"}}
	if _, err := materializeRepairTargetEdits(task, "api/main.go", edits); err == nil {
		t.Fatal("syntactically valid historical Go became invalid before apply")
	}
	args, _ := json.Marshal(map[string]any{"changes": []any{map[string]any{"path": "api/main.go", "target_edits": edits}}})
	_, err := Run(context.Background(), &fake{arguments: string(args)}, providers.Profile{}, "", task)
	var diagnostic *Error
	if !errors.As(err, &diagnostic) || diagnostic.Code != ErrorInvalidProposal || diagnostic.Phase != PhaseValidatingProposal {
		t.Fatal("syntax regression did not use bounded invalid-proposal recovery", err)
	}
	// Exact edits cannot bypass the same recipe's syntax invariant.
	if _, err := materializeRepairDraft(task, "api/main.go", nil, []TextEdit{{Old: "FakePing struct { Count int }", New: "func TestReady() {}"}}, nil); err == nil {
		t.Fatal("exact text form bypassed syntax regression check")
	}
	change, err := materializeRepairTargetEdits(task, "api/main.go", []DraftTargetEdit{{TargetID: target.ID, New: "FakePing struct { Count int; Err error }"}})
	if err != nil || !strings.Contains(change.Content, "type FakePing struct") {
		t.Fatal("valid type-spec repair rejected", err)
	}
	// Re-signed persisted proofs are checked as well as fresh model responses.
	bad := change
	bad.Content = strings.Replace(draft.Content, "FakePing struct { Count int }", "func TestReady() {}", 1)
	bad.Repair = &DraftEditProof{SourceSHA256: draft.SourceSHA256, TargetEdits: edits}
	if err := validateRepairDraftChange(task, bad); err == nil {
		t.Fatal("persisted syntax regression accepted")
	}
	// Historical version zero keeps its previous replay semantics.
	task.RepairTargetVersion = 0
	if _, err := materializeRepairDraft(task, "api/main.go", nil, []TextEdit{{Old: "FakePing struct { Count int }", New: "func TestReady() {}"}}, nil); err != nil {
		t.Fatal("historical recipe changed", err)
	}
}

func TestRepairSyntaxDoesNotReplaceCompilerOrRejectMalformedSourceRepair(t *testing.T) {
	task := targetFixture(t)
	target := findTarget(t, task, "NewServer.WriteTimeout")
	// Parsing proves structure only: this undefined name must still reach the
	// actual mandatory compiler/test gate, not be described as verified code.
	if _, err := materializeRepairTargetEdits(task, "api/main.go", []DraftTargetEdit{{TargetID: target.ID, New: "unknownPackage.MissingAPI()"}}); err != nil {
		t.Fatal("syntax guard attempted to replace compilation", err)
	}
	draft := task.RepairDrafts["api/main.go"]
	draft.Content = "package api\nfunc F(\n"
	draft.SourceSHA256 = Hash(draft.Content)
	task.RepairDrafts["api/main.go"] = draft
	if _, err := materializeRepairDraft(task, "api/main.go", nil, []TextEdit{{Old: "func F(", New: "func F() {}"}}, nil); err != nil {
		t.Fatal("malformed historical source cannot be repaired", err)
	}
}

func TestRepairTargetWireDoesNotForbidItsOwnSourceForm(t *testing.T) {
	task := targetFixture(t)
	_, _, wire, err := FreezeTask(task)
	if err != nil {
		t.Fatal(err)
	}
	r := proposalRequest(task, wire)
	message := r.Messages[0].Content
	if strings.Contains(message, "submit ONLY edits") || !strings.Contains(message, "For repair_drafts paths use target_edits") {
		t.Fatal("named-target schema is contradicted by historical-only source instructions")
	}
	task.RepairTargetVersion = 0
	_, _, wire, err = FreezeTask(task)
	if err != nil {
		t.Fatal(err)
	}
	message = proposalRequest(task, wire).Messages[0].Content
	if !strings.Contains(message, "submit ONLY edits") || strings.Contains(message, "target_edits") {
		t.Fatal("legacy worker instruction contract changed")
	}
}

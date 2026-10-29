package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
)

func repairProtocolTask() Task {
	task := repairHappyTask(true)
	task.ID, task.Brief = "repair-contract", "Repair the historical draft using its compiler diagnostic"
	return task
}

func TestRepairDraftWorkerProtocol(t *testing.T) {
	for _, retain := range []bool{false, true} {
		task := repairProtocolTask()
		form := map[string]any{"path": "api/main.go"}
		if retain {
			form["retain_draft"] = true
		} else {
			form["edits"] = []TextEdit{{Old: "draft", New: "fixed"}}
		}
		args, _ := json.Marshal(map[string]any{"changes": []any{form}})
		adapter := &fake{arguments: string(args)}
		result, err := Run(context.Background(), adapter, providers.Profile{}, "", task)
		if err != nil {
			t.Fatalf("retain=%t: %v", retain, err)
		}
		if err = ValidateResult(task, result); err != nil {
			t.Fatal(err)
		}
		change := result.Changes[0]
		want := "fixed version"
		if retain {
			want = "draft version"
		}
		if change.Content != want || change.BeforeSHA256 != "absent" || change.Strategy != "create_file" ||
			change.Repair == nil || change.Repair.Retain != retain || len(change.Edits) != 0 {
			t.Fatalf("wrong derived change: %+v", change)
		}
		var wire Task
		if err = json.Unmarshal([]byte(adapter.request.Messages[1].Content), &wire); err != nil {
			t.Fatal(err)
		}
		if current, ok := wire.Files["api/main.go"]; !ok || current != "" || len(wire.AbsentFiles) != 1 ||
			wire.RepairDrafts["api/main.go"].Content != "draft version" {
			t.Fatal("historical source replaced the actual snapshot on the wire")
		}
		body, _ := json.Marshal(result)
		var persisted Result
		if err = json.Unmarshal(body, &persisted); err != nil {
			t.Fatal(err)
		}
		if err = ValidateResult(task, persisted); err != nil {
			t.Fatal(err)
		}
		files, _, err := BuildReviewFiles(task, persisted)
		if err != nil || files["api/main.go"] != want {
			t.Fatalf("review did not receive rederived source: %v", err)
		}
		for _, mutate := range []func(*Change){
			func(c *Change) { c.Content += "forged" },
			func(c *Change) { c.Repair = nil },
			func(c *Change) { c.Repair.SourceSHA256 = Hash("wrong") },
			func(c *Change) { c.BeforeSHA256 = Hash("") },
		} {
			var bad Result
			_ = json.Unmarshal(body, &bad)
			mutate(&bad.Changes[0])
			bad.ProposalID = proposalID(bad.InputSHA256, bad)
			if err = ValidateResult(task, bad); err == nil {
				t.Fatal("accepted re-signed draft proof/content tampering")
			}
		}
	}
}

func TestRepairDraftRejectsInvalidFormsAndOrdinaryRetention(t *testing.T) {
	for _, args := range []string{
		`{"changes":[{"path":"api/main.go","content":"replacement"}]}`,
		`{"changes":[{"path":"api/main.go","retain_draft":false}]}`,
		`{"changes":[{"path":"api/main.go","retain_draft":true,"edits":[{"old":"draft","new":"fixed"}]}]}`,
		`{"changes":[{"path":"api/main.go","retain_draft":true,"content":"replacement"}]}`,
	} {
		if _, err := Run(context.Background(), &fake{arguments: args}, providers.Profile{}, "", repairProtocolTask()); err == nil {
			t.Fatal("accepted invalid draft source form")
		}
	}
	task := repairProtocolTask()
	task.RepairDrafts = nil
	args := `{"changes":[{"path":"api/main.go","retain_draft":true,"content":"replacement"}]}`
	if _, err := Run(context.Background(), &fake{arguments: args}, providers.Profile{}, "", task); err == nil {
		t.Fatal("retention accepted without an explicit historical draft")
	}
}

func TestRepairDraftCredentialAndProofResourceGuards(t *testing.T) {
	key := "quoted\"credential"
	task := repairProtocolTask()
	draft := task.RepairDrafts["api/main.go"]
	draft.Content += key
	draft.SourceSHA256 = Hash(draft.Content)
	task.RepairDrafts["api/main.go"] = draft
	adapter := &fake{}
	if _, err := Run(context.Background(), adapter, providers.Profile{}, key, task); err == nil || adapter.calls != 0 || strings.Contains(err.Error(), key) {
		t.Fatal("draft credential was sent or exposed")
	}
	// Even a transient secret removed by a later edit must not persist in proof.
	args, _ := json.Marshal(map[string]any{"changes": []any{map[string]any{
		"path": "api/main.go", "edits": []TextEdit{{Old: "draft", New: key}, {Old: key, New: "fixed"}},
	}}})
	adapter = &fake{arguments: string(args)}
	if _, err := Run(context.Background(), adapter, providers.Profile{}, key, repairProtocolTask()); err == nil || adapter.calls != 1 {
		t.Fatal("secret in retained edit proof was accepted")
	}
	result := Result{Changes: []Change{{Repair: &DraftEditProof{SourceSHA256: Hash("draft"), Edits: []TextEdit{{Old: "x", New: strings.Repeat("z", maxWorkerRawProposalBytes)}}}}}}
	if err := validateWorkerResultResources(result); err == nil {
		t.Fatal("draft proof escaped the raw metadata budget")
	}
}

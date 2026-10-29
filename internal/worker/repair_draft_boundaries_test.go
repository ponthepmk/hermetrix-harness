package worker

import (
	"strings"
	"testing"
)

func TestRepairDraftRejectsUnboundScopeAndOrigin(t *testing.T) {
	for _, scenario := range []string{
		"implicit-writable", "readonly", "unselected", "three-files",
		"empty-artifact", "oversize-artifact", "bad-proposal", "bad-input",
		"bad-artifact-sha", "bad-source-sha", "empty-source", "same-as-current",
		"unsafe-path", "false-absence",
	} {
		t.Run(scenario, func(t *testing.T) {
			task := repairHappyTask(false)
			draft := task.RepairDrafts["api/main.go"]
			switch scenario {
			case "implicit-writable":
				task.WritablePaths = nil
			case "readonly":
				task.WritablePaths = []string{"other.go"}
			case "unselected":
				delete(task.Files, "api/main.go")
			case "three-files":
				for _, path := range []string{"api/a.go", "api/b.go"} {
					task.RepairDrafts[path] = draft
					task.Files[path] = "baseline"
					task.WritablePaths = append(task.WritablePaths, path)
				}
			case "empty-artifact":
				draft.SourceArtifactID = " \t"
			case "oversize-artifact":
				draft.SourceArtifactID = strings.Repeat("a", 257)
			case "bad-proposal":
				draft.ProposalID = strings.Repeat("A", 64)
			case "bad-input":
				draft.InputSHA256 = "unverified"
			case "bad-artifact-sha":
				draft.SourceArtifactSHA256 = strings.Repeat("z", 64)
			case "bad-source-sha":
				draft.SourceSHA256 = Hash("different source")
			case "empty-source":
				draft.Content, draft.SourceSHA256 = "", Hash("")
			case "same-as-current":
				task.Files["api/main.go"] = draft.Content
			case "unsafe-path":
				delete(task.RepairDrafts, "api/main.go")
				task.RepairDrafts["../private.go"] = draft
				task.Files["../private.go"] = "baseline"
				task.WritablePaths = append(task.WritablePaths, "../private.go")
			case "false-absence":
				task.AbsentFiles = []string{"api/main.go"}
			}
			if scenario != "unsafe-path" {
				task.RepairDrafts["api/main.go"] = draft
			}
			if _, _, _, err := FreezeTask(task); err == nil {
				t.Fatal("unbound historical source accepted")
			}
		})
	}
}

func TestRepairDraftSharedBudgetAndEmptyExistingPreimage(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		task := repairHappyTask(false)
		// Empty existing files are not absent leaves, even when restoring a draft.
		task.Files["api/main.go"] = ""
		draft := task.RepairDrafts["api/main.go"]
		draft.Content = strings.Repeat("ก", 1365) + "a" // 4096 UTF-8 bytes.
		draft.SourceSHA256 = Hash(draft.Content)
		task.RepairDrafts["api/main.go"] = draft
		task.Files["api/test.go"] = "baseline"
		task.WritablePaths = append(task.WritablePaths, "api/test.go")
		if overflow {
			draft.Content += "b"
			draft.SourceSHA256 = Hash(draft.Content)
		}
		task.RepairDrafts["api/test.go"] = draft
		if _, _, _, err := FreezeTask(task); (err != nil) != overflow {
			t.Fatalf("overflow=%t: %v", overflow, err)
		}
		if overflow {
			continue
		}
		retain := true
		change, err := materializeRepairDraft(task, "api/main.go", nil, nil, &retain)
		if err != nil || change.BeforeSHA256 != Hash("") || change.Strategy != "replace_file" {
			t.Fatalf("empty existing preimage conflated with absence: %+v %v", change, err)
		}
	}
}

func TestRepairDraftProofOwnsEditsAndRejectsMixedRetention(t *testing.T) {
	task := repairHappyTask(false)
	edits := []TextEdit{{Old: "draft", New: "fixed"}}
	change, err := materializeRepairDraft(task, "api/main.go", nil, edits, nil)
	if err != nil {
		t.Fatal(err)
	}
	edits[0].New = "mutated after materialization"
	if err = validateRepairDraftChange(task, change); err != nil {
		t.Fatal("proof aliases caller-owned edit memory", err)
	}
	change.Repair.Retain = true
	if err = validateRepairDraftChange(task, change); err == nil {
		t.Fatal("retention combined with edits accepted in persisted proof")
	}
}

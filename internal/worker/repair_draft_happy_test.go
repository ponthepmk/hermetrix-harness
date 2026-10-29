package worker

import (
	"reflect"
	"testing"
)

func repairHappyTask(absent bool) Task {
	content := "draft version"
	task := Task{
		Files: map[string]string{
			"api/main.go": "baseline",
		},
		WritablePaths: []string{"api/main.go"},
		RepairDrafts: map[string]RepairDraft{
			"api/main.go": RepairDraft{
				ProposalID:           Hash("proposal-fixture"),
				InputSHA256:          Hash("input-fixture"),
				SourceArtifactSHA256: Hash("source-artifact-fixture"),
				SourceSHA256:         Hash(content),
				SourceArtifactID:     "artifact-fixture",
				Content:              content,
			},
		},
	}
	if absent {
		task.Files["api/main.go"] = ""
		task.AbsentFiles = []string{"api/main.go"}
	}
	return task
}

func TestRepairDraftActualPreimages(t *testing.T) {
	retain := true
	edit := []TextEdit{{Old: "draft", New: "fixed"}}
	for _, tc := range []struct {
		name   string
		absent bool
		retain *bool
		edits  []TextEdit
		want   Change
	}{
		{"existing edit", false, nil, edit, Change{
			Path:         "api/main.go",
			BeforeSHA256: Hash("baseline"),
			Content:      "fixed version",
			Strategy:     "replace_file",
			Repair:       &DraftEditProof{SourceSHA256: Hash("draft version"), Edits: edit, Retain: false},
		}},
		{"existing retain", false, &retain, nil, Change{
			Path:         "api/main.go",
			BeforeSHA256: Hash("baseline"),
			Content:      "draft version",
			Strategy:     "replace_file",
			Repair:       &DraftEditProof{SourceSHA256: Hash("draft version"), Retain: true},
		}},
		{"absent edit", true, nil, edit, Change{
			Path:         "api/main.go",
			BeforeSHA256: "absent",
			Content:      "fixed version",
			Strategy:     "create_file",
			Repair:       &DraftEditProof{SourceSHA256: Hash("draft version"), Edits: edit, Retain: false},
		}},
		{"absent retain", true, &retain, nil, Change{
			Path:         "api/main.go",
			BeforeSHA256: "absent",
			Content:      "draft version",
			Strategy:     "create_file",
			Repair:       &DraftEditProof{SourceSHA256: Hash("draft version"), Retain: true},
		}},
	} {
		task := repairHappyTask(tc.absent)
		current := task.Files["api/main.go"]
		change, err := materializeRepairDraft(task, "api/main.go", nil, tc.edits, tc.retain)
		if err != nil {
			t.Fatalf("materializeRepairDraft: %v", err)
		}
		if !reflect.DeepEqual(change, tc.want) {
			t.Fatalf("change mismatch: got %+v want %+v", change, tc.want)
		}
		if err := validateRepairDraftChange(task, change); err != nil {
			t.Fatalf("validateRepairDraftChange: %v", err)
		}
		if task.Files["api/main.go"] != current {
			t.Fatalf("task file changed: got %q want %q", task.Files["api/main.go"], current)
		}
	}
}

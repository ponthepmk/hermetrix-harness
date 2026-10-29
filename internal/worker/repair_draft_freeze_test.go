package worker

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRepairDraftFreezeBinding(t *testing.T) {
	task := repairHappyTask(true)
	task.ID = "binding-test"
	task.Brief = "binding test"

	snapshot, canonical, wire, err := FreezeTask(task)
	if err != nil {
		t.Fatalf("FreezeTask: %v", err)
	}
	if !reflect.DeepEqual(snapshot.RepairDrafts, task.RepairDrafts) {
		t.Fatal("snapshot RepairDrafts mismatch")
	}
	if snapshot.Files["api/main.go"] != "" || !reflect.DeepEqual(snapshot.AbsentFiles, []string{"api/main.go"}) {
		t.Fatal("actual file not empty/absent")
	}

	var canon, wireTask Task
	if err := json.Unmarshal(canonical, &canon); err != nil {
		t.Fatalf("canonical decode: %v", err)
	}
	if err := json.Unmarshal(wire, &wireTask); err != nil {
		t.Fatalf("wire decode: %v", err)
	}
	if canon.RepairDrafts["api/main.go"].Content != "draft version" || canon.RepairDrafts["api/main.go"].SourceSHA256 != Hash("draft version") {
		t.Fatal("canonical draft content/sourceSHA mismatch")
	}
	if wireTask.RepairDrafts["api/main.go"].Content != "draft version" || wireTask.RepairDrafts["api/main.go"].SourceSHA256 != Hash("draft version") {
		t.Fatal("wire draft content/sourceSHA mismatch")
	}

	// Feature changes canonical hash compared with otherwise identical task.RepairDrafts=nil.
	task2 := repairHappyTask(true)
	task2.ID = "binding-test"
	task2.Brief = "binding test"
	task2.RepairDrafts = nil
	snapshot2, canonical2, _, err := FreezeTask(task2)
	if err != nil {
		t.Fatalf("FreezeTask task2: %v", err)
	}
	if !reflect.DeepEqual(snapshot2.RepairDrafts, task2.RepairDrafts) {
		t.Fatal("snapshot2 RepairDrafts mismatch")
	}
	if string(canonical) == string(canonical2) {
		t.Fatal("canonical hash should differ when RepairDrafts present")
	}

	// Mutate original map's struct entry by temp value+reassignment and verify frozen map content/sourceSHA unchanged (no alias).
	draft := task.RepairDrafts["api/main.go"]
	draft.Content = "mutated"
	task.RepairDrafts["api/main.go"] = draft
	if snapshot.RepairDrafts["api/main.go"].Content != "draft version" || snapshot.RepairDrafts["api/main.go"].SourceSHA256 != Hash("draft version") {
		t.Fatal("frozen draft mutated (alias)")
	}
}

func TestRepairDraftFreezeByteBoundary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		wantErr bool
	}{
		{"accept 8192 bytes", strings.Repeat("ก", 2730) + "ab", false},
		{"reject 8193 bytes", strings.Repeat("ก", 2730) + "abc", true},
		{"reject invalid UTF8", "\xff\xfe", true},
	} {
		task := repairHappyTask(true)
		draft := task.RepairDrafts["api/main.go"]
		draft.Content = tc.content
		draft.SourceSHA256 = Hash(tc.content)
		task.RepairDrafts["api/main.go"] = draft
		_, _, _, err := FreezeTask(task)
		if (err != nil) != tc.wantErr {
			t.Fatalf("%s: err=%v wantErr=%v", tc.name, err, tc.wantErr)
		}
		if task.Files["api/main.go"] != "" {
			t.Fatalf("actual source preimage not empty")
		}
		if !tc.wantErr {
			if task.RepairDrafts["api/main.go"].SourceArtifactSHA256 == "" {
				t.Fatalf("SourceArtifact metadata invalid")
			}
		}
	}
}

func TestRepairDraftFreezeViewConflict(t *testing.T) {
	task := repairHappyTask(false)
	text := task.Files["api/main.go"]
	view, err := NewBoundedFileView("api/main.go", text, Hash(text), 0, len(text))
	if err != nil {
		t.Fatalf("NewBoundedFileView: %v", err)
	}
	task.FileViews = map[string][]BoundedFileView{"api/main.go": {view}}

	savedDrafts := task.RepairDrafts
	task.RepairDrafts = nil
	if _, _, _, err := FreezeTask(task); err != nil {
		t.Fatalf("valid view no drafts FreezeTask: %v", err)
	}
	task.RepairDrafts = savedDrafts

	if _, _, _, err := FreezeTask(task); err == nil {
		t.Fatal("valid view with repair draft should reject")
	}
}

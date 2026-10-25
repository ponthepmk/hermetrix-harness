package taskengine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hermetrix-harness/internal/store"
)

func TestStepOutputScopePersistenceRoundTrip(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dataStore, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	svc := NewService(dataStore)
	task, err := svc.Create(ctx, CreateTaskInput{Title: "out", Objective: "persist outputs",
		OriginalRequest: "persist outputs", Criteria: []Criterion{{ID: "AC1", Description: "scope"}},
		Contract: GoalContract{WritablePaths: []string{"a", "b"}}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = svc.CreatePlan(ctx, CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Reason: "plan", Actor: "planner",
		Steps: []StepSpec{{Key: "s1", Title: "S1", Instructions: "work",
			RequirementIDs: []string{"AC1"}, Checks: []string{"c1"}, OutputPaths: []string{"a"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := task.Plan.Steps[0].OutputPaths; len(got) != 1 || got[0] != "a" {
		t.Fatalf("OutputPaths=%v, want [a]", got)
	}
	taskID, planRev, taskRev, subjectHash := task.ID, task.Plan.Revision, task.Revision, task.Plan.SubjectHash
	if err := dataStore.Close(); err != nil {
		t.Fatal(err)
	}
	dataStore2, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore2.Close()
	svc2 := NewService(dataStore2)
	reopened, err := svc2.Get(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Revision != taskRev {
		t.Fatalf("revision=%d want %d", reopened.Revision, taskRev)
	}
	if got := reopened.Plan.Steps[0].OutputPaths; len(got) != 1 || got[0] != "a" {
		t.Fatalf("reopened OutputPaths=%v, want [a]", got)
	}
	if reopened.Plan.SubjectHash != subjectHash {
		t.Fatalf("hash=%q want %q", reopened.Plan.SubjectHash, subjectHash)
	}
	if _, err := dataStore2.DB.Exec(`UPDATE task_steps SET output_paths_json=? WHERE task_id=? AND plan_revision=?`,
		`["b"]`, taskID, planRev); err != nil {
		if strings.Contains(err.Error(), "no such column") {
			t.Fatal("featuremissing: output_paths_json column absent from schema")
		}
		t.Fatal(err)
	}
	plan, err := svc2.GetPlanRevision(ctx, taskID, planRev)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Steps[0].OutputPaths; len(got) != 1 || got[0] != "b" {
		t.Fatalf("mutated OutputPaths=%v, want [b]", got)
	}
	if plan.SubjectHash == subjectHash {
		t.Fatal("subject hash unchanged after output path mutation")
	}
}

func TestStepOutputScopePersistenceJSONOmitEmpty(t *testing.T) {
	blank := StepSpec{Key: "k", Title: "T", Instructions: "I"}
	data, err := json.Marshal(blank)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "output_paths") {
		t.Fatalf("blank JSON has output_paths: %s", data)
	}
	explicit := StepSpec{Key: "k", Title: "T", Instructions: "I", OutputPaths: []string{"a", "b"}}
	data2, err := json.Marshal(explicit)
	if err != nil {
		t.Fatal(err)
	}
	var dec StepSpec
	if err := json.Unmarshal(data2, &dec); err != nil {
		t.Fatal(err)
	}
	if len(dec.OutputPaths) != 2 || dec.OutputPaths[0] != "a" || dec.OutputPaths[1] != "b" {
		t.Fatalf("decoded=%v, want [a b]", dec.OutputPaths)
	}
}

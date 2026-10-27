package skills

import (
	"context"
	"testing"
)

func TestBehavioralRunBaseSnapshotPinsOwnedVersion(t *testing.T) {
	service, dataStore := testService(t)
	candidate := improvementAwaitingEval(t, service)
	bindings, err := loadBindingsCases(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) == 0 {
		t.Fatal("no bindings cases")
	}
	binding := bindings[0]
	run, err := service.PrepareBehavioralRun(context.Background(), "base-owned-positive", candidate.ID, binding)
	if err != nil {
		t.Fatal(err)
	}
	version, err := service.GetVersion(context.Background(), candidate.BaseVersionID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Manifest.BaseVersionID != candidate.BaseVersionID ||
		run.Manifest.BaseHash != version.ContentHash ||
		run.Manifest.BaseBlobRef != version.PackageBlobRef {
		t.Fatalf("base snapshot mismatch: %v %v %v", run.Manifest.BaseVersionID, run.Manifest.BaseHash, run.Manifest.BaseBlobRef)
	}
	if run.Manifest.BaseVersionID == "" || run.Manifest.BaseHash == "" || run.Manifest.BaseBlobRef == "" {
		t.Fatal("base snapshot fields empty")
	}
	var count int
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("run count=%d err=%v", count, err)
	}
}

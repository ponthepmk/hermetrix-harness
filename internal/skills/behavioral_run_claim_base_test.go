package skills

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestBehavioralRunClaimRefusesChangedCurrentBase(t *testing.T) {
	service, dataStore := testService(t)
	ctx := context.Background()
	candidate := improvementAwaitingEval(t, service)
	bindings, err := loadBindingsCases(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) == 0 {
		t.Fatal("no bindings cases")
	}
	binding := bindings[0]

	originalBase := candidate.BaseVersionID
	if originalBase == "" {
		t.Fatal("candidate.BaseVersionID must be nonempty")
	}

	run, err := service.PrepareBehavioralRun(ctx, "claim-base-run", candidate.ID, binding)
	if err != nil {
		t.Fatalf("expected valid prepared run, got err=%v", err)
	}
	if run.ID == "" {
		t.Fatalf("expected nonempty run ID, got %q", run.ID)
	}
	if run.ManifestSHA256 == "" {
		t.Fatalf("expected nonempty manifest sha256, got %q", run.ManifestSHA256)
	}
	if run.Manifest.BaseVersionID != originalBase {
		t.Fatalf("prepared run base changed, got %q", run.Manifest.BaseVersionID)
	}

	newID := "claim-base-new-version"
	insertSQL := `INSERT INTO skill_versions (id, skill_id, parent_version_id, content_hash, package_blob_ref, manifest_json, author_actor, change_message, created_at)
	SELECT ?, skill_id, id, content_hash, package_blob_ref, manifest_json, author_actor, change_message, created_at
	FROM skill_versions WHERE id = ?`
	res, err := dataStore.DB.ExecContext(ctx, insertSQL, newID, originalBase)
	if err != nil {
		t.Fatal(err)
	}
	count, countErr := res.RowsAffected()
	if countErr != nil || count != 1 {
		t.Fatalf("expected 1 row affected, got count=%d countErr=%v", count, countErr)
	}

	updateSQL := `UPDATE skills SET current_version_id = ? WHERE id = ?`
	res, err = dataStore.DB.ExecContext(ctx, updateSQL, newID, candidate.TargetSkillID)
	if err != nil {
		t.Fatal(err)
	}
	count, countErr = res.RowsAffected()
	if countErr != nil || count != 1 {
		t.Fatalf("expected 1 row affected, got count=%d countErr=%v", count, countErr)
	}

	if candidate.BaseVersionID != originalBase {
		t.Fatalf("candidate.BaseVersionID changed, got %s", candidate.BaseVersionID)
	}

	claim, err := service.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, binding)
	if err == nil {
		t.Fatalf("expected error, got run ID=%s", claim.ID)
	}
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected ErrRevisionConflict, got %v", err)
	}
	if !reflect.DeepEqual(claim, BehavioralRun{}) {
		t.Fatalf("expected zero BehavioralRun, got %+v", claim)
	}

	full, err := service.GetBehavioralRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("expected prepared run record, got err=%v", err)
	}
	if !reflect.DeepEqual(full, run) {
		t.Fatalf("prepared run record differs from prepared run: %+v", full)
	}

	var runCount int64
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&runCount); err != nil || runCount != 1 {
		t.Fatalf("expected 1 behavioral_eval_runs, got count=%d err=%v", runCount, err)
	}

	var evalCount int64
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM candidate_behavioral_evals`).Scan(&evalCount); err != nil || evalCount != 0 {
		t.Fatalf("expected 0 candidate_behavioral_evals, got count=%d err=%v", evalCount, err)
	}
}

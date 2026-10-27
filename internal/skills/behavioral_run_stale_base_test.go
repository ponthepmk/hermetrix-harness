package skills

import (
	"context"
	"errors"
	"testing"
)

func TestBehavioralRunRefusesStaleCurrentBase(t *testing.T) {
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

	newID := "stale-base-new-version"
	originalBase := candidate.BaseVersionID
	if originalBase == "" {
		t.Fatal("candidate.BaseVersionID must be nonempty")
	}

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

	run, err := service.PrepareBehavioralRun(ctx, "stale-base-refused", candidate.ID, binding)
	if err == nil {
		t.Fatalf("expected error, got run ID=%s", run.ID)
	}
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected ErrRevisionConflict, got %v", err)
	}
	if run.ID != "" {
		t.Fatalf("expected empty run ID, got %s", run.ID)
	}

	var countSQL int64
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&countSQL); err != nil || countSQL != 0 {
		t.Fatalf("expected 0 runs, got count=%d err=%v", countSQL, err)
	}
}

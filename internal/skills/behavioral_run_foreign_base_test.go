package skills

import (
	"context"
	"errors"
	"testing"
)

func TestBehavioralRunRefusesOtherSkillBase(t *testing.T) {
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

	otherInput := candidateInput("other-baseline", "Other baseline", "# Other procedure")
	other, err := service.CreateCandidate(ctx, otherInput)
	if err != nil {
		t.Fatal(err)
	}
	otherSkill, err := service.PromoteCandidate(ctx, other.ID, "user", other.Revision)
	if err != nil {
		t.Fatal(err)
	}

	res, err := dataStore.DB.ExecContext(ctx, `UPDATE skill_candidates SET base_version_id = ? WHERE id = ?`, otherSkill.CurrentVersionID, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	count, countErr := res.RowsAffected()
	if countErr != nil || count != 1 {
		t.Fatalf("expected 1 row affected, got count=%d countErr=%v", count, countErr)
	}

	run, err := service.PrepareBehavioralRun(ctx, "foreign-base-rejected", candidate.ID, binding)
	if err == nil {
		t.Fatalf("expected error, got run ID=%s", run.ID)
	}
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected ErrRevisionConflict, got %v", err)
	}
	if run.ID != "" {
		t.Fatalf("expected empty run ID, got %s", run.ID)
	}

	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expected 0 runs, got count=%d err=%v", count, err)
	}
}

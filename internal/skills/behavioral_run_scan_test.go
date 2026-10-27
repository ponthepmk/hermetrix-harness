package skills

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestBehavioralRunScanRejectsDenormalizedAndNullCorruption(t *testing.T) {
	service, dataStore := testService(t)
	candidate := improvementAwaitingEval(t, service)
	bindings, err := loadBindingsCases(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) == 0 {
		t.Fatal("no bindings cases")
	}
	run, err := service.PrepareBehavioralRun(context.Background(), "scan-owned", candidate.ID, bindings[0])
	if err != nil {
		t.Fatal(err)
	}
	valid, err := scanBehavioralRun(dataStore.DB.QueryRowContext(context.Background(), behavioralRunSelect+` WHERE id = ?`, run.ID))
	if err != nil {
		t.Fatalf("valid row scan: %v", err)
	}
	if !reflect.DeepEqual(valid, run) {
		t.Fatalf("valid row not equal: %v", valid)
	}
	queries := []string{
		strings.Replace(behavioralRunSelect, "owner_principal_id", `"other-owner"`, 1) + ` WHERE id = ?`,
		strings.Replace(behavioralRunSelect, "candidate_id", `"other-candidate"`, 1) + ` WHERE id = ?`,
		strings.Replace(behavioralRunSelect, "candidate_revision", `candidate_revision + 1`, 1) + ` WHERE id = ?`,
		strings.Replace(behavioralRunSelect, "candidate_hash", `"`+strings.Repeat("f", 64)+`"`, 1) + ` WHERE id = ?`,
		strings.Replace(behavioralRunSelect, "completed_at", `''`, 1) + ` WHERE id = ?`,
		strings.Replace(behavioralRunSelect, "started_at", `"not-a-time"`, 1) + ` WHERE id = ?`,
	}
	for _, query := range queries {
		var zero BehavioralRun
		r, err := scanBehavioralRun(dataStore.DB.QueryRowContext(context.Background(), query, run.ID))
		if !errors.Is(err, ErrBehavioralRunBindings) {
			t.Fatalf("query %q: expected ErrBehavioralRunBindings, got %v", query, err)
		}
		if !reflect.DeepEqual(r, zero) {
			t.Fatalf("query %q: expected zero run, got %v", query, zero)
		}
	}
	var missing BehavioralRun
	missing, err = scanBehavioralRun(dataStore.DB.QueryRowContext(context.Background(), behavioralRunSelect+` WHERE id = ?`, "missing-scan-id"))
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing id: expected sql.ErrNoRows, got %v", err)
	}
	if !reflect.DeepEqual(missing, BehavioralRun{}) {
		t.Fatalf("missing id: expected zero BehavioralRun, got %+v", missing)
	}
}

package taskcoord

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestCodingOperationImmutableRecord(t *testing.T) {
	a := &goalAdapter{}
	f := newCodingOperationFixture(t, a)
	result, err := f.operations.Start(context.Background(), f.input)
	if err != nil {
		t.Fatalf("create coding operation: %v", err)
	}
	saved := result

	requestSHA256 := strings.Repeat("f", 64)
	if len(requestSHA256) != 64 {
		t.Fatalf("request sha256 length = %d, want 64", len(requestSHA256))
	}

	stmt, err := f.data.DB.Prepare("UPDATE coding_goal_operations SET request_sha256=? WHERE id=? AND owner_principal_id=?")
	if err != nil {
		t.Fatalf("prepare update: %v", err)
	}
	defer stmt.Close()
	if _, err := stmt.ExecContext(t.Context(), requestSHA256, saved.Operation.ID, saved.Operation.OwnerPrincipalID); err == nil {
		t.Fatal("update coding_goal_operations succeeded, want immutable trigger failure")
	}

	stmt2, err := f.data.DB.Prepare("DELETE FROM coding_goal_operations WHERE id=? AND owner_principal_id=?")
	if err != nil {
		t.Fatalf("prepare delete: %v", err)
	}
	defer stmt2.Close()
	if _, err := stmt2.ExecContext(t.Context(), saved.Operation.ID, saved.Operation.OwnerPrincipalID); err == nil {
		t.Fatal("delete coding_goal_operations succeeded, want immutable trigger failure")
	}

	got, err := f.operations.Get(context.Background(), saved.Operation.ID)
	if err != nil {
		t.Fatalf("get coding operation after failed updates: %v", err)
	}
	if !reflect.DeepEqual(got, saved) {
		t.Fatalf("coding operation changed after failed updates: got %+v, want %+v", got, saved)
	}

	assertCodingOperationCounts(t, f.data, 2, 1, 1)
	if a.plans != 0 || a.proposals != 0 || a.reviews != 0 {
		t.Fatalf("fake provider counters nonzero: plans=%d proposals=%d reviews=%d", a.plans, a.proposals, a.reviews)
	}
}

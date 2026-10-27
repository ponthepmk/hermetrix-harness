package taskcoord

import (
	"context"
	"reflect"
	"testing"
)

func TestCodingOperationReopenReplay(t *testing.T) {
	f := newCodingOperationFixture(t, &goalAdapter{})
	res, err := f.operations.Start(context.Background(), f.input)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if res.Goal == nil {
		t.Fatal("start: goal is nil")
	}
	if _, err := f.operations.Advance(context.Background(), res.Operation.ID); err != nil {
		t.Fatalf("advance: %v", err)
	}
	op, err := f.operations.Get(context.Background(), res.Operation.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if op.Goal == nil {
		t.Fatal("get: goal is nil")
	}
	saved := op
	if f.adapter.plans != 1 || f.adapter.proposals != 0 || f.adapter.reviews != 0 {
		t.Fatalf("plans=%d proposals=%d reviews=%d, want 1 0 0", f.adapter.plans, f.adapter.proposals, f.adapter.reviews)
	}
	f = reopenCodingOperationFixture(t, f)
	replayed, err := f.operations.Start(context.Background(), f.input)
	if err != nil {
		t.Fatalf("replay start: %v", err)
	}
	if !reflect.DeepEqual(replayed, saved) {
		t.Fatalf("replay start mismatch: got %+v, want %+v", replayed, saved)
	}
	replayedGoal, err := f.operations.Get(context.Background(), replayed.Operation.ID)
	if err != nil {
		t.Fatalf("replay get: %v", err)
	}
	if !reflect.DeepEqual(replayedGoal, saved) {
		t.Fatalf("replay get mismatch: got %+v, want %+v", replayedGoal, saved)
	}
	assertCodingOperationCounts(t, f.data, 2, 1, 1)
	if f.adapter.plans != 1 || f.adapter.proposals != 0 || f.adapter.reviews != 0 {
		t.Fatalf("plans=%d proposals=%d reviews=%d, want 1 0 0", f.adapter.plans, f.adapter.proposals, f.adapter.reviews)
	}
}

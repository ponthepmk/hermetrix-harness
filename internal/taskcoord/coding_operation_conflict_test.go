package taskcoord

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"hermetrix-harness/internal/taskengine"
)

func TestCodingOperationChangedRequest(t *testing.T) {
	f := newCodingOperationFixture(t, nil)
	ctx := context.Background()

	result, err := f.operations.Start(ctx, f.input)
	if err != nil {
		t.Fatalf("start original: %v", err)
	}
	if result.Goal == nil {
		t.Fatalf("start original result goal nil")
	}

	for _, name := range []string{"title", "maxActions", "actor"} {
		t.Run(name, func(t *testing.T) {
			modified := f.input
			switch name {
			case "title":
				modified.Task.Title = "changed"
			case "maxActions":
				modified.Goal.MaxActions = 1
			case "actor":
				modified.Goal.Actor = "changed"
			}
			if modified.OperationID != f.input.OperationID {
				t.Fatalf("operation id changed")
			}
			got, err := f.operations.Start(ctx, modified)
			if !errors.Is(err, taskengine.ErrStaleRevision) {
				t.Fatalf("start modified %s: %v", name, err)
			}
			if !reflect.DeepEqual(got, CodingOperationResult{}) {
				t.Fatalf("start modified %s result not zero", name)
			}
			stored, readErr := f.operations.Get(ctx, f.input.OperationID)
			if readErr != nil {
				t.Fatalf("get after %s: %v", name, readErr)
			}
			if !reflect.DeepEqual(stored, result) {
				t.Fatalf("get after %s changed", name)
			}
			if f.adapter.plans != 0 || f.adapter.proposals != 0 || f.adapter.reviews != 0 {
				t.Fatalf("adapter counts not zero: plans=%d proposals=%d reviews=%d", f.adapter.plans, f.adapter.proposals, f.adapter.reviews)
			}
			assertCodingOperationCounts(t, f.data, 2, 1, 1)
		})
	}
}

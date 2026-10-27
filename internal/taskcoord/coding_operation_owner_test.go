package taskcoord

import (
	"context"
	"reflect"
	"testing"

	"hermetrix-harness/internal/identity"
)

func TestCodingOperationUnknownOwner(t *testing.T) {
	a := &goalAdapter{}
	fixture := newCodingOperationFixture(t, a)
	input := fixture.input

	ownerCtx := context.Background()
	saved, err := fixture.operations.Start(ownerCtx, input)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if saved.Goal == nil {
		t.Fatalf("Start result Goal = nil, want nonnil")
	}

	unknownCtx := identity.WithPrincipal(ownerCtx, "unknown-owner")
	nonOwnerStart, err := fixture.operations.Start(unknownCtx, input)
	if err == nil {
		t.Fatalf("Start under unknown owner: expected error")
	}
	if !reflect.DeepEqual(nonOwnerStart, CodingOperationResult{}) {
		t.Fatalf("Start under unknown owner result = %+v, want zero", nonOwnerStart)
	}

	got, err := fixture.operations.Get(unknownCtx, input.OperationID)
	if err == nil {
		t.Fatalf("Get: expected error, got nil")
	}
	if !reflect.DeepEqual(got, CodingOperationResult{}) {
		t.Fatalf("Get result = %+v, want zero", got)
	}

	adv, err := fixture.operations.Advance(unknownCtx, input.OperationID)
	if err == nil {
		t.Fatalf("Advance: expected error, got nil")
	}
	if !reflect.DeepEqual(adv, CodingOperationResult{}) {
		t.Fatalf("Advance result = %+v, want zero", adv)
	}

	final, err := fixture.operations.Get(ownerCtx, input.OperationID)
	if err != nil {
		t.Fatalf("owner Get: %v", err)
	}
	if !reflect.DeepEqual(final, saved) {
		t.Fatalf("owner Get = %+v, want saved %+v", final, saved)
	}

	assertCodingOperationCounts(t, fixture.data, 2, 1, 1)

	if fixture.adapter.plans != 0 || fixture.adapter.proposals != 0 || fixture.adapter.reviews != 0 {
		t.Fatalf("adapter counts = plans:%d proposals:%d reviews:%d, want 0,0,0",
			fixture.adapter.plans, fixture.adapter.proposals, fixture.adapter.reviews)
	}
}

package taskcoord

import (
	"context"
	"testing"

	"hermetrix-harness/internal/taskengine"
)

func newPendingCodingOperationFixture(t *testing.T) (codingOperationFixture, CodingOperation) {
	t.Helper()

	f := newCodingOperationFixture(t, &goalAdapter{})
	ctx := context.Background()

	normalized, raw, sha, err := canonicalCodingInput(f.input)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	owner, err := codingOperationOwner(ctx, f.data.DB)
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	cfg, err := f.coordinator.resolveCodingGoalConfig(ctx, taskengine.Task{
		ProjectID:    normalized.Task.ProjectID,
		EgressPolicy: normalized.Task.EgressPolicy,
	}, normalized.Goal)
	if err != nil {
		t.Fatalf("goal config: %v", err)
	}
	op, err := f.operations.createCodingOperation(ctx, normalized, raw, sha, owner, cfg)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	return f, op
}

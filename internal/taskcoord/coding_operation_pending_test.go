package taskcoord

import (
	"context"
	"reflect"
	"testing"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
)

func TestCodingOperationPendingRecovery(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		f := newCodingOperationFixture(t, nil)
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
		got, err := f.operations.Get(ctx, op.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if !reflect.DeepEqual(got.Operation, op) {
			t.Fatalf("operation mismatch")
		}
		if got.Task.ID != op.TaskID {
			t.Fatalf("task mismatch")
		}
		if got.Goal != nil {
			t.Fatalf("goal should be nil")
		}
		if f.adapter.plans != 0 || f.adapter.proposals != 0 || f.adapter.reviews != 0 {
			t.Fatalf("fake transport counters should be zero")
		}
		assertCodingOperationCounts(t, f.data, 2, 1, 0)
	})
	t.Run("changed_root", func(t *testing.T) {
		f := newCodingOperationFixture(t, nil)
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
		movedRoot := t.TempDir()
		if _, err := f.coordinator.products.SaveProject(ctx, product.ProjectInput{
			ID:       f.input.Task.ProjectID,
			Name:     "moved",
			RootPath: movedRoot,
		}); err != nil {
			t.Fatalf("save moved project: %v", err)
		}
		_, err = f.operations.Advance(ctx, op.ID)
		if err == nil {
			t.Fatalf("advance should fail after root change")
		}
		got, err := f.operations.Get(ctx, op.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Goal != nil {
			t.Fatalf("goal should be nil")
		}
		assertCodingOperationCounts(t, f.data, 2, 1, 0)
		if _, err := f.coordinator.products.SaveProject(ctx, product.ProjectInput{
			ID:       f.input.Task.ProjectID,
			Name:     f.input.Task.Title,
			RootPath: f.root,
		}); err != nil {
			t.Fatalf("restore project: %v", err)
		}
		_, err = f.operations.Advance(ctx, op.ID)
		if err != nil {
			t.Fatalf("advance after restore: %v", err)
		}
		got, err = f.operations.Get(ctx, op.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Goal == nil {
			t.Fatalf("goal should be non-nil")
		}
		if got.Goal.ID != op.GoalID {
			t.Fatalf("goal id mismatch")
		}
		assertCodingOperationCounts(t, f.data, 2, 1, 1)
	})
	t.Run("start_again", func(t *testing.T) {
		f := newCodingOperationFixture(t, nil)
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
		_, err = f.operations.Advance(ctx, op.ID)
		if err != nil {
			t.Fatalf("advance: %v", err)
		}
		got, err := f.operations.Get(ctx, op.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if !reflect.DeepEqual(got.Operation, op) {
			t.Fatalf("operation mismatch")
		}
		if got.Goal == nil {
			t.Fatalf("goal should be non-nil")
		}
		assertCodingOperationCounts(t, f.data, 2, 1, 1)
	})
}

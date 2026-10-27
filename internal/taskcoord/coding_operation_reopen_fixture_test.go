package taskcoord

import (
	"context"
	"testing"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

func reopenCodingOperationFixture(t *testing.T, f codingOperationFixture) codingOperationFixture {
	t.Helper()
	ctx := context.Background()
	dataRoot := f.data.Root
	f.coordinator.products.Close()
	if err := f.data.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	db, err := store.Open(ctx, dataRoot)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("close reopened store: %v", err)
		}
	})
	p := product.NewService(db, skills.NewService(db))
	t.Cleanup(p.Close)
	ps := providers.NewService(db, f.adapter)
	f.data = db
	f.tasks = taskengine.NewService(db)
	f.coordinator = New(f.tasks, p, ps)
	f.operations = NewCodingOperations(db, f.coordinator)
	return f
}

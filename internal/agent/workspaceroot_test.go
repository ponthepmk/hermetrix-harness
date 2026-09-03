package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/runtime"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	toolruntime "hermetrix-harness/internal/tools"
)

// newAgentTestService builds a *Service against a temporary store, wired the
// same way skillManageFixture wires one, for tests that only need the
// service and its own cleanup rather than a ready-made session.
func newAgentTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	estimator := ctxcompiler.NewAdaptiveEstimator()
	compiler := ctxcompiler.NewCompiler(estimator, ctxcompiler.NewBlobSpiller(dataStore.Blobs), ctxcompiler.StructuredCompactor{})
	registry, err := toolruntime.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	skillService := skills.NewService(dataStore)
	service := NewService(dataStore, providers.NewService(dataStore, nil), compiler, estimator,
		runtime.NewInferenceGate(), registry, skillService)
	return service, func() { _ = dataStore.Close() }
}

func TestScopedToolsFollowsTheSessionProject(t *testing.T) {
	service, cleanup := newAgentTestService(t)
	defer cleanup()
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "marker.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectID := identity.New("prj")
	if _, err := service.store.DB.ExecContext(ctx,
		`INSERT INTO projects(id,name,root_path,state,created_at,updated_at)
		 VALUES(?,'scoped',?,'active',datetime('now'),datetime('now'))`, projectID, root); err != nil {
		t.Fatal(err)
	}
	scoped, err := service.scopedTools(ctx, Session{ID: "ses_1", ProjectID: projectID})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if scoped.Root() != resolved {
		t.Fatalf("scoped root = %q, want %q", scoped.Root(), resolved)
	}
}

func TestScopedToolsRefusesAProjectWithNoCodeFolder(t *testing.T) {
	service, cleanup := newAgentTestService(t)
	defer cleanup()
	ctx := context.Background()
	projectID := identity.New("prj")
	if _, err := service.store.DB.ExecContext(ctx,
		`INSERT INTO projects(id,name,root_path,state,created_at,updated_at)
		 VALUES(?,'rootless','','active',datetime('now'),datetime('now'))`, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.scopedTools(ctx, Session{ID: "ses_1", ProjectID: projectID}); !errors.Is(err, ErrSessionHasNoRoot) {
		t.Fatalf("err = %v, want ErrSessionHasNoRoot", err)
	}
	if _, err := service.scopedTools(ctx, Session{ID: "ses_2"}); !errors.Is(err, ErrSessionHasNoRoot) {
		t.Fatalf("session with no project: err = %v, want ErrSessionHasNoRoot", err)
	}
}

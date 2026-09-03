package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"hermetrix-harness/internal/identity"
)

func TestScopedToolsFollowsTheSessionProject(t *testing.T) {
	service, _ := skillManageFixture(t)
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
	service, _ := skillManageFixture(t)
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

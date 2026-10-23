package product

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotGuardExcludesServiceWritesAndRootChanges(t *testing.T) {
	s, _, _ := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sum.js"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := s.SaveProject(ctx, ProjectInput{Name: "snapshot", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.CaptureProjectSnapshot(ctx, p.ID, "", []string{"sum.js", "new.js"})
	if err != nil || snapshot.Hashes["new.js"] != "absent" {
		t.Fatalf("capture=%+v err=%v", snapshot, err)
	}
	release, err := s.GuardProjectSnapshot(ctx, p.ID, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if release != nil {
			release()
		}
	}()
	for _, operation := range []string{"single", "batch", "root"} {
		limited, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		switch operation {
		case "single":
			_, err = s.WriteProjectFile(limited, p.ID, WriteFileInput{Path: "sum.js", Content: "after", ExpectedSHA256: snapshot.Hashes["sum.js"], Actor: "owner"})
		case "batch":
			_, err = s.WriteProjectFiles(limited, p.ID, []WriteFileInput{{Path: "new.js", Content: "new", ExpectedSHA256: "absent", Actor: "owner"}})
		case "root":
			_, err = s.SaveProject(limited, ProjectInput{ID: p.ID, Name: "moved", RootPath: t.TempDir()})
		}
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s escaped snapshot guard: %v", operation, err)
		}
	}
	release()
	release = nil
	if _, err = s.WriteProjectFile(ctx, p.ID, WriteFileInput{Path: "sum.js", Content: "after", ExpectedSHA256: snapshot.Hashes["sum.js"], Actor: "owner"}); err != nil {
		t.Fatal(err)
	}
	if unlock, err := s.GuardProjectSnapshot(ctx, p.ID, snapshot); err == nil {
		unlock()
		t.Fatal("old snapshot accepted newer source")
	}
	if _, err = os.Stat(filepath.Join(root, "new.js")); !os.IsNotExist(err) {
		t.Fatal("snapshot or blocked batch created an absent output")
	}
}

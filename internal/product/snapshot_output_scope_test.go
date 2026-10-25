package product

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotOutputScopeGlobal(t *testing.T) {
	s, p, root := manifestScaleFixture(t, 0, 0)
	ctx := context.Background()

	// 512 distinct canonical root paths; absent, parent existing.
	paths := make([]string, 512)
	for i := range paths {
		paths[i] = fmt.Sprintf("output%03d.txt", i)
	}

	// Legacy 64 cap would have failed 512 cleanly; new cap permits it.
	snap, err := s.CaptureProjectSnapshot(ctx, p.ID, root, paths)
	if err != nil {
		t.Fatalf("CaptureProjectSnapshot(512): %v", err)
	}
	if len(snap.Hashes) != 512 {
		t.Fatalf("Hashes len = %d, want 512", len(snap.Hashes))
	}
	if snap.Root != root {
		t.Fatalf("Root = %q, want %q", snap.Root, root)
	}
	for i, pth := range paths {
		if got := snap.Hashes[pth]; got != "absent" {
			t.Fatalf("path %d (%s): hash = %q, want absent", i, pth, got)
		}
	}

	// GuardProjectSnapshot re-validates; returns unlock, then release.
	release, err := s.GuardProjectSnapshot(ctx, p.ID, snap)
	if err != nil {
		t.Fatalf("GuardProjectSnapshot: %v", err)
	}
	release()

	// No actual files created.
	for _, pth := range []string{paths[0], paths[511]} {
		if _, err := os.Stat(filepath.Join(root, pth)); !os.IsNotExist(err) {
			t.Fatalf("file %s should not exist", pth)
		}
	}

	// 513 paths: cap is never waived.
	over := append(append([]string{}, paths...), "output512.txt")
	if _, err := s.CaptureProjectSnapshot(ctx, p.ID, root, over); err == nil {
		t.Fatal("CaptureProjectSnapshot(513): want error, got nil")
	}
}

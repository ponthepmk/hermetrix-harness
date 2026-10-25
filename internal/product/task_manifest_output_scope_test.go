package product

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskManifestGlobalWritableScope(t *testing.T) {
	ctx := context.Background()
	successCases := []struct {
		name string
		n    int
	}{
		{"n33", 33},
		{"n512", 512},
	}
	for _, tc := range successCases {
		t.Run("success_"+tc.name, func(t *testing.T) {
			s, p, root := manifestScaleFixture(t, 0, 0)
			n := tc.n
			paths := make([]string, n)
			for i := range paths {
				paths[i] = fmt.Sprintf("z_trading/output%03d.txt", i)
			}
			m, err := s.ProjectTaskManifest(ctx, p.ID, "", paths, n)
			if err != nil {
				t.Fatalf("expected success, got: %v", err)
			}
			if len(m.Files) != n {
				t.Fatalf("expected %d files, got %d", n, len(m.Files))
			}
			if m.MetadataReads != n {
				t.Fatalf("expected MetadataReads=%d, got %d", n, m.MetadataReads)
			}
			seen := make(map[string]int, n)
			for _, f := range m.Files {
				if !f.Absent {
					t.Fatalf("path %q expected Absent=true", f.Path)
				}
				seen[f.Path]++
			}
			for i := range paths {
				if seen[paths[i]] != 1 {
					t.Fatalf("path %q expected exactly once, got %d", paths[i], seen[paths[i]])
				}
			}
			for _, idx := range []int{0, n - 1} {
				if _, statErr := os.Stat(filepath.Join(root, paths[idx])); !os.IsNotExist(statErr) {
					t.Fatalf("file %q must not exist on disk", paths[idx])
				}
			}
		})
	}
	rejectCases := []struct {
		name  string
		n     int
		limit int
	}{
		{"n513_limit2000_globalcap", 513, 2000},
		{"n33_limit32_manifestlimit", 33, 32},
	}
	for _, tc := range rejectCases {
		t.Run("reject_"+tc.name, func(t *testing.T) {
			s, p, _ := manifestScaleFixture(t, 0, 0)
			paths := make([]string, tc.n)
			for i := range paths {
				paths[i] = fmt.Sprintf("z_trading/output%03d.txt", i)
			}
			if _, err := s.ProjectTaskManifest(ctx, p.ID, "", paths, tc.limit); err == nil {
				t.Fatalf("expected rejection: %d paths with limit %d", tc.n, tc.limit)
			}
		})
	}
}

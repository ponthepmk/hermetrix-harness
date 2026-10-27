package product

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
)

func manifestScaleFixture(tb testing.TB, count int, bytes int) (*Service, Project, string) {
	tb.Helper()
	db, err := store.Open(context.Background(), tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = db.Close() })
	s := NewService(db, skills.NewService(db))
	tb.Cleanup(s.Close)
	root := tb.TempDir()
	payload := make([]byte, bytes)
	for i := range payload {
		payload[i] = 'a'
	}
	for i := 0; i < count; i++ {
		dir := filepath.Join(root, fmt.Sprintf("a_pkg%03d", i/64))
		if err = os.MkdirAll(dir, 0700); err != nil {
			tb.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, fmt.Sprintf("unrelated_%05d.go", i)), payload, 0600); err != nil {
			tb.Fatal(err)
		}
	}
	dir := filepath.Join(root, "z_trading")
	if err = os.MkdirAll(dir, 0700); err != nil {
		tb.Fatal(err)
	}
	for _, name := range []string{"risk_manager.mq5", "risk_manager_test.mqh"} {
		if err = os.WriteFile(filepath.Join(dir, name), []byte("// risk manager\n"), 0600); err != nil {
			tb.Fatal(err)
		}
	}
	p, err := s.SaveProject(context.Background(), ProjectInput{Name: "scale", RootPath: root})
	if err != nil {
		tb.Fatal(err)
	}
	return s, p, root
}

func TestLegacyManifestReproductionDropsLateRelevantSource(t *testing.T) {
	s, p, _ := manifestScaleFixture(t, 2048, 64)
	files, err := s.ProjectFileManifest(context.Background(), p.ID, 2000)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == "z_trading/risk_manager.mq5" {
			t.Fatal("reproduction changed: lexical prefix includes late source")
		}
	}
	scoped, err := legacyManifestForWrites(s, context.Background(), p.ID, []string{"z_trading/risk_manager.mq5"}, 512)
	if err != nil {
		t.Fatal(err)
	}
	rank := -1
	for i, f := range scoped {
		if f.Path == "z_trading/risk_manager.mq5" {
			rank = i
		}
	}
	t.Logf("before: %d-file lexical list omits target; explicitly writable target at planner rank %d of %d", len(files), rank, len(scoped))
	current, err := s.ProjectFileManifestForWrites(context.Background(), p.ID, []string{"z_trading/risk_manager.mq5"}, 512)
	if err != nil || len(current) == 0 || current[0].Path != "z_trading/risk_manager.mq5" {
		t.Fatalf("writable compatibility entry point lost its first pin: %+v err=%v", current, err)
	}
}

// Baseline measures the pre-change prefix discovery and per-file hashing path.
// It excludes planner JSON fitting and does not call a model.
func BenchmarkPlannerManifestBaseline(b *testing.B) {
	s, p, _ := manifestScaleFixture(b, 4096, 32<<10)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		files, err := legacyManifestForWrites(s, context.Background(), p.ID, []string{"z_trading/risk_manager.mq5"}, 512)
		if err != nil {
			b.Fatal(err)
		}
		read := 0
		for _, f := range files {
			doc, err := s.ReadProjectFile(context.Background(), p.ID, f.Path)
			if err != nil {
				b.Fatal(err)
			}
			read += doc.Bytes
		}
		b.ReportMetric(float64(read), "snapshot-bytes/op")
	}
}

func TestTaskManifestFindsLateSourceRetainsExactPinsAndReportsPartialView(t *testing.T) {
	s, p, root := manifestScaleFixture(t, 2048, 64)
	ctx := context.Background()
	m, err := s.ProjectTaskManifest(ctx, p.ID, "repair risk manager backtest", nil, 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 32 || !m.Truncated || m.Files[0].Path != "z_trading/risk_manager.mq5" || m.Files[1].Path != "z_trading/risk_manager_test.mqh" {
		t.Fatalf("ranked manifest=%+v", m)
	}
	if m.MetadataReads >= m.SourcePathsSeen/2 {
		t.Fatalf("low relevance candidates incurred full stat cost: %+v", m)
	}
	again, err := s.ProjectTaskManifest(ctx, p.ID, "repair risk manager backtest", nil, 32)
	if err != nil || !reflect.DeepEqual(m, again) {
		t.Fatalf("nondeterministic manifest: err=%v", err)
	}
	pinned, err := s.ProjectTaskManifest(ctx, p.ID, "unrelated_00000.go", []string{"z_trading/new.mq5", "z_trading/risk_manager.mq5"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned.Files) != 2 || pinned.Files[0].Path != "z_trading/new.mq5" || !pinned.Files[0].Absent || pinned.Files[1].Path != "z_trading/risk_manager.mq5" {
		t.Fatalf("discovery displaced exact writable scope: %+v", pinned)
	}
	if _, err = os.Stat(filepath.Join(root, "z_trading", "new.mq5")); !os.IsNotExist(err) {
		t.Fatal("manifest created an absent file")
	}
	// A fresh request observes external files; no TTL cache can hide them.
	if err = os.WriteFile(filepath.Join(root, "z_trading", "risk_manager_report.mqh"), []byte("// new source"), 0600); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.ProjectTaskManifest(ctx, p.ID, "risk manager report", nil, 3)
	if err != nil || fresh.Files[0].Path != "z_trading/risk_manager_report.mqh" {
		t.Fatalf("new source was stale: %+v err=%v", fresh, err)
	}
	if err = os.Remove(filepath.Join(root, "z_trading", "risk_manager_report.mqh")); err != nil {
		t.Fatal(err)
	}
	fresh, err = s.ProjectTaskManifest(ctx, p.ID, "risk manager report", nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fresh.Files {
		if strings.Contains(f.Path, "_report") {
			t.Fatal("removed source remained cached")
		}
	}
}

func TestTaskManifestPreservesFiltersCancellationAndCanonicalRoot(t *testing.T) {
	s, p, root := manifestScaleFixture(t, 4, 64)
	for _, path := range []string{"credentials.json", ".env.json", "node_modules/p/risk_manager.mq5", ".hidden/risk_manager.go"} {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte("private"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.go"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside.go"), filepath.Join(root, "risk_manager_link.go")); err != nil {
		t.Fatal(err)
	}
	m, err := s.ProjectTaskManifest(context.Background(), p.ID, "credentials env risk manager link", nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range m.Files {
		if strings.Contains(f.Path, "credentials") || strings.Contains(f.Path, ".env") || strings.Contains(f.Path, "node_modules") || strings.Contains(f.Path, ".hidden") || strings.Contains(f.Path, "_link") {
			t.Fatalf("excluded path crossed discovery: %s", f.Path)
		}
	}
	for _, path := range []string{"credentials.json", "node_modules/p/risk_manager.mq5", "../outside.go", "missing/new.go", "risk_manager_link.go"} {
		if _, err := s.ProjectTaskManifest(context.Background(), p.ID, "", []string{path}, 10); err == nil {
			t.Fatalf("invalid writable path accepted: %s", path)
		}
	}
	if runtime.GOOS == "windows" {
		if _, err := s.ProjectTaskManifest(context.Background(), p.ID, "", []string{"z_trading/risk_manager.mq5", "z_trading/RISK_MANAGER.mq5"}, 10); err == nil {
			t.Fatal("same physical Windows source was pinned twice")
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.ProjectTaskManifest(canceled, p.ID, "risk", nil, 10); err == nil {
		t.Fatal("canceled discovery returned a partial success")
	}
	if _, err = s.SaveProject(context.Background(), ProjectInput{ID: p.ID, Name: "moved", RootPath: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.HashTaskManifest(context.Background(), p.ID, m, 1024); err == nil {
		t.Fatal("hashing followed a redirected project root")
	}
}

func TestTaskManifestHashBudgetIsHardEvenWhenSourceGrows(t *testing.T) {
	s, p, root := manifestScaleFixture(t, 4, 4096)
	m, err := s.ProjectTaskManifest(context.Background(), p.ID, "risk", []string{"z_trading/new.mq5"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	hashes, err := s.HashTaskManifest(context.Background(), p.ID, m, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if !hashes.Truncated || hashes.BytesRead > 8193 || hashes.Hashes["z_trading/new.mq5"] != "absent" || hashes.Hashes["z_trading/risk_manager.mq5"] == "" {
		t.Fatalf("bounded hashes=%+v", hashes)
	}
	// Metadata can become stale between discovery and read. The reader must
	// stop at the budget sentinel, rather than trusting the old small size.
	path := "a_pkg000/unrelated_00000.go"
	if err = os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(strings.Repeat("x", 3<<20)), 0600); err != nil {
		t.Fatal(err)
	}
	m.Files = []FileEntry{{Path: path, Bytes: 1}}
	hashes, err = s.HashTaskManifest(context.Background(), p.ID, m, 1024)
	if err != nil || hashes.BytesRead != 1025 || !hashes.Truncated || len(hashes.Hashes) != 0 {
		t.Fatalf("growing source escaped read budget: %+v err=%v", hashes, err)
	}
}

func BenchmarkPlannerManifestRanked(b *testing.B) {
	s, p, _ := manifestScaleFixture(b, 4096, 32<<10)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, err := s.ProjectTaskManifest(context.Background(), p.ID, "repair risk manager backtest", []string{"z_trading/risk_manager.mq5"}, 512)
		if err != nil {
			b.Fatal(err)
		}
		hashes, err := s.HashTaskManifest(context.Background(), p.ID, m, MaxPlannerSnapshotBytes)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(hashes.BytesRead), "snapshot-bytes/op")
		b.ReportMetric(float64(m.MetadataReads), "file-stats/op")
	}
}

// Preserved before-change algorithm for reproduction and a comparable baseline.
func legacyManifestForWrites(s *Service, ctx context.Context, projectID string, writable []string, limit int) ([]FileEntry, error) {
	if limit < 1 || limit > 2000 {
		limit = 2000
	}
	if len(writable) > 32 || len(writable) > limit {
		return nil, fmt.Errorf("writable file manifest exceeds its bounded scope")
	}
	items, err := s.ProjectFileManifest(ctx, projectID, limit)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	priority := map[string]bool{}
	for _, item := range items {
		seen[item.Path] = true
	}
	for _, relative := range writable {
		if !sourceManifestPathAllowed(relative) {
			return nil, fmt.Errorf("writable path %q is excluded by source/secret discovery policy", relative)
		}
		priority[relative] = true
		if seen[relative] {
			continue
		}
		doc, readErr := s.SnapshotProjectFile(ctx, projectID, relative)
		if readErr != nil {
			return nil, readErr
		}
		items = append(items, FileEntry{Name: filepath.Base(relative), Path: relative, Bytes: int64(doc.Bytes), ModifiedAt: doc.ModifiedAt, Absent: doc.SHA256 == "absent"})
		seen[relative] = true
	}
	sort.Slice(items, func(i, j int) bool {
		if priority[items[i].Path] != priority[items[j].Path] {
			return priority[items[i].Path]
		}
		return items[i].Path < items[j].Path
	})
	if len(items) > limit {
		items = items[:limit]
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, nil
}

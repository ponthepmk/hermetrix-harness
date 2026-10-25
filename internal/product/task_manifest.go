package product

import (
	"container/heap"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"

	"hermetrix-harness/internal/taskscope"
)

const maxTaskManifestEntries = 100000
const MaxPlannerSnapshotBytes = 8 << 20

func manifestPathKey(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

type TaskManifest struct {
	Files           []FileEntry
	Root            string
	Truncated       bool
	VisitedEntries  int
	SourcePathsSeen int
	MetadataReads   int
}

type rankedManifestFile struct {
	file  FileEntry
	score int
}
type manifestHeap []rankedManifestFile

func (h manifestHeap) Len() int           { return len(h) }
func (h manifestHeap) Less(i, j int) bool { return betterManifestFile(h[j], h[i]) }
func (h manifestHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *manifestHeap) Push(x any)        { *h = append(*h, x.(rankedManifestFile)) }
func (h *manifestHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
func betterManifestFile(a, b rankedManifestFile) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	return a.file.Path < b.file.Path
}

func manifestWords(value string) []string {
	runes := []rune(value)
	var split strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]) || unicode.IsUpper(runes[i-1]) && i+1 < len(runes) && unicode.IsLower(runes[i+1])) {
			split.WriteByte(' ')
		}
		split.WriteRune(unicode.ToLower(r))
	}
	return strings.FieldsFunc(split.String(), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// ProjectTaskManifest ranks paths across a fresh bounded walk, rather than
// sending a lexical prefix. Exact writable leaves are inspected first and never
// displaced by discovery. Only promising heap candidates incur file stat calls;
// this operation reads no source bytes and maintains no stale filesystem cache.
func (s *Service) ProjectTaskManifest(ctx context.Context, projectID, query string, writable []string, limit int) (TaskManifest, error) {
	result := TaskManifest{}
	if limit < 1 || limit > 2000 {
		limit = 2000
	}
	if len(writable) > limit {
		return result, fmt.Errorf("task manifest writable scope exceeds bounded limits")
	}
	if err := taskscope.ValidateWritablePaths(writable); err != nil {
		return result, err
	}
	root, err := s.ProjectRootIdentity(ctx, projectID)
	if err != nil {
		return result, err
	}
	result.Root = root
	seen := map[string]bool{}
	parents := map[string]bool{}
	for _, p := range writable {
		if len(p) > 512 || !sourceManifestPathAllowed(p) || seen[manifestPathKey(p)] {
			return result, fmt.Errorf("invalid, duplicate or excluded writable source path %q", p)
		}
		file, err := taskManifestMetadata(root, p)
		if err != nil {
			return result, err
		}
		result.MetadataReads++
		result.Files = append(result.Files, file)
		seen[manifestPathKey(p)] = true
		parents[manifestPathKey(filepath.ToSlash(filepath.Dir(p)))] = true
	}
	// Authorization order does not affect deterministic ranking among exact pins.
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	if len(query) > 16<<10 {
		query = query[:16<<10]
	}
	terms := map[string]bool{}
	for _, word := range manifestWords(query) {
		if len([]rune(word)) >= 2 {
			terms[word] = true
		}
		if len(terms) == 64 {
			break
		}
	}
	lowerQuery := strings.ToLower(query)
	score := func(p string) int {
		n := 0
		if parents[manifestPathKey(filepath.ToSlash(filepath.Dir(p)))] {
			n += 500
		}
		if strings.Contains(lowerQuery, strings.ToLower(p)) {
			n += 10000
		}
		for _, word := range manifestWords(p) {
			if terms[word] {
				n += 2
			}
		}
		for _, word := range manifestWords(filepath.Base(p)) {
			if terms[word] {
				n += 8
			}
		}
		switch strings.ToLower(p) {
		case "go.mod", "package.json", "cargo.toml", "pyproject.toml", "makefile", "dockerfile":
			n += 4
		}
		return n
	}
	capacity := limit - len(result.Files)
	candidates := &manifestHeap{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			result.Truncated = true
			return nil
		}
		if path == root {
			return nil
		}
		result.VisitedEntries++
		if result.VisitedEntries > maxTaskManifestEntries {
			result.Truncated = true
			return filepath.SkipAll
		}
		name := entry.Name()
		if entry.IsDir() {
			lower := strings.ToLower(name)
			if skippedManifestDirectories[lower] || strings.HasPrefix(lower, ".") && lower != ".github" || sensitiveManifestName(lower) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 || sensitiveManifestName(name) || !sourceLikeManifestName(name) {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if len(relative) > 512 || !sourceManifestPathAllowed(relative) {
			return nil
		}
		result.SourcePathsSeen++
		if seen[manifestPathKey(relative)] {
			return nil
		}
		candidate := rankedManifestFile{file: FileEntry{Name: name, Path: relative}, score: score(relative)}
		if capacity == 0 || candidates.Len() == capacity && !betterManifestFile(candidate, (*candidates)[0]) {
			result.Truncated = true
			return nil
		}
		result.MetadataReads++
		info, err := entry.Info()
		if err != nil {
			result.Truncated = true
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > maxManifestFileBytes {
			return nil
		}
		candidate.file.Bytes = info.Size()
		candidate.file.ModifiedAt = info.ModTime().UTC()
		if candidates.Len() == capacity {
			heap.Pop(candidates)
			result.Truncated = true
		}
		heap.Push(candidates, candidate)
		return nil
	})
	if err != nil {
		return TaskManifest{}, err
	}
	ranked := []rankedManifestFile(*candidates)
	sort.Slice(ranked, func(i, j int) bool { return betterManifestFile(ranked[i], ranked[j]) })
	for _, item := range ranked {
		result.Files = append(result.Files, item.file)
	}
	return result, nil
}

func taskManifestMetadata(root, relative string) (FileEntry, error) {
	parent, err := resolveInside(root, filepath.Dir(filepath.FromSlash(relative)), true)
	if err != nil {
		return FileEntry{}, fmt.Errorf("writable source requires existing owned parent: %w", err)
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return FileEntry{}, fmt.Errorf("writable source parent is not a directory")
	}
	file := FileEntry{Name: filepath.Base(relative), Path: relative}
	info, err = os.Lstat(filepath.Join(parent, filepath.Base(relative)))
	if os.IsNotExist(err) {
		file.Absent = true
		return file, nil
	}
	if err != nil {
		return file, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxManifestFileBytes {
		return file, fmt.Errorf("writable source must be a bounded ordinary file")
	}
	file.Bytes = info.Size()
	file.ModifiedAt = info.ModTime().UTC()
	return file, nil
}

type ManifestHashes struct {
	Hashes    map[string]string
	BytesRead int
	Truncated bool
}

// HashTaskManifest reads only the already-fitted planner files, bound to the
// captured root and a total byte budget. One overflow sentinel byte is allowed.
func (s *Service) HashTaskManifest(ctx context.Context, projectID string, manifest TaskManifest, budget int) (ManifestHashes, error) {
	out := ManifestHashes{Hashes: map[string]string{}}
	if budget < 1 || budget > MaxPlannerSnapshotBytes || manifest.Root == "" || len(manifest.Files) > 512 {
		return out, fmt.Errorf("manifest hash budget/root is invalid")
	}
	root, err := s.ProjectRootIdentity(ctx, projectID)
	if err != nil {
		return out, err
	}
	if root != manifest.Root {
		return out, fmt.Errorf("project root changed during manifest hashing")
	}
	for _, file := range manifest.Files {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		if file.Absent {
			current, err := taskManifestMetadata(root, file.Path)
			if err != nil {
				return out, err
			}
			if !current.Absent {
				return out, fmt.Errorf("absent manifest source appeared before planning")
			}
			out.Hashes[file.Path] = "absent"
			continue
		}
		remaining := budget - out.BytesRead
		if remaining <= 0 || file.Bytes > int64(remaining) {
			out.Truncated = true
			continue
		}
		document, err := readProjectFileAtRootBounded(root, file.Path, min(remaining, maxWorkbenchFileBytes))
		out.BytesRead += document.Bytes
		if err != nil {
			out.Truncated = true
			continue
		}
		out.Hashes[file.Path] = document.SHA256
	}
	return out, nil
}

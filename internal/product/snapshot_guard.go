package product

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectSnapshot binds known task outputs to exact observed bytes. It is not
// a claim about every dependency or future external edit in the filesystem.
type ProjectSnapshot struct {
	Root   string            `json:"root"`
	Hashes map[string]string `json:"hashes"`
}

func (s *Service) lockSnapshotPaths(ctx context.Context, projectID, expectedRoot string, paths []string) (string, func(), error) {
	if len(paths) > 64 {
		return "", nil, fmt.Errorf("snapshot exceeds 64 source paths")
	}
	rootUnlock, err := s.guardProjectRoot(ctx, projectID, expectedRoot)
	if err != nil {
		return "", nil, err
	}
	releases := []func(){rootUnlock}
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	root, err := s.ProjectRootIdentity(ctx, projectID)
	if err != nil {
		release()
		return "", nil, err
	}
	if expectedRoot != "" && root != expectedRoot {
		release()
		return "", nil, fmt.Errorf("project root changed from frozen snapshot")
	}
	keys := map[string]bool{}
	for _, p := range paths {
		clean := filepath.Clean(filepath.FromSlash(p))
		if p == "" || clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			release()
			return "", nil, fmt.Errorf("snapshot path must stay inside project")
		}
		keys[manifestPathKey(root+"\x00"+filepath.ToSlash(clean))] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		unlock, err := s.pathLocks.acquire(ctx, key)
		if err != nil {
			release()
			return "", nil, err
		}
		releases = append(releases, unlock)
	}
	return root, release, nil
}

// CaptureProjectSnapshot observes exact known paths while excluding writes
// through this service. No locks survive a model request or command execution.
func (s *Service) CaptureProjectSnapshot(ctx context.Context, projectID, expectedRoot string, paths []string) (ProjectSnapshot, error) {
	root, release, err := s.lockSnapshotPaths(ctx, projectID, expectedRoot, paths)
	if err != nil {
		return ProjectSnapshot{}, err
	}
	defer release()
	snapshot := ProjectSnapshot{Root: root, Hashes: map[string]string{}}
	for _, p := range paths {
		if err = ctx.Err(); err != nil {
			return ProjectSnapshot{}, err
		}
		document, err := snapshotProjectFileAtRoot(root, p)
		if err != nil {
			return ProjectSnapshot{}, err
		}
		snapshot.Hashes[document.Path] = document.SHA256
	}
	return snapshot, nil
}

// GuardProjectSnapshot rechecks a saved snapshot and holds root/path guards
// through a short evidence commit. Callers must release it; callbacks must not
// perform project writes or recursively acquire these same guards.
func (s *Service) GuardProjectSnapshot(ctx context.Context, projectID string, snapshot ProjectSnapshot) (func(), error) {
	if snapshot.Root == "" || snapshot.Hashes == nil {
		return nil, fmt.Errorf("saved source snapshot is missing")
	}
	paths := make([]string, 0, len(snapshot.Hashes))
	for p, sha := range snapshot.Hashes {
		if sha != "absent" && !validSHA256(sha) {
			return nil, fmt.Errorf("saved source snapshot has invalid digest")
		}
		paths = append(paths, p)
	}
	root, release, err := s.lockSnapshotPaths(ctx, projectID, snapshot.Root, paths)
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		if err = ctx.Err(); err != nil {
			release()
			return nil, err
		}
		document, err := snapshotProjectFileAtRoot(root, p)
		if err != nil {
			release()
			return nil, fmt.Errorf("source snapshot unavailable for %s: %w", p, err)
		}
		if document.SHA256 != snapshot.Hashes[p] {
			release()
			return nil, &PreimageChangedError{Path: p, ExpectedSHA256: snapshot.Hashes[p], CurrentSHA256: document.SHA256}
		}
	}
	return release, nil
}

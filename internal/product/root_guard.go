package product

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

func (s *Service) ProjectRootIdentity(ctx context.Context, projectID string) (string, error) {
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	root, err := requireRoot(project)
	if err != nil {
		return "", err
	}
	return canonicalProjectRoot(root)
}

func canonicalProjectRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		canonical = strings.ToLower(canonical)
	}
	return canonical, nil
}

// Bind one captured root to the frozen authority and use that canonical path
// for the whole operation. A later database edit cannot redirect its I/O.
func bindProjectRoot(root, expected string) (string, error) {
	canonical, err := canonicalProjectRoot(root)
	if err != nil {
		return "", err
	}
	if expected != "" && canonical != expected {
		return "", fmt.Errorf("project root changed from the frozen proposal scope")
	}
	return canonical, nil
}

func (s *Service) guardProjectRoot(ctx context.Context, projectID, expected string) (func(), error) {
	unlock, err := s.pathLocks.acquire(ctx, "project-root:\x00"+projectID)
	if err != nil {
		return nil, err
	}
	if expected != "" {
		current, readErr := s.ProjectRootIdentity(ctx, projectID)
		if readErr != nil || current != expected {
			unlock()
			return nil, fmt.Errorf("project root changed from the frozen proposal scope")
		}
	}
	return unlock, nil
}

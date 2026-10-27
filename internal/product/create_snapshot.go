package product

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SnapshotProjectFile distinguishes a missing leaf from an existing empty file.
// The parent must already be an owned, reachable directory inside the project.
// This is a read operation: it never prepares the workspace by creating files.
func (s *Service) SnapshotProjectFile(ctx context.Context, projectID, relative string) (FileDocument, error) {
	return s.SnapshotProjectFileAtRoot(ctx, projectID, "", relative)
}

func (s *Service) SnapshotProjectFileAtRoot(ctx context.Context, projectID, expectedRoot, relative string) (FileDocument, error) {
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return FileDocument{}, err
	}
	root, err := requireRoot(project)
	if err != nil {
		return FileDocument{}, err
	}
	root, err = bindProjectRoot(root, expectedRoot)
	if err != nil {
		return FileDocument{}, err
	}
	return snapshotProjectFileAtRoot(root, relative)
}

func snapshotProjectFileAtRoot(root, relative string) (FileDocument, error) {
	clean := filepath.Clean(strings.TrimSpace(relative))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return FileDocument{}, fmt.Errorf("file snapshot must stay inside project")
	}
	parent, err := resolveInside(root, filepath.Dir(clean), true)
	if err != nil {
		return FileDocument{}, fmt.Errorf("file snapshot requires an existing owned parent: %w", err)
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return FileDocument{}, fmt.Errorf("file snapshot parent is not a directory")
	}
	if _, err = os.Lstat(filepath.Join(parent, filepath.Base(clean))); os.IsNotExist(err) {
		return FileDocument{Path: filepath.ToSlash(clean), SHA256: "absent"}, nil
	} else if err != nil {
		return FileDocument{}, err
	}
	return readProjectFileAtRoot(root, clean)
}

func sourceManifestPathAllowed(relative string) bool {
	if relative == "" || filepath.IsAbs(relative) || strings.ContainsAny(relative, "\\:\r\n\x00") {
		return false
	}
	parts := strings.Split(relative, "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." || sensitiveManifestName(part) {
			return false
		}
		if i < len(parts)-1 && (skippedManifestDirectories[strings.ToLower(part)] || strings.HasPrefix(part, ".") && part != ".github") {
			return false
		}
	}
	return sourceLikeManifestName(parts[len(parts)-1])
}

// ProjectFileManifestForWrites prioritizes the owner's exact writable paths,
// including host-observed absent leaves, while retaining the same secret/source
// filters as discovery. A model can never invent its own creation candidates.
func (s *Service) ProjectFileManifestForWrites(ctx context.Context, projectID string, writable []string, limit int) ([]FileEntry, error) {
	manifest, err := s.ProjectTaskManifest(ctx, projectID, "", writable, limit)
	return manifest.Files, err
}

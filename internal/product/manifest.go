package product

import (
	"context"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

const maxManifestFileBytes = 2 << 20

var skippedManifestDirectories = map[string]bool{
	".git": true, ".hermetrix": true, ".next": true, ".cache": true,
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"coverage": true, "tmp": true,
}

// ProjectFileManifest returns a deterministic, bounded list of ordinary
// source-like files. It deliberately does not follow symlinks and excludes
// common credential and generated paths before any path can cross a provider
// boundary. File contents are not read by this operation.
func (s *Service) ProjectFileManifest(ctx context.Context, projectID string, limit int) ([]FileEntry, error) {
	if limit <= 0 || limit > 2000 {
		limit = 2000
	}
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	root, err := requireRoot(project)
	if err != nil {
		return nil, err
	}
	items := make([]FileEntry, 0, min(limit, 256))
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // unreadable paths are not candidates
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			lowerName := strings.ToLower(name)
			if skippedManifestDirectories[lowerName] || (strings.HasPrefix(lowerName, ".") && lowerName != ".github") || sensitiveManifestName(lowerName) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 || len(items) >= limit || sensitiveManifestName(name) || !sourceLikeManifestName(name) {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() || info.Size() > maxManifestFileBytes {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil
		}
		for _, component := range strings.Split(filepath.ToSlash(relative), "/") {
			if sensitiveManifestName(component) {
				return nil
			}
		}
		items = append(items, FileEntry{Name: name, Path: filepath.ToSlash(relative), Bytes: info.Size(), ModifiedAt: info.ModTime().UTC()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, nil
}

func sensitiveManifestName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" || strings.HasPrefix(lower, ".env") || strings.HasSuffix(lower, ".pem") ||
		strings.HasSuffix(lower, ".key") || strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx") {
		return true
	}
	for _, marker := range []string{"secret", "credential", "id_rsa", "id_ed25519", "access_token", "api_key"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func sourceLikeManifestName(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "dockerfile", "makefile", "procfile", "gemfile", "go.mod", "go.sum", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock":
		return true
	}
	switch strings.ToLower(filepath.Ext(lower)) {
	case ".go", ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".json", ".yaml", ".yml", ".toml",
		".md", ".txt", ".py", ".php", ".sql", ".html", ".css", ".scss", ".less", ".sh", ".ps1",
		".xml", ".proto", ".graphql", ".gql", ".java", ".kt", ".kts", ".rs", ".c", ".h", ".cpp", ".hpp",
		".cs", ".rb", ".swift", ".dart", ".vue", ".svelte":
		return true
	default:
		return false
	}
}

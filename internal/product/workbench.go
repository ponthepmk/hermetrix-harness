package product

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const maxWorkbenchFileBytes = 2 << 20

func (s *Service) ReadProjectFile(ctx context.Context, projectID, relative string) (FileDocument, error) {
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return FileDocument{}, err
	}
	path, clean, err := regularProjectFile(project.RootPath, relative)
	if err != nil {
		return FileDocument{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return FileDocument{}, err
	}
	if len(data) > maxWorkbenchFileBytes {
		return FileDocument{}, fmt.Errorf("workbench editor supports files up to 2 MiB")
	}
	if !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return FileDocument{}, fmt.Errorf("workbench editor supports UTF-8 text files only")
	}
	info, err := os.Stat(path)
	if err != nil {
		return FileDocument{}, err
	}
	return fileDocument(clean, data, info), nil
}

func (s *Service) WriteProjectFile(ctx context.Context, projectID string, input WriteFileInput) (WriteFileResult, error) {
	input.Actor = strings.TrimSpace(input.Actor)
	if input.Actor == "" || len(input.Content) > maxWorkbenchFileBytes || !utf8.ValidString(input.Content) || strings.IndexByte(input.Content, 0) >= 0 {
		return WriteFileResult{}, fmt.Errorf("actor and UTF-8 content up to 2 MiB are required")
	}
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return WriteFileResult{}, err
	}
	clean := filepath.Clean(strings.TrimSpace(input.Path))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return WriteFileResult{}, fmt.Errorf("file path must stay inside the project root")
	}
	parent, err := resolveInside(project.RootPath, filepath.Dir(clean), true)
	if err != nil {
		return WriteFileResult{}, err
	}
	path := filepath.Join(parent, filepath.Base(clean))
	if rel, relErr := filepath.Rel(project.RootPath, path); relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return WriteFileResult{}, fmt.Errorf("file path escapes project root")
	}
	var before []byte
	mode := os.FileMode(0o644)
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return WriteFileResult{}, fmt.Errorf("workbench editor refuses symlinks and non-regular files")
		}
		before, err = os.ReadFile(path)
		if err != nil {
			return WriteFileResult{}, err
		}
		if len(before) > maxWorkbenchFileBytes || !utf8.Valid(before) {
			return WriteFileResult{}, fmt.Errorf("existing file is not bounded UTF-8 text")
		}
		mode = info.Mode().Perm()
		if input.ExpectedSHA256 == "" || input.ExpectedSHA256 != hashBytes(before) {
			return WriteFileResult{}, fmt.Errorf("file changed since it was opened; reload before saving")
		}
	} else if !os.IsNotExist(statErr) {
		return WriteFileResult{}, statErr
	} else if input.ExpectedSHA256 != "" {
		return WriteFileResult{}, fmt.Errorf("new file must not include an expected hash")
	}
	after := []byte(input.Content)
	diff := boundedTextDiff(filepath.ToSlash(clean), string(before), input.Content)
	temp, err := os.CreateTemp(parent, ".hermetrix-write-*")
	if err != nil {
		return WriteFileResult{}, err
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(mode); err != nil {
		return WriteFileResult{}, err
	}
	if _, err := temp.Write(after); err != nil {
		return WriteFileResult{}, err
	}
	if err := temp.Sync(); err != nil {
		return WriteFileResult{}, err
	}
	if err := temp.Close(); err != nil {
		return WriteFileResult{}, err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return WriteFileResult{}, err
	}
	committed = true
	info, err := os.Stat(path)
	if err != nil {
		return WriteFileResult{}, err
	}
	document := fileDocument(filepath.ToSlash(clean), after, info)
	receipt, err := s.CreateArtifact(ctx, ArtifactInput{ProjectID: project.ID, Name: filepath.Base(clean) + ".diff",
		Kind: "workbench_diff", MIMEType: "text/x-diff; charset=utf-8", Content: diff,
		Metadata: map[string]any{"actor": input.Actor, "path": filepath.ToSlash(clean), "before_sha256": hashBytes(before),
			"after_sha256": document.SHA256, "committed_at": time.Now().UTC().Format(time.RFC3339Nano)}})
	if err != nil {
		return WriteFileResult{Document: document, BeforeSHA256: hashBytes(before), Diff: diff},
			fmt.Errorf("file saved but audit receipt failed: %w", err)
	}
	return WriteFileResult{Document: document, BeforeSHA256: hashBytes(before), Diff: diff, ReceiptArtifact: receipt}, nil
}

func regularProjectFile(root, relative string) (string, string, error) {
	clean := filepath.Clean(strings.TrimSpace(relative))
	path, err := resolveInside(root, clean, true)
	if err != nil {
		return "", "", err
	}
	leaf := filepath.Join(root, clean)
	info, err := os.Lstat(leaf)
	if err != nil {
		return "", "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("workbench editor refuses symlinks and non-regular files")
	}
	return path, filepath.ToSlash(clean), nil
}

func fileDocument(path string, data []byte, info os.FileInfo) FileDocument {
	return FileDocument{Path: path, Content: string(data), SHA256: hashBytes(data), Bytes: len(data),
		Mode: info.Mode().Perm().String(), ModifiedAt: info.ModTime().UTC()}
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func boundedTextDiff(path, before, after string) string {
	if before == after {
		return "--- a/" + path + "\n+++ b/" + path + "\n(no changes)\n"
	}
	const preview = 24000
	clip := func(value string) string {
		if len(value) <= preview {
			return value
		}
		return value[:preview] + "\n… diff preview clipped by Hermetrix …\n"
	}
	minus := strings.ReplaceAll(clip(before), "\n", "\n-")
	plus := strings.ReplaceAll(clip(after), "\n", "\n+")
	return "--- a/" + path + "\n+++ b/" + path + "\n@@ full bounded preview @@\n-" + minus + "\n+" + plus + "\n"
}

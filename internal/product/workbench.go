package product

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	root, err := requireRoot(project)
	if err != nil {
		return FileDocument{}, err
	}
	path, clean, err := regularProjectFile(root, relative)
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
	root, err := requireRoot(project)
	if err != nil {
		return WriteFileResult{}, err
	}
	clean := filepath.Clean(strings.TrimSpace(input.Path))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return WriteFileResult{}, fmt.Errorf("file path must stay inside the project root")
	}
	parent, err := resolveInside(root, filepath.Dir(clean), true)
	if err != nil {
		return WriteFileResult{}, err
	}
	path := filepath.Join(parent, filepath.Base(clean))
	if rel, relErr := filepath.Rel(root, path); relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return WriteFileResult{}, fmt.Errorf("file path escapes project root")
	}
	lockRoot := root
	if canonical, canonicalErr := filepath.EvalSymlinks(root); canonicalErr == nil {
		lockRoot = canonical
	}
	release, err := s.pathLocks.acquire(ctx, lockRoot+"\x00"+filepath.ToSlash(clean))
	if err != nil {
		return WriteFileResult{}, err
	}
	defer release()
	return s.writeProjectFileLocked(ctx, project, root, clean, input)
}

func (s *Service) writeProjectFileLocked(ctx context.Context, project Project, root, clean string, input WriteFileInput) (WriteFileResult, error) {
	// Resolve again while holding the path lock. A parent symlink may have
	// changed while this writer waited; the compare-and-write must target the
	// same canonical project path used to serialize it.
	parent, err := resolveInside(root, filepath.Dir(clean), true)
	if err != nil {
		return WriteFileResult{}, err
	}
	path := filepath.Join(parent, filepath.Base(clean))
	if rel, relErr := filepath.Rel(root, path); relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return WriteFileResult{}, fmt.Errorf("file path escapes project root")
	}
	var before []byte
	beforeSHA := "absent"
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
		beforeSHA = hashBytes(before)
		if !validSHA256(input.ExpectedSHA256) {
			return WriteFileResult{}, fmt.Errorf("existing file writes require a lowercase SHA-256 preimage")
		}
		if input.ExpectedSHA256 != beforeSHA {
			return WriteFileResult{}, &PreimageChangedError{Path: filepath.ToSlash(clean),
				ExpectedSHA256: input.ExpectedSHA256, CurrentSHA256: beforeSHA}
		}
	} else if !os.IsNotExist(statErr) {
		return WriteFileResult{}, statErr
	} else if input.ExpectedSHA256 != "" && input.ExpectedSHA256 != "absent" {
		return WriteFileResult{}, fmt.Errorf("new file writes require expected_sha256=absent")
	}
	after := []byte(input.Content)
	afterSHA := hashBytes(after)
	diff := boundedTextDiff(filepath.ToSlash(clean), string(before), input.Content)
	intent, err := s.createFileMutationIntent(ctx, project.ID, filepath.ToSlash(clean), input.Actor, beforeSHA, afterSHA)
	if err != nil {
		return WriteFileResult{}, err
	}
	abandon := func(message string) {
		_ = s.transitionFileMutation(context.WithoutCancel(ctx), intent.OperationID, mutationPlanned, mutationAbandoned, "", message)
	}
	temp, err := os.CreateTemp(parent, ".hermetrix-write-*")
	if err != nil {
		abandon("temporary file creation failed")
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
		abandon("temporary file permission failed")
		return WriteFileResult{}, err
	}
	if _, err := temp.Write(after); err != nil {
		abandon("temporary file write failed")
		return WriteFileResult{}, err
	}
	if err := temp.Sync(); err != nil {
		abandon("temporary file sync failed")
		return WriteFileResult{}, err
	}
	if err := temp.Close(); err != nil {
		abandon("temporary file close failed")
		return WriteFileResult{}, err
	}
	currentSHA := "absent"
	if current, readErr := os.ReadFile(path); readErr == nil {
		currentSHA = hashBytes(current)
	} else if !os.IsNotExist(readErr) {
		abandon("final preimage read failed")
		return WriteFileResult{}, readErr
	}
	if currentSHA != beforeSHA {
		abandon("final preimage changed before replacement")
		return WriteFileResult{}, &PreimageChangedError{Path: filepath.ToSlash(clean),
			ExpectedSHA256: beforeSHA, CurrentSHA256: currentSHA}
	}
	if err = s.transitionFileMutation(ctx, intent.OperationID, mutationPlanned, mutationDispatched, "", ""); err != nil {
		return WriteFileResult{}, err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = s.transitionFileMutation(context.WithoutCancel(ctx), intent.OperationID, mutationDispatched, mutationAbandoned, "", "atomic replacement failed")
		return WriteFileResult{}, err
	}
	committed = true
	info, err := os.Stat(path)
	if err != nil {
		return WriteFileResult{}, err
	}
	document := fileDocument(filepath.ToSlash(clean), after, info)
	result := WriteFileResult{Document: document, BeforeSHA256: beforeSHA, Diff: diff, OperationID: intent.OperationID}
	if err = syncContainingDirectory(parent); err != nil {
		return result, &MutationCommittedReceiptFailed{Result: result, ReceiptErrorCode: "directory_sync_failed", Cause: err}
	}
	receipt, err := s.CreateArtifact(ctx, ArtifactInput{ProjectID: project.ID, Name: filepath.Base(clean) + ".diff",
		Kind: "workbench_diff", MIMEType: "text/x-diff; charset=utf-8", Content: diff,
		Metadata: map[string]any{"actor": input.Actor, "path": filepath.ToSlash(clean), "before_sha256": beforeSHA,
			"after_sha256": document.SHA256, "operation_id": intent.OperationID,
			"committed_at": time.Now().UTC().Format(time.RFC3339Nano)}})
	if err != nil {
		return result, &MutationCommittedReceiptFailed{Result: result, ReceiptErrorCode: "artifact_persist_failed", Cause: err}
	}
	result.ReceiptArtifact = receipt
	if err = s.transitionFileMutation(ctx, intent.OperationID, mutationDispatched, mutationObserved, receipt.ID, ""); err != nil {
		return result, &MutationCommittedReceiptFailed{Result: result, ReceiptErrorCode: "intent_receipt_bind_failed", Cause: err}
	}
	return result, nil
}

// WriteProjectFiles applies one reviewed multi-file change while holding every
// canonical path lock. It validates all preimages before the first mutation and
// keeps the locks through reverse-order rollback.
func (s *Service) WriteProjectFiles(ctx context.Context, projectID string, inputs []WriteFileInput) (BatchWriteResult, error) {
	if len(inputs) == 0 {
		return BatchWriteResult{}, fmt.Errorf("at least one file write is required")
	}
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return BatchWriteResult{}, err
	}
	root, err := requireRoot(project)
	if err != nil {
		return BatchWriteResult{}, err
	}
	lockRoot := root
	if canonical, canonicalErr := filepath.EvalSymlinks(root); canonicalErr == nil {
		lockRoot = canonical
	}
	type item struct {
		input WriteFileInput
		clean string
		key   string
	}
	items := make([]item, 0, len(inputs))
	seen := map[string]bool{}
	for _, input := range inputs {
		input.Actor = strings.TrimSpace(input.Actor)
		if input.Actor == "" || len(input.Content) > maxWorkbenchFileBytes || !utf8.ValidString(input.Content) || strings.IndexByte(input.Content, 0) >= 0 {
			return BatchWriteResult{}, fmt.Errorf("actor and UTF-8 content up to 2 MiB are required")
		}
		clean := filepath.Clean(strings.TrimSpace(input.Path))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return BatchWriteResult{}, fmt.Errorf("file path must stay inside the project root")
		}
		key := lockRoot + "\x00" + filepath.ToSlash(clean)
		if seen[key] {
			return BatchWriteResult{}, fmt.Errorf("file batch contains duplicate path %s", filepath.ToSlash(clean))
		}
		seen[key] = true
		items = append(items, item{input: input, clean: clean, key: key})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].key < items[j].key })
	releases := make([]func(), 0, len(items))
	for _, item := range items {
		release, lockErr := s.pathLocks.acquire(ctx, item.key)
		if lockErr != nil {
			for index := len(releases) - 1; index >= 0; index-- {
				releases[index]()
			}
			return BatchWriteResult{}, lockErr
		}
		releases = append(releases, release)
	}
	defer func() {
		for index := len(releases) - 1; index >= 0; index-- {
			releases[index]()
		}
	}()
	preimages := make(map[string]FileDocument, len(items))
	for _, item := range items {
		document, readErr := s.ReadProjectFile(ctx, projectID, item.clean)
		if readErr != nil {
			return BatchWriteResult{}, readErr
		}
		if !validSHA256(item.input.ExpectedSHA256) || document.SHA256 != item.input.ExpectedSHA256 {
			return BatchWriteResult{}, &PreimageChangedError{Path: filepath.ToSlash(item.clean),
				ExpectedSHA256: item.input.ExpectedSHA256, CurrentSHA256: document.SHA256}
		}
		preimages[filepath.ToSlash(item.clean)] = document
	}
	result := BatchWriteResult{Receipts: make([]WriteFileResult, 0, len(items))}
	for _, item := range items {
		receipt, writeErr := s.writeProjectFileLocked(ctx, project, root, item.clean, item.input)
		if writeErr == nil {
			result.Receipts = append(result.Receipts, receipt)
			continue
		}
		var committed *MutationCommittedReceiptFailed
		if errors.As(writeErr, &committed) && committed.Result.Document.Path != "" {
			result.Receipts = append(result.Receipts, committed.Result)
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		for index := len(result.Receipts) - 1; index >= 0; index-- {
			applied := result.Receipts[index]
			original := preimages[applied.Document.Path]
			rollback := FileRollbackResult{Path: applied.Document.Path, State: "failed"}
			current, readErr := s.ReadProjectFile(cleanupCtx, projectID, applied.Document.Path)
			if readErr != nil {
				rollback.Error = readErr.Error()
			} else if current.SHA256 != applied.Document.SHA256 {
				rollback.State, rollback.Error = "conflict", "file changed after apply"
			} else {
				_, restoreErr := s.writeProjectFileLocked(cleanupCtx, project, root, filepath.FromSlash(applied.Document.Path), WriteFileInput{
					Path: applied.Document.Path, Content: original.Content, ExpectedSHA256: current.SHA256,
					Actor: item.input.Actor + ":rollback",
				})
				if restoreErr == nil {
					rollback.State = "restored"
				} else {
					rollback.Error = restoreErr.Error()
				}
			}
			result.Rollback = append(result.Rollback, rollback)
		}
		cancel()
		return result, writeErr
	}
	return result, nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
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

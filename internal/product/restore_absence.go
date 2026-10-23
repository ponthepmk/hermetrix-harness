package product

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type fileAbsenceReceipt struct {
	Path                  string `json:"path"`
	ExpectedAppliedSHA256 string `json:"expected_applied_sha256"`
	AfterSHA256           string `json:"after_sha256"`
	QuarantinePath        string `json:"quarantine_path"`
	QuarantineSHA256      string `json:"quarantine_sha256"`
	OperationID           string `json:"operation_id"`
}

func verifiedAbsenceReceipt(root, projectID, relative, beforeSHA, operationID string, artifact Artifact, body []byte) bool {
	if artifact.ProjectID != projectID || artifact.Kind != "file_absence_receipt" || hashBytes(body) != artifact.Checksum {
		return false
	}
	var receipt fileAbsenceReceipt
	if json.Unmarshal(body, &receipt) != nil || receipt.Path != relative || receipt.OperationID != operationID || receipt.ExpectedAppliedSHA256 != beforeSHA || receipt.AfterSHA256 != "absent" || receipt.QuarantineSHA256 != beforeSHA {
		return false
	}
	parent, err := resolveInside(root, filepath.Dir(filepath.FromSlash(relative)), true)
	if err != nil || !filepath.IsAbs(receipt.QuarantinePath) || !strings.HasPrefix(filepath.Base(receipt.QuarantinePath), ".hermetrix-rollback-") {
		return false
	}
	wantParent, err := canonicalProjectRoot(parent)
	if err != nil {
		return false
	}
	gotParent, err := canonicalProjectRoot(filepath.Dir(receipt.QuarantinePath))
	if err != nil || wantParent != gotParent {
		return false
	}
	info, err := os.Lstat(receipt.QuarantinePath)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxWorkbenchFileBytes {
		return false
	}
	retained, err := os.ReadFile(receipt.QuarantinePath)
	return err == nil && hashBytes(retained) == beforeSHA
}

// RestoreProjectFileAbsence undoes an exact newly created file. It is not a
// recursive delete: reviewed bytes move into a retained quarantine and the
// visible path must be absent before a rollback receipt can pass.
func (s *Service) RestoreProjectFileAbsence(ctx context.Context, projectID, relative, expectedSHA, actor string) (FileRollbackResult, error) {
	return s.RestoreProjectFileAbsenceAtRoot(ctx, projectID, "", relative, expectedSHA, actor)
}

func (s *Service) RestoreProjectFileAbsenceAtRoot(ctx context.Context, projectID, expectedRoot, relative, expectedSHA, actor string) (FileRollbackResult, error) {
	unlock, err := s.guardProjectRoot(ctx, projectID, expectedRoot)
	if err != nil {
		return FileRollbackResult{}, err
	}
	defer unlock()
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return FileRollbackResult{}, err
	}
	root, err := requireRoot(project)
	if err != nil {
		return FileRollbackResult{}, err
	}
	root, err = bindProjectRoot(root, expectedRoot)
	if err != nil {
		return FileRollbackResult{}, err
	}
	clean := filepath.Clean(relative)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return FileRollbackResult{}, fmt.Errorf("rollback path must stay inside project")
	}
	lockRoot := root
	if canonical, canonicalErr := filepath.EvalSymlinks(root); canonicalErr == nil {
		lockRoot = canonical
	}
	release, err := s.pathLocks.acquire(ctx, lockRoot+"\x00"+filepath.ToSlash(clean))
	if err != nil {
		return FileRollbackResult{}, err
	}
	defer release()
	return s.restoreProjectFileAbsenceLocked(ctx, project, root, clean, expectedSHA, actor)
}

func (s *Service) restoreProjectFileAbsenceLocked(ctx context.Context, project Project, root, clean, expectedSHA, actor string) (FileRollbackResult, error) {
	result := FileRollbackResult{Path: filepath.ToSlash(clean), State: "failed"}
	if !validSHA256(expectedSHA) || strings.TrimSpace(actor) == "" {
		return result, fmt.Errorf("absence rollback requires actor and exact applied SHA-256")
	}
	doc, err := snapshotProjectFileAtRoot(root, clean)
	if err != nil {
		return result, err
	}
	if doc.SHA256 == "absent" {
		result.State = "restored"
		return result, nil
	}
	if doc.SHA256 != expectedSHA {
		return result, &PreimageChangedError{Path: result.Path, ExpectedSHA256: expectedSHA, CurrentSHA256: doc.SHA256}
	}
	source, _, err := regularProjectFile(root, clean)
	if err != nil {
		return result, err
	}
	parent := filepath.Dir(source)
	quarantine, err := os.CreateTemp(parent, ".hermetrix-rollback-*")
	if err != nil {
		return result, err
	}
	quarantinePath := quarantine.Name()
	if err = quarantine.Close(); err != nil {
		return result, err
	}
	moved := false
	defer func() {
		if !moved {
			_ = os.Remove(quarantinePath)
		}
	}()
	intent, err := s.createFileMutationIntent(ctx, project.ID, result.Path, actor, expectedSHA, "absent")
	if err != nil {
		return result, err
	}
	result.OperationID = intent.OperationID
	if err = s.transitionFileMutation(ctx, intent.OperationID, mutationPlanned, mutationDispatched, "", ""); err != nil {
		return result, err
	}
	if err = os.Rename(source, quarantinePath); err != nil {
		if receiptErr := s.transitionFileMutation(context.WithoutCancel(ctx), intent.OperationID, mutationDispatched, mutationAbandoned, "", "absence rollback did not move source"); receiptErr != nil {
			return result, receiptErr
		}
		return result, err
	}
	moved = true
	fail := func(message string) (FileRollbackResult, error) {
		if saveErr := s.transitionFileMutation(context.WithoutCancel(ctx), intent.OperationID, mutationDispatched, mutationUncertain, "", message); saveErr != nil {
			return result, saveErr
		}
		return result, fmt.Errorf("%s; retained file %s requires inspection", message, filepath.Base(quarantinePath))
	}
	retained, err := os.ReadFile(quarantinePath)
	if err != nil {
		return fail("cannot inspect quarantined rollback bytes")
	}
	if hashBytes(retained) != expectedSHA {
		// Never overwrite a newer file while restoring a conflicting writer.
		if restoreErr := os.Link(quarantinePath, source); restoreErr != nil {
			return fail("rollback preimage changed; both paths require inspection")
		}
		return fail("rollback preimage changed; conflicting bytes restored without replacement")
	}
	if _, err = os.Lstat(source); !os.IsNotExist(err) {
		return fail("visible path is not confirmed absent after rollback")
	}
	if err = syncContainingDirectory(parent); err != nil {
		return fail("rollback directory sync failed")
	}
	// Quarantine is retained, including across a receipt failure or crash.
	body, err := json.Marshal(fileAbsenceReceipt{Path: result.Path, ExpectedAppliedSHA256: expectedSHA, AfterSHA256: "absent", QuarantinePath: quarantinePath, QuarantineSHA256: hashBytes(retained), OperationID: intent.OperationID})
	if err != nil {
		return fail("cannot encode absence receipt")
	}
	artifact, err := s.CreateArtifact(ctx, ArtifactInput{ProjectID: project.ID, Name: filepath.Base(clean) + ".absence.json", Kind: "file_absence_receipt", MIMEType: "application/json", Content: string(body),
		Metadata: map[string]any{"operation_id": intent.OperationID, "path": result.Path, "before_sha256": expectedSHA, "after_sha256": "absent", "quarantine_path": quarantinePath}})
	if err != nil {
		return fail("cannot persist absence receipt")
	}
	result.ArtifactID = artifact.ID
	if err = s.transitionFileMutation(ctx, intent.OperationID, mutationDispatched, mutationObserved, artifact.ID, ""); err != nil {
		return result, err
	}
	result.State = "restored"
	return result, nil
}

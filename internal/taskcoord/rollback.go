package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
	"sort"
)

type rollbackFileReceipt struct {
	Path                   string `json:"path"`
	ExpectedAppliedSHA256  string `json:"expected_applied_sha256"`
	OriginalSHA256         string `json:"original_sha256"`
	BeforeSHA256           string `json:"before_sha256,omitempty"`
	AfterSHA256            string `json:"after_sha256,omitempty"`
	Status                 string `json:"status"`
	Error                  string `json:"error,omitempty"`
	RestorationArtifactID  string `json:"restoration_artifact_id,omitempty"`
	RestorationOperationID string `json:"restoration_operation_id,omitempty"`
}

type rollbackReceipt struct {
	ProposalID         string                `json:"proposal_id"`
	PreimageArtifactID string                `json:"preimage_artifact_id"`
	Restored           bool                  `json:"restored"`
	Files              []rollbackFileReceipt `json:"files"`
	Error              string                `json:"error,omitempty"`
}

type rollbackOutcome struct {
	Restored bool
	Artifact product.Artifact
}

func (s *Service) rollbackProposal(ctx context.Context, proposal taskengine.CodeProposal) rollbackOutcome {
	receipt := rollbackReceipt{ProposalID: proposal.ID, PreimageArtifactID: proposal.RollbackArtifactID,
		Files: []rollbackFileReceipt{}}
	finish := func() rollbackOutcome {
		body, err := json.Marshal(receipt)
		if err != nil {
			return rollbackOutcome{Restored: receipt.Restored}
		}
		artifact, err := s.products.CreateArtifact(context.WithoutCancel(ctx), product.ArtifactInput{
			ProjectID: proposal.ProjectID, Name: proposal.ID + ".rollback-receipt.json",
			Kind: "code_rollback_receipt", MIMEType: "application/vnd.hermetrix.rollback-receipt+json",
			Content: string(body), Metadata: map[string]any{"proposal_id": proposal.ID, "restored": receipt.Restored},
		})
		if err != nil {
			return rollbackOutcome{Restored: receipt.Restored}
		}
		return rollbackOutcome{Restored: receipt.Restored, Artifact: artifact}
	}
	loaded, loadErr := s.loadProposalBundle(ctx, proposal)
	if loadErr != nil {
		receipt.Error = fmt.Sprintf("failed to load proposal bundle: %v", loadErr)
		return finish()
	}
	expectedApplied := map[string]string{}
	for _, change := range loaded.Bundle.Result.Changes {
		expectedApplied[change.Path] = worker.Hash(change.Content)
	}
	if proposal.RollbackArtifactID == "" {
		receipt.Error = "rollback preimage artifact is missing"
		return finish()
	}
	artifact, body, err := s.products.GetArtifact(ctx, proposal.RollbackArtifactID)
	if err != nil || artifact.Kind != "code_proposal_rollback" || artifact.ProjectID != proposal.ProjectID ||
		artifact.Metadata["proposal_id"] != proposal.ID {
		receipt.Error = "rollback preimage artifact is unavailable or does not match this proposal"
		return finish()
	}
	var originals map[string]product.FileDocument
	if json.Unmarshal(body, &originals) != nil || len(originals) == 0 {
		receipt.Error = "rollback preimage artifact has no valid files"
		return finish()
	}
	frozenRoot, _ := artifact.Metadata["project_root"].(string)
	if frozenRoot != "" {
		if currentRoot, rootErr := s.products.ProjectRootIdentity(ctx, proposal.ProjectID); rootErr != nil || currentRoot != frozenRoot {
			receipt.Error = "project root changed; inspect original scope before rollback"
			return finish()
		}
	}
	paths := make([]string, 0, len(originals))
	for path := range originals {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	receipt.Restored = true
	for _, path := range paths {
		original := originals[path]
		file := rollbackFileReceipt{Path: path, ExpectedAppliedSHA256: expectedApplied[path], OriginalSHA256: original.SHA256}
		current, readErr := s.products.SnapshotProjectFileAtRoot(ctx, proposal.ProjectID, frozenRoot, path)
		if readErr != nil {
			file.Status, file.Error = "not_restored", readErr.Error()
		} else {
			file.BeforeSHA256 = current.SHA256
			switch {
			case expectedApplied[path] == "":
				file.Status, file.Error = "not_restored", "applied file hash is unavailable"
			case current.SHA256 == original.SHA256:
				file.Status, file.AfterSHA256 = "already_original", current.SHA256
			case current.SHA256 != expectedApplied[path]:
				file.Status, file.Error = "changed_since_apply", "file no longer matches the applied proposal"
			case original.SHA256 == "absent":
				removed, removeErr := s.products.RestoreProjectFileAbsenceAtRoot(ctx, proposal.ProjectID, frozenRoot, path, current.SHA256, "verification-rollback")
				file.RestorationArtifactID, file.RestorationOperationID = removed.ArtifactID, removed.OperationID
				if removeErr != nil {
					file.Status, file.Error = "not_restored", removeErr.Error()
				} else if removed.State != "restored" {
					file.Status, file.Error = "not_restored", "absence rollback lacks a verified receipt"
				} else {
					file.Status, file.AfterSHA256 = "restored", "absent"
				}
			default:
				_, writeErr := s.products.WriteProjectFilesAtRoot(ctx, proposal.ProjectID, frozenRoot, []product.WriteFileInput{{
					Path: path, Content: original.Content, ExpectedSHA256: current.SHA256, Actor: "verification-rollback",
				}})
				if writeErr != nil {
					file.Status, file.Error = "not_restored", writeErr.Error()
				} else if after, afterErr := s.products.ReadProjectFile(ctx, proposal.ProjectID, path); afterErr != nil {
					file.Status, file.Error = "not_verified", afterErr.Error()
				} else {
					file.AfterSHA256 = after.SHA256
					if after.SHA256 == original.SHA256 {
						file.Status = "restored"
					} else {
						file.Status, file.Error = "not_verified", "file hash does not match the original"
					}
				}
			}
		}
		if file.Status != "restored" && file.Status != "already_original" {
			receipt.Restored = false
		}
		receipt.Files = append(receipt.Files, file)
	}
	return finish()
}

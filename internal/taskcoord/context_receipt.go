package taskcoord

import (
	"context"
	"sort"

	"hermetrix-harness/internal/taskengine"
)

// ContextReceipt exposes identities from the verified immutable proposal input.
// Bodies are deliberately absent: this is evidence of inclusion, not a second
// source of instructions or an assertion that a reference improved the answer.
type ContextReceipt struct {
	InputArtifactID string               `json:"input_artifact_id"`
	InputSHA256     string               `json:"input_sha256"`
	SourceFiles     int                  `json:"source_files"`
	Skills          []ContextSkill       `json:"skills"`
	Knowledge       []ContextKnowledge   `json:"knowledge"`
	RepairDrafts    []ContextRepairDraft `json:"repair_drafts,omitempty"`
}
type ContextRepairDraft struct {
	Path                 string `json:"path"`
	ProposalID           string `json:"proposal_id"`
	SourceArtifactID     string `json:"source_artifact_id"`
	SourceArtifactSHA256 string `json:"source_artifact_sha256"`
	SourceSHA256         string `json:"source_sha256"`
}
type ContextSkill struct {
	SkillID     string `json:"skill_id"`
	VersionID   string `json:"version_id"`
	ContentHash string `json:"content_hash"`
	Name        string `json:"name"`
}
type ContextKnowledge struct {
	Citation string `json:"citation"`
	Version  string `json:"version"`
}

func (s *Service) ProposalContext(ctx context.Context, proposal taskengine.CodeProposal) (*ContextReceipt, error) {
	loaded, err := s.loadProposalBundle(ctx, proposal)
	if err != nil {
		return nil, err
	}
	// Legacy proposals have no saved canonical input. Do not infer inclusion.
	if loaded.InputTask == nil {
		return nil, nil
	}
	task := loaded.InputTask
	receipt := &ContextReceipt{InputArtifactID: loaded.Bundle.InputArtifactID, InputSHA256: loaded.Bundle.Result.InputSHA256, SourceFiles: len(task.Files), Skills: []ContextSkill{}, Knowledge: []ContextKnowledge{}}
	for _, ref := range task.SkillRefs {
		receipt.Skills = append(receipt.Skills, ContextSkill{SkillID: ref.SkillID, VersionID: ref.VersionID, ContentHash: ref.ContentHash, Name: ref.Name})
	}
	for _, ref := range task.ProjectBrainRefs {
		receipt.Knowledge = append(receipt.Knowledge, ContextKnowledge{Citation: ref.Citation, Version: ref.Version})
	}
	paths := make([]string, 0, len(task.RepairDrafts))
	for path := range task.RepairDrafts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		draft := task.RepairDrafts[path]
		receipt.RepairDrafts = append(receipt.RepairDrafts, ContextRepairDraft{
			Path: path, ProposalID: draft.ProposalID, SourceArtifactID: draft.SourceArtifactID,
			SourceArtifactSHA256: draft.SourceArtifactSHA256, SourceSHA256: draft.SourceSHA256,
		})
	}
	return receipt, nil
}

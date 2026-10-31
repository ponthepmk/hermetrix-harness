package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

// attachRepairDrafts uses only a checksum-verified, verification-failed proposal
// belonging to this task and requirement. It never substitutes historical text
// into Files, changes writable scope, or changes any run/goal budget.
func (s *Service) attachRepairDrafts(ctx context.Context, packet taskengine.StepPacket, task worker.Task, preset inference.Preset, root string) (worker.Task, error) {
	if len(packet.Requirement.Contract.WritablePaths) == 0 || len(task.RepairDrafts) != 0 {
		return task, nil
	}
	for _, evidence := range task.FailureEvidence {
		if evidence.ArtifactID == "" || !contains(packet.Step.Checks, evidence.CheckID) {
			continue
		}
		matched := false
		for _, record := range packet.ValidationEvidence {
			matched = matched || matchesEvidenceRecord(record, packet, evidence)
		}
		if !matched {
			continue
		}
		proposal, found, err := s.tasks.FailedProposalForLog(ctx, packet.TaskID, historicalFailureStepID(packet, evidence), evidence.ArtifactID)
		if err != nil {
			return task, err
		}
		if !found {
			continue
		}
		if proposal.ProjectID != packet.ProjectID {
			return task, fmt.Errorf("repair draft proposal project mismatch")
		}
		origin, err := s.tasks.Attempt(ctx, proposal.AttemptID)
		if err != nil {
			return task, err
		}
		if origin.Packet == nil {
			continue // Legacy attempts cannot establish a frozen requirement.
		}
		p := origin.Packet
		if err = taskengine.ValidateStepPacket(*p); err != nil {
			return task, fmt.Errorf("invalid repair draft origin packet: %w", err)
		}
		if origin.TaskID != packet.TaskID || origin.StepID != proposal.StepID || origin.InputHash != proposal.PacketHash ||
			p.CanonicalPacketHash != proposal.PacketHash || p.TaskID != packet.TaskID || p.ProjectID != packet.ProjectID ||
			p.Requirement.ID != packet.Requirement.ID || p.Requirement.Revision != packet.Requirement.Revision {
			continue
		}
		loaded, err := s.loadProposalBundle(ctx, proposal)
		if err != nil {
			return task, err
		}
		if loaded.InputTask == nil || loaded.Artifact.Metadata["project_root"] != root {
			continue
		}
		log, body, err := s.products.GetArtifact(ctx, evidence.ArtifactID)
		if err != nil {
			return task, err
		}
		if log.Kind != "terminal_log" || log.ProjectID != packet.ProjectID || log.Checksum != evidence.ArtifactSHA256 || worker.Hash(string(body)) != log.Checksum {
			return task, fmt.Errorf("repair draft failure log binding mismatch")
		}
		candidate := task
		candidate.RepairDrafts = make(map[string]worker.RepairDraft)
		changes := slices.Clone(loaded.Bundle.Result.Changes)
		sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
		remaining := worker.MaxRepairDraftBytes
		for _, change := range changes {
			current, selected := task.Files[change.Path]
			_, partial := task.FileViews[change.Path]
			if !selected || partial || !contains(task.WritablePaths, change.Path) ||
				!contains(packet.Requirement.Contract.WritablePaths, change.Path) || change.Content == "" ||
				change.Content == current || !utf8.ValidString(change.Content) || len(change.Content) > remaining {
				continue
			}
			missing := contains(task.AbsentFiles, change.Path)
			if change.BeforeSHA256 == "absent" && !missing || change.BeforeSHA256 != "absent" && (missing || worker.Hash(current) != change.BeforeSHA256) {
				continue // The workspace no longer matches the rolled-back preimage.
			}
			candidate.RepairDrafts[change.Path] = worker.RepairDraft{
				ProposalID: proposal.ID, SourceArtifactID: loaded.Artifact.ID, SourceArtifactSHA256: loaded.Artifact.Checksum,
				InputSHA256: loaded.Bundle.Result.InputSHA256, SourceSHA256: worker.Hash(change.Content), Content: change.Content,
			}
			remaining -= len(change.Content)
			if len(candidate.RepairDrafts) == worker.MaxRepairDraftFiles {
				break
			}
		}
		if len(candidate.RepairDrafts) == 0 {
			return task, nil
		}
		candidate.RepairTargetVersion = worker.FocusedRepairTargetVersion
		candidate.FailureEvidence = slices.Clone(task.FailureEvidence)
		for i := range candidate.FailureEvidence {
			stripRepairDiagnostic(&candidate.FailureEvidence[i], proposal, loaded)
		}
		candidate.Brief = strings.ReplaceAll(candidate.Brief,
			"prefer complete replacement content if exact old/new matching failed",
			"for explicitly supplied repair drafts use exact edits or explicit retention; ordinary paths keep their normal source forms")
		if !proposalTaskFits(candidate, preset) {
			candidate.RepairTargetVersion = worker.AtomicRepairTargetVersion
			if !proposalTaskFits(candidate, preset) {
				candidate.RepairTargetVersion = worker.RepairTargetVersion
				if !proposalTaskFits(candidate, preset) {
					candidate.RepairTargetVersion = 0
					if !proposalTaskFits(candidate, preset) {
						return task, nil
					}
				}
			} // Keep bounded whole drafts or the original context; never clip.
		}
		frozen, _, _, err := worker.FreezeTask(candidate)
		if err != nil {
			return task, err
		}
		return frozen, nil
	}
	return task, nil
}

func stripRepairDiagnostic(e *worker.FailureEvidence, proposal taskengine.CodeProposal, loaded loadedProposal) {
	var wrapper struct {
		Terminal  string `json:"terminal_output"`
		Truncated bool   `json:"terminal_output_truncated"`
		Source    struct {
			Proposal string `json:"proposal_id"`
			Artifact string `json:"source_artifact_id"`
			SHA      string `json:"source_artifact_sha256"`
		} `json:"historical_failed_source"`
	}
	if json.Unmarshal([]byte(e.Output), &wrapper) == nil && wrapper.Source.Proposal == proposal.ID &&
		wrapper.Source.Artifact == loaded.Artifact.ID && wrapper.Source.SHA == loaded.Artifact.Checksum {
		e.Output, e.Truncated = wrapper.Terminal, wrapper.Truncated
	}
}

// A repair proposal must differ from the verified failing source somewhere.
// Retention restores unchanged companions alongside a repair; on its own it
// would spend another apply/check/rollback cycle reproducing the same source.
func repeatsFailedDrafts(task worker.Task, result worker.Result) bool {
	if len(task.RepairDrafts) == 0 || len(result.Changes) == 0 {
		return false
	}
	for _, change := range result.Changes {
		draft, historical := task.RepairDrafts[change.Path]
		if !historical || change.Content != draft.Content {
			return false
		}
	}
	return true
}

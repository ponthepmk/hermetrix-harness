package taskcoord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

const reductionEffect = "proposal.reduce"

type repairReductionProof struct {
	Version              int                    `json:"version"`
	SourceProposalID     string                 `json:"source_proposal_id"`
	SourceArtifactSHA256 string                 `json:"source_artifact_sha256"`
	Selection            worker.RepairSelection `json:"selection"`
}

// validateReduction rederives a persisted host proposal from one complete native
// parent. It never trusts an envelope merely because its hashes were re-signed.
func (s *Service) validateReduction(ctx context.Context, p taskengine.CodeProposal, loaded loadedProposal) error {
	proof := loaded.Bundle.Reduction
	if proof == nil || proof.Version != 1 || proof.SourceProposalID == p.ID || loaded.InputTask == nil {
		return fmt.Errorf("invalid reduction proof")
	}
	parent, err := s.tasks.GetCodeProposal(ctx, proof.SourceProposalID)
	if err != nil {
		return err
	}
	original, err := s.loadProposalWithoutReduction(ctx, parent)
	if err != nil {
		return err
	}
	if original.Bundle.Reduction != nil || proof.SourceArtifactSHA256 != original.Artifact.Checksum ||
		p.ProviderID != parent.ProviderID || p.ProviderRevision != parent.ProviderRevision {
		return fmt.Errorf("reduction must bind one original native parent and implementer")
	}
	attempt, err := s.tasks.Attempt(ctx, p.AttemptID)
	if err != nil {
		return err
	}
	if attempt.Packet == nil {
		return fmt.Errorf("reduction requires a frozen current packet")
	}
	root, _ := loaded.Artifact.Metadata["project_root"].(string)
	bound, err := s.reductionSourceBound(ctx, *attempt.Packet, parent, original, root)
	if err != nil {
		return err
	}
	if !bound {
		return fmt.Errorf("reduction source no longer binds failed evidence and requirement")
	}
	_, expected, err := worker.ReduceRepairResult(*original.InputTask, original.Bundle.Result, loaded.InputTask.ID, proof.Selection)
	if err != nil {
		return err
	}
	want, _ := json.Marshal(expected)
	got, _ := json.Marshal(loaded.Bundle.Result)
	if !bytes.Equal(want, got) || loaded.Artifact.Metadata["origin"] != "host_single_edit_reduction" {
		return fmt.Errorf("reduced result differs from the recorded single native edit")
	}
	return nil
}

// A native parent must be tied to real failed mandatory checks, complete
// provider receipts, the same immutable requirement and a verified rollback.
func (s *Service) reductionSourceBound(ctx context.Context, packet taskengine.StepPacket, p taskengine.CodeProposal, loaded loadedProposal, root string) (bool, error) {
	if loaded.InputTask == nil || loaded.Bundle.Reduction != nil || p.State != taskengine.ProposalVerifyFailed ||
		p.TaskID != packet.TaskID || p.ProjectID != packet.ProjectID || loaded.Artifact.Metadata["project_root"] != root ||
		loaded.Artifact.Metadata["provider_id"] != p.ProviderID || loaded.Artifact.Metadata["provider_revision"] != p.ProviderRevision {
		return false, nil
	}
	if err := taskengine.ValidateStepPacket(packet); err != nil {
		return false, err
	}
	origin, err := s.tasks.Attempt(ctx, p.AttemptID)
	if err != nil {
		return false, err
	}
	if origin.Packet == nil || origin.State != taskengine.AttemptFailed {
		return false, nil
	}
	prior := *origin.Packet
	if err = taskengine.ValidateStepPacket(prior); err != nil {
		return false, err
	}
	priorIDs, currentIDs := slices.Clone(prior.Step.RequirementIDs), slices.Clone(packet.Step.RequirementIDs)
	slices.Sort(priorIDs)
	slices.Sort(currentIDs)
	if prior.TaskID != p.TaskID || prior.ProjectID != p.ProjectID || origin.InputHash != p.PacketHash || prior.CanonicalPacketHash != p.PacketHash ||
		prior.Step.ID != p.StepID || prior.Requirement.ID != packet.Requirement.ID || prior.Requirement.Revision != packet.Requirement.Revision ||
		!slices.Equal(prior.Step.Checks, packet.Step.Checks) || !slices.Equal(priorIDs, currentIDs) {
		return false, nil
	}
	if _, err = worker.RepairSelections(*loaded.InputTask, loaded.Bundle.Result); err != nil {
		return false, nil
	}
	matched := false
	for _, v := range packet.ValidationEvidence {
		if v.Status != taskengine.ValidationFail || !contains(packet.Step.Checks, v.CheckID) {
			continue
		}
		for _, ref := range v.EvidenceRefs {
			if !strings.HasPrefix(ref, "artifact:") {
				continue
			}
			id := strings.TrimPrefix(ref, "artifact:")
			failed, found, e := s.tasks.FailedProposalForLog(ctx, p.TaskID, p.StepID, id)
			if e != nil {
				return false, e
			}
			if !found || failed.ID != p.ID {
				continue
			}
			log, body, e := s.products.GetArtifact(ctx, id)
			if e != nil {
				return false, e
			}
			if log.ProjectID != p.ProjectID || log.Kind != "terminal_log" || worker.Hash(string(body)) != log.Checksum {
				return false, fmt.Errorf("reduction failure log mismatch")
			}
			matched = true
		}
	}
	if !matched {
		return false, nil
	}
	effects, err := s.tasks.AttemptEffects(ctx, p.AttemptID)
	if err != nil {
		return false, err
	}
	native := false
	for _, effect := range effects {
		if effect.State != taskengine.EffectObserved && effect.State != taskengine.EffectReconciled && effect.State != taskengine.EffectAbandoned {
			return false, nil
		}
		if effect.Action == proposalEffect && (effect.State == taskengine.EffectObserved || effect.State == taskengine.EffectReconciled) &&
			effect.Receipt["status"] == "proposed_unverified" && effect.Receipt["proposal_id"] == p.ID && effect.Receipt["artifact_id"] == p.ArtifactID {
			native = true
		}
	}
	if !native {
		return false, nil
	}
	refs, err := s.tasks.ProposalFailureRefs(ctx, p)
	if err != nil {
		return false, err
	}
	for _, ref := range refs {
		if !strings.HasPrefix(ref, "rollback_artifact:") {
			continue
		}
		a, body, e := s.products.GetArtifact(ctx, strings.TrimPrefix(ref, "rollback_artifact:"))
		if e != nil {
			return false, e
		}
		var receipt rollbackReceipt
		if a.Kind != "code_rollback_receipt" || a.ProjectID != p.ProjectID || worker.Hash(string(body)) != a.Checksum || json.Unmarshal(body, &receipt) != nil ||
			!receipt.Restored || receipt.ProposalID != p.ID || receipt.PreimageArtifactID != p.RollbackArtifactID || len(receipt.Files) != len(loaded.Bundle.Result.Changes) {
			continue
		}
		seen := map[string]bool{}
		for _, file := range receipt.Files {
			if seen[file.Path] || (file.Status != "restored" && file.Status != "already_original") {
				return false, fmt.Errorf("invalid reduction rollback receipt")
			}
			seen[file.Path] = true
			found := false
			for _, change := range loaded.Bundle.Result.Changes {
				if change.Path == file.Path && file.OriginalSHA256 == change.BeforeSHA256 && file.AfterSHA256 == change.BeforeSHA256 && file.ExpectedAppliedSHA256 == worker.Hash(change.Content) {
					found = true
				}
			}
			if !found {
				return false, fmt.Errorf("reduction rollback preimage mismatch")
			}
		}
		return true, nil
	}
	return false, nil
}

// tryGoalReduction is a normal, budget-charged goal propose action. Its own
// explicitly authorized host effect is recorded; it creates no provider call.
func (s *Service) tryGoalReduction(ctx context.Context, g taskengine.GoalRun, x taskengine.ExecutionSnapshot, auth taskengine.RunAuthority) (out ProposalOutput, handled bool, err error) {
	if x.Attempt == nil || x.Attempt.Packet == nil || !contains(x.Attempt.Packet.Step.EffectScope, reductionEffect) {
		return out, false, nil
	}
	packet := *x.Attempt.Packet
	effects, err := s.tasks.AttemptEffects(ctx, x.Attempt.ID)
	if err != nil {
		return out, true, err
	}
	for _, effect := range effects {
		if effect.Action == reductionEffect {
			return out, true, fmt.Errorf("reduction already has durable state; inspect its existing proposal/effect")
		}
	}
	seen := map[string]bool{}
	for i := len(packet.ValidationEvidence) - 1; i >= 0; i-- {
		v := packet.ValidationEvidence[i]
		if v.Status != taskengine.ValidationFail || !contains(packet.Step.Checks, v.CheckID) {
			continue
		}
		for _, ref := range v.EvidenceRefs {
			if !strings.HasPrefix(ref, "artifact:") {
				continue
			}
			evidence := worker.FailureEvidence{ValidationID: v.ID, CheckID: v.CheckID, SubjectRevision: v.SubjectRevision, Actual: v.Actual, ArtifactID: strings.TrimPrefix(ref, "artifact:")}
			parent, found, e := s.tasks.FailedProposalForLog(ctx, packet.TaskID, historicalFailureStepID(packet, evidence), strings.TrimPrefix(ref, "artifact:"))
			if e != nil {
				return out, true, e
			}
			if !found {
				continue
			}
			loaded, e := s.loadProposalBundle(ctx, parent)
			if e != nil {
				return out, true, e
			}
			if loaded.Bundle.Reduction != nil {
				parent, e = s.tasks.GetCodeProposal(ctx, loaded.Bundle.Reduction.SourceProposalID)
				if e != nil {
					return out, true, e
				}
				loaded, e = s.loadProposalBundle(ctx, parent)
				if e != nil {
					return out, true, e
				}
			}
			if seen[parent.ID] {
				continue
			}
			seen[parent.ID] = true
			if parent.ProviderID != g.Config.ProviderID || parent.ProviderRevision != g.Config.ProviderRevision {
				continue
			}
			bound, e := s.reductionSourceBound(ctx, packet, parent, loaded, g.Config.ProjectRoot)
			if e != nil {
				return out, true, e
			}
			if !bound {
				continue
			}
			// Every input file, including readonly tests/modules, must still be the
			// original source. A drift invalidates reduction; nothing is applied.
			for path, content := range loaded.InputTask.Files {
				doc, e := s.products.SnapshotProjectFileAtRoot(ctx, packet.ProjectID, g.Config.ProjectRoot, path)
				if e != nil {
					return out, true, e
				}
				want := worker.Hash(content)
				if contains(loaded.InputTask.AbsentFiles, path) {
					want = "absent"
				}
				if doc.SHA256 != want {
					return out, false, nil
				}
			}
			used, e := s.tasks.UsedRepairReductionTargets(ctx, packet.TaskID, parent.ID)
			if e != nil {
				return out, true, e
			}
			choices, _ := worker.RepairSelections(*loaded.InputTask, loaded.Bundle.Result)
			for _, selected := range choices {
				if slices.Contains(used, selected.TargetID) {
					continue
				}
				id := packet.TaskID + ":" + packet.Step.Key + ":" + packet.CanonicalPacketHash
				task, result, e := worker.ReduceRepairResult(*loaded.InputTask, loaded.Bundle.Result, id, selected)
				if e != nil {
					continue
				} // e.g. restoring this subset would be a current no-op.
				proof := repairReductionProof{Version: 1, SourceProposalID: parent.ID, SourceArtifactSHA256: loaded.Artifact.Checksum, Selection: selected}
				out, e = s.persistGoalReduction(ctx, auth, x.Attempt.ID, packet, g.Config.ProjectRoot, parent, task, result, proof)
				return out, true, e
			}
			return out, false, nil // exhausted this native parent's bounded candidates.
		}
	}
	return out, false, nil
}

func (s *Service) persistGoalReduction(ctx context.Context, auth taskengine.RunAuthority, attemptID string, packet taskengine.StepPacket, root string, parent taskengine.CodeProposal, task worker.Task, result worker.Result, proof repairReductionProof) (out ProposalOutput, err error) {
	effect, err := s.tasks.PlanEffect(ctx, auth, attemptID, reductionEffect, "proposal:"+parent.ID, "task-packet:"+packet.CanonicalPacketHash)
	if err != nil {
		return out, err
	}
	out.Effect = effect
	effect, err = s.tasks.DispatchEffect(ctx, auth, effect.OperationID)
	if err != nil {
		return out, err
	}
	out.Effect = effect
	defer func() {
		if err != nil {
			if e, oe := s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "reduction_persist_failed", "source_proposal_id": parent.ID, "target_id": proof.Selection.TargetID}); oe == nil {
				out.Effect = e
			}
		}
	}()
	_, canonical, _, err := worker.FreezeTask(task)
	if err != nil {
		return out, err
	}
	metadata := map[string]any{"task_id": packet.TaskID, "step_id": packet.Step.ID, "attempt_id": attemptID, "packet_hash": packet.CanonicalPacketHash, "proposal_id": result.ProposalID, "project_root": root, "source_operation_id": effect.OperationID, "origin": "host_single_edit_reduction", "source_proposal_id": parent.ID}
	input, err := s.products.CreateArtifact(ctx, product.ArtifactInput{ProjectID: packet.ProjectID, Name: result.ProposalID + ".input.json", Kind: "code_proposal_input", MIMEType: "application/json", Content: string(canonical), Metadata: metadata})
	if err != nil {
		return out, err
	}
	body, err := json.Marshal(proposalBundle{Version: 2, InputArtifactID: input.ID, Result: result, Reduction: &proof})
	if err != nil {
		return out, err
	}
	artifact, err := s.products.CreateArtifact(ctx, product.ArtifactInput{ProjectID: packet.ProjectID, Name: result.ProposalID + ".reduced.json", Kind: "code_proposal", MIMEType: "application/vnd.hermetrix.code-proposal+json", Content: string(body), Metadata: metadata})
	if err != nil {
		return out, err
	}
	out.Artifact = artifact
	out.Result = result
	proposal, err := s.tasks.RecordCodeProposal(ctx, taskengine.RecordCodeProposalInput{ID: result.ProposalID, TaskID: packet.TaskID, StepID: packet.Step.ID, AttemptID: attemptID, ProjectID: packet.ProjectID, PacketHash: packet.CanonicalPacketHash, ProviderID: parent.ProviderID, ProviderRevision: parent.ProviderRevision, ArtifactID: artifact.ID, ResultHash: artifact.Checksum})
	if err != nil {
		return out, err
	}
	out.Proposal = proposal
	out.Effect, err = s.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "proposed_unverified", "proposal_id": proposal.ID, "artifact_id": artifact.ID, "source_proposal_id": parent.ID, "target_id": proof.Selection.TargetID, "provider_called": false})
	return out, err
}

package taskcoord

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"hermetrix-harness/internal/learning"
	"hermetrix-harness/internal/taskengine"
)

type taskLearningObserver struct{ coordinator *Service }

func (o taskLearningObserver) PrepareCompletion(ctx context.Context, task taskengine.Task) (taskengine.CompletionStage, error) {
	if task.State != taskengine.StateVerifying {
		return nil, nil
	}
	s := o.coordinator
	snapshot, err := s.tasks.Execution(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if snapshot.Task.Revision != task.Revision {
		return nil, taskengine.ErrStaleRevision
	}
	var solutions []learning.SolutionEvidence
	if snapshot.Proposal != nil && snapshot.Proposal.State == taskengine.ProposalVerified {
		loaded, err := s.loadProposalBundle(ctx, *snapshot.Proposal)
		if err != nil {
			return nil, fmt.Errorf("prepare completed task learning evidence: %w", err)
		}
		if loaded.InputTask != nil {
			solutions = []learning.SolutionEvidence{taskLearningSolution(loaded)}
		}
	}
	return func(ctx context.Context, tx *sql.Tx, completed taskengine.Task, report taskengine.CompletionReport) error {
		return s.stageTaskLearning(ctx, tx, completed, report, solutions)
	}, nil
}

func (s *Service) stageTaskLearning(ctx context.Context, tx *sql.Tx, task taskengine.Task, report taskengine.CompletionReport, solutions []learning.SolutionEvidence) error {
	if s.learning == nil {
		return nil
	}
	digest := learning.Digest{GoalAndConstraints: task.Objective + "\n" + strings.Join(task.Requirement.Constraints, "\n"), Outcome: "completed", Solutions: solutions}
	for _, step := range task.Plan.Steps {
		digest.Decisions = append(digest.Decisions, step.Title+": "+step.Instructions)
	}
	seen := map[string]bool{}
	for _, gate := range report.Gates {
		if gate.Status != taskengine.ValidationPass {
			continue
		}
		for _, ref := range gate.EvidenceRefs {
			if !seen[ref] {
				seen[ref] = true
				if strings.HasPrefix(ref, "acceptance:") {
					digest.Decisions = append(digest.Decisions, "Owner accepted the exact completion evidence: "+ref)
					continue
				}
				digest.VerifiedBy = append(digest.VerifiedBy, ref)
				digest.ToolReceipts = append(digest.ToolReceipts, ref)
				if strings.HasPrefix(ref, "artifact:") {
					digest.Artifacts = append(digest.Artifacts, ref)
				}
			}
		}
	}
	for _, solution := range solutions {
		ref := "artifact:" + solution.ProposalArtifactID
		if !seen[ref] {
			digest.Artifacts = append(digest.Artifacts, ref)
			seen[ref] = true
		}
	}
	_, err := s.learning.StageTrigger(ctx, tx, learning.EnqueueInput{SessionID: "task:" + task.ID, MilestoneID: "task-complete:" + fmt.Sprint(task.Revision), TriggerKind: "successful_milestone", Digest: digest})
	return err
}

// Only the already validated proposal passed by Review enters this digest.
// Keep source bodies out of background learning and name inclusion honestly.
func taskLearningSolution(proposal loadedProposal) learning.SolutionEvidence {
	result := proposal.Bundle.Result
	out := learning.SolutionEvidence{ProposalArtifactID: proposal.Artifact.ID, InputSHA256: result.InputSHA256}
	summary := []rune(result.Summary)
	if len(summary) > 1200 {
		summary = summary[:1200]
		out.Truncated = true
	}
	out.Summary = string(summary)
	for i, change := range result.Changes {
		if i == 32 {
			out.Truncated = true
			break
		}
		out.ChangedPaths = append(out.ChangedPaths, change.Path)
	}
	if proposal.InputTask != nil {
		for _, ref := range proposal.InputTask.SkillRefs {
			out.IncludedSkills = append(out.IncludedSkills, ref.SkillID+"@"+ref.VersionID+"#"+ref.ContentHash)
		}
	}
	return out
}

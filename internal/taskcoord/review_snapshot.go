package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
)

// Final goal receipts are fenced independently of the reviewer call: a source
// or project change between task finalization and goal finalization is not
// permission to report the new workspace as a completed goal.
func (s *Service) guardGoalCompletionSnapshot(ctx context.Context, g taskengine.GoalRun, x taskengine.ExecutionSnapshot) (func(), error) {
	snapshot := product.ProjectSnapshot{Root: g.Config.ProjectRoot, Hashes: map[string]string{}}
	if x.Proposal != nil {
		artifact, body, err := s.products.GetArtifact(ctx, x.Proposal.VerificationArtifactID)
		if err != nil {
			return nil, err
		}
		if artifact.ProjectID != x.Task.ProjectID || artifact.Kind != "code_verification_bundle" || artifact.Metadata["proposal_id"] != x.Proposal.ID {
			return nil, fmt.Errorf("goal verification artifact binding changed")
		}
		var bundle verificationBundle
		if err = json.Unmarshal(body, &bundle); err != nil || bundle.ProposalID != x.Proposal.ID {
			return nil, fmt.Errorf("goal verification bundle is invalid")
		}
		snapshot = bundle.Snapshot
	}
	if snapshot.Root != g.Config.ProjectRoot {
		return nil, fmt.Errorf("goal verification root differs from frozen goal scope")
	}
	return s.products.GuardProjectSnapshot(ctx, x.Task.ProjectID, snapshot)
}

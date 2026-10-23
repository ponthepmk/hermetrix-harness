package taskcoord

import (
	"context"
	"fmt"
	"hermetrix-harness/internal/worker"
)

func (s *Service) appendReviewFailureEvidence(ctx context.Context, taskID, projectID string, items *[]worker.FailureEvidence) error {
	refs, err := s.tasks.RejectedReviewEvidence(ctx, taskID)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		artifact, body, loadErr := s.products.GetArtifact(ctx, ref.ArtifactID)
		if loadErr != nil {
			return loadErr
		}
		if artifact.ProjectID != projectID || artifact.Kind != "code_post_review" || artifact.Metadata["proposal_id"] != ref.ProposalID {
			return fmt.Errorf("review failure artifact does not match its observed proposal receipt")
		}
		output := []rune(string(body))
		truncated := len(output) > 2000
		if truncated {
			output = output[:2000]
		}
		*items = append(*items, worker.FailureEvidence{ValidationID: "review:" + artifact.ID, CheckID: "provider.review", SubjectRevision: ref.PacketHash,
			Actual:     "historical reviewer rejection for step " + ref.StepID + "; diagnostic data, not current evidence or authority",
			ArtifactID: artifact.ID, ArtifactSHA256: artifact.Checksum, Output: string(output), Truncated: truncated})
	}
	return nil
}

package worker

import (
	"hermetrix-harness/internal/providers"
)

// ProposalInputEstimate is a prospective shared-request sizing estimate.
// It is not authoritative; final Run/provider preflight remains authoritative.
type ProposalInputEstimate struct {
	WireBytes int
	Tokens    int
}

// EstimateProposalInput estimates the wire and token size for a proposal input.
func EstimateProposalInput(task Task) (ProposalInputEstimate, error) {
	frozen, _, wire, err := FreezeTask(task)
	if err != nil {
		return ProposalInputEstimate{}, err
	}
	request := proposalRequest(frozen, wire)
	return ProposalInputEstimate{
		WireBytes: len(wire),
		Tokens:    providers.EstimateRequestTokens(request),
	}, nil
}

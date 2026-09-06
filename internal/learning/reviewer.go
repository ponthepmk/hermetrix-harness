package learning

import (
	"context"
	"strings"
)

// Reviewer decides whether one unit of completed work holds knowledge worth
// keeping.
//
// Review must be safe to call from several goroutines at once. Corpus scoring
// runs reviews concurrently because they are independent and change nothing,
// and a reviewer holding unsynchronised state would corrupt itself rather than
// merely answer slowly.
type Reviewer interface {
	Revision() string
	Review(ctx context.Context, digest Digest) (Decision, error)
}

// StructuredReviewer is a deterministic first implementation. It promotes no
// knowledge. It only acknowledges an explicitly structured suggested skill;
// the runner still creates an untrusted candidate through the normal checks.
type StructuredReviewer struct{}

func (StructuredReviewer) Revision() string { return "structured-reviewer-v1" }

func (StructuredReviewer) Review(ctx context.Context, digest Digest) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if digest.SuggestedSkill == nil || strings.TrimSpace(digest.SuggestedSkill.Markdown) == "" {
		return Decision{Kind: "no_change", Reason: "digest contains no bounded, reusable procedure"}, nil
	}
	kind := digest.SuggestedSkill.ChangeKind
	if kind == "" {
		kind = "create"
	}
	// Design section 6.1: a Skill proposed from a turn nothing measured must
	// say so in its own reason, visible in Skill Studio without digging.
	// ModelReviewer gets this from an instruction it can only ask a model to
	// follow; this path takes a structured suggestion as given and never asks
	// anyone anything, so the same guarantee has to be deterministic here.
	// VerifiedBy citing real receipts, rather than a boolean, is what makes
	// this check honest -- an empty list means no run measured the outcome,
	// full stop, whatever the suggestion's own reason claims.
	//
	// This only touches a proposal. no_change already returned above: there
	// is no approach in play for "not verified by a run" to be a fact about.
	reason := strings.TrimSpace(digest.SuggestedSkill.Reason)
	if len(digest.VerifiedBy) == 0 {
		if reason != "" {
			reason += " "
		}
		reason += "This approach was not verified by a run."
	}
	return Decision{Kind: kind, Reason: reason,
		ExpectedBenefit: "reuse an explicitly observed procedure without changing active skills",
		Risks:           []string{"candidate requires lint, security checks, review, and explicit promotion"}}, nil
}

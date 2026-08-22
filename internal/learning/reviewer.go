package learning

import (
	"context"
	"strings"
)

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
	return Decision{Kind: kind, Reason: digest.SuggestedSkill.Reason,
		ExpectedBenefit: "reuse an explicitly observed procedure without changing active skills",
		Risks:           []string{"candidate requires lint, security checks, review, and explicit promotion"}}, nil
}

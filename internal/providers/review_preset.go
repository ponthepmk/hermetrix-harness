package providers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hermetrix-harness/internal/inference"
)

// A post-test decision is not a debugging/code-generation turn. Give its
// structured verdict a separate immutable contract, without overwriting old
// debug budgets or silently converting incomplete reviews into approval.
func (s *Service) ResolveCodeReviewPreset(ctx context.Context, profile Profile) (inference.Preset, error) {
	base, err := inference.LoadPreset(ctx, s.store.DB, "worker-debug", 1)
	if err != nil {
		return inference.Preset{}, err
	}
	cap := min(profile.MaxOutputTokens, 4096)
	if cap < 1 {
		return inference.Preset{}, fmt.Errorf("review output allowance is unavailable")
	}
	p := base
	p.Role = "worker-review"
	p.ReasoningMode = "disabled"
	p.ReasoningTokenCap = 0
	p.AnswerReserve = cap
	p.GenerationCap = cap
	p.ContextMax = min(base.ContextMax, profile.ContextWindow-cap-512)
	p.ContextTarget = min(base.ContextTarget, p.ContextMax)
	sum := sha256.Sum256([]byte(fmt.Sprintf("post-test-review-v1:%s:%d:%d", base.ContentDigest, cap, p.ContextMax)))
	p.ID = fmt.Sprintf("worker-review-%x", sum[:12])
	p.Revision = 1
	p.ContentDigest = ""
	if err = inference.ValidatePreset(p, profile.ContextWindow, profile.MaxOutputTokens, 512); err != nil {
		return inference.Preset{}, err
	}
	if existing, e := inference.LoadPreset(ctx, s.store.DB, p.ID, 1); e == nil {
		return existing, nil
	}
	result, err := inference.CreatePreset(ctx, s.store.DB, p)
	if err == nil {
		return result, nil
	}
	if existing, e := inference.LoadPreset(ctx, s.store.DB, p.ID, 1); e == nil {
		return existing, nil
	}
	return inference.Preset{}, err
}

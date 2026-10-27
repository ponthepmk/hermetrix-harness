package providers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hermetrix-harness/internal/inference"
)

// ResolveCodeProposalPreset preserves an immutable, auditable output contract.
// A profile allowing more than 8K must not silently dispatch the old 8K preset.
func (s *Service) ResolveCodeProposalPreset(ctx context.Context, profile Profile) (inference.Preset, error) {
	base, err := s.ResolveTaskPreset(ctx, profile, "worker-max", 1)
	if err != nil {
		return inference.Preset{}, err
	}
	cap := min(profile.MaxOutputTokens, 32768)
	if cap <= base.GenerationCap {
		return base, nil
	}
	available := profile.ContextWindow - cap - 512
	if available < 1 {
		return inference.Preset{}, fmt.Errorf("proposal output leaves no input budget")
	}
	p := base
	p.GenerationCap, p.AnswerReserve = cap, cap
	p.ContextMax = min(base.ContextMax, available)
	p.ContextTarget = min(base.ContextTarget, p.ContextMax)
	id := sha256.Sum256([]byte(fmt.Sprintf("proposal-budget-v1:%s:%d:%d", base.ContentDigest, cap, p.ContextMax)))
	p.ID, p.Revision, p.ContentDigest = fmt.Sprintf("worker-proposal-%x", id[:12]), 1, ""
	if err = inference.ValidatePreset(p, profile.ContextWindow, profile.MaxOutputTokens, 512); err != nil {
		return inference.Preset{}, err
	}
	if existing, loadErr := inference.LoadPreset(ctx, s.store.DB, p.ID, 1); loadErr == nil {
		return existing, nil
	}
	created, err := inference.CreatePreset(ctx, s.store.DB, p)
	if err == nil {
		return created, nil
	}
	// Concurrent requests may have persisted the same deterministic contract.
	if existing, loadErr := inference.LoadPreset(ctx, s.store.DB, p.ID, 1); loadErr == nil {
		return existing, nil
	}
	return inference.Preset{}, err
}

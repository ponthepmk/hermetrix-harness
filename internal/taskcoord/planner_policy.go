package taskcoord

import (
	"context"

	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/providers"
)

func (s *Service) selectPlannerPreset(ctx context.Context, profile providers.Profile, reviewRejected bool) (inference.Preset, error) {
	p, err := s.providers.ResolvePlannerPreset(ctx, profile, "planner", 1)
	if err != nil {
		return inference.Preset{}, err
	}
	if !reviewRejected {
		if providers.IsLocalProfile(profile) && profile.ContextWindow >= 65536 && profile.MaxOutputTokens >= 8192 &&
			(profile.ContextEvidence == "runtime_probe" || profile.ContextEvidence == "qualified") &&
			profile.RuntimeFingerprintID != "" {
			pl, err := s.providers.ResolvePlannerPreset(ctx, profile, "planner-large", 1)
			if err == nil && pl.AnswerReserve > p.AnswerReserve {
				return pl, nil
			}
		}
		return p, nil
	}
	pl, err := s.providers.ResolvePlannerPreset(ctx, profile, "planner-large", 1)
	if err == nil {
		return pl, nil
	}
	return p, nil
}

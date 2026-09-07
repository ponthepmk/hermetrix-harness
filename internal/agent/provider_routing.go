package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/providers"
)

type sessionProviderRoute struct {
	Policy     string
	Candidates []string
}

// routeSessionProvider performs failover only before the SessionContract is
// frozen. Once a session exists its provider stays sticky: replaying a failed
// sample against another model would change model/tool semantics underneath an
// already committed StepBinding.
func (s *Service) routeSessionProvider(ctx context.Context, input CreateSessionInput, profile ctxcompiler.Profile) (providers.Profile, QualificationBinding, sessionProviderRoute, error) {
	policy := strings.TrimSpace(input.RoutingPolicy)
	if policy == "" {
		policy = "explicit"
	}
	if policy != "explicit" && policy != "ordered-failover" {
		return providers.Profile{}, QualificationBinding{}, sessionProviderRoute{}, fmt.Errorf("unsupported provider routing policy %q", policy)
	}
	candidates := []string{}
	seen := map[string]bool{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			candidates = append(candidates, id)
		}
	}
	add(input.ProviderID)
	for _, id := range input.ProviderCandidates {
		add(id)
	}
	if policy == "explicit" && len(candidates) != 1 {
		return providers.Profile{}, QualificationBinding{}, sessionProviderRoute{}, fmt.Errorf("explicit provider routing requires exactly one provider_id")
	}
	if policy == "ordered-failover" && len(candidates) == 0 {
		profiles, err := s.providers.List(ctx)
		if err != nil {
			return providers.Profile{}, QualificationBinding{}, sessionProviderRoute{}, err
		}
		for _, provider := range profiles {
			add(provider.ID)
		}
	}
	if len(candidates) == 0 {
		return providers.Profile{}, QualificationBinding{}, sessionProviderRoute{}, fmt.Errorf("provider routing found no candidates")
	}
	rejections := []string{}
	for _, id := range candidates {
		provider, err := s.providers.Get(ctx, id)
		if err != nil {
			rejections = append(rejections, id+": missing")
			continue
		}
		if !provider.Enabled {
			rejections = append(rejections, provider.Name+": disabled")
			continue
		}
		if !provider.CredentialReady {
			rejections = append(rejections, provider.Name+": credential unavailable")
			continue
		}
		if profile.Total > provider.ContextWindow {
			rejections = append(rejections, fmt.Sprintf("%s: profile requires %d tokens but provider declares %d",
				provider.Name, profile.Total, provider.ContextWindow))
			continue
		}
		if budget := answerBudget(profile.OutputReserve, provider.ReasoningRatio); budget < minimumAnswerBudget {
			rejections = append(rejections, fmt.Sprintf("%s: measured reasoning leaves only %d answer tokens; choose a larger output reserve or a model that reasons less", provider.Name, budget))
			continue
		}
		qualification, err := s.resolveQualification(ctx, provider, profile, input.QualificationOverride)
		if err != nil {
			rejections = append(rejections, provider.Name+": "+err.Error())
			continue
		}
		return provider, qualification, sessionProviderRoute{Policy: policy, Candidates: candidates}, nil
	}
	return providers.Profile{}, QualificationBinding{}, sessionProviderRoute{},
		fmt.Errorf("no provider candidate can open %s: %s", profile.Name, strings.Join(rejections, "; "))
}

func (s *Service) resolveQualification(ctx context.Context, provider providers.Profile, profile ctxcompiler.Profile,
	override *QualificationOverrideInput) (QualificationBinding, error) {
	providerRevision := providers.Revision(provider)
	binding := QualificationBinding{ProviderRevision: providerRevision, ContextProfile: profile.Name}
	if profile.Name == "compact-32k" {
		binding.Mode = "compatibility"
		return binding, nil
	}
	var runID string
	err := s.store.DB.QueryRowContext(ctx, `SELECT id FROM model_qualification_runs
		WHERE provider_id=? AND model=? AND provider_revision=? AND requested_profile=?
		AND state='completed' AND eligible=1 ORDER BY completed_at DESC LIMIT 1`, provider.ID, provider.Model,
		providerRevision, profile.Name).Scan(&runID)
	if err == nil {
		binding.Mode, binding.RunID = "qualified", runID
		return binding, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return QualificationBinding{}, fmt.Errorf("load model qualification: %w", err)
	}
	if override == nil {
		return QualificationBinding{}, fmt.Errorf("context profile %s requires an exact eligible qualification for provider/model revision %s; run qualification or submit an explicit reviewed override", profile.Name, providerRevision)
	}
	actor := strings.TrimSpace(override.Actor)
	reason := strings.TrimSpace(override.Reason)
	if actor == "" || reason == "" {
		return QualificationBinding{}, fmt.Errorf("qualification override requires actor and reason")
	}
	if utf8.RuneCountInString(actor) > 120 || utf8.RuneCountInString(reason) > 1000 {
		return QualificationBinding{}, fmt.Errorf("qualification override actor/reason is too long")
	}
	expires := time.Now().UTC().Add(24 * time.Hour)
	binding.Mode, binding.Actor, binding.Reason, binding.ExpiresAt = "explicit_override", actor, reason, &expires
	return binding, nil
}

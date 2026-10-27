package providers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"hermetrix-harness/internal/inference"
)

// ResolvePlannerPreset returns the planner preset for the current profile.
// It first tries the existing task preset; only when that fails does it derive
// a smaller output-fit contract that keeps the planner role and schema intact.
func (s *Service) ResolvePlannerPreset(ctx context.Context, profile Profile, id string, revision int) (inference.Preset, error) {
	base, err := inference.LoadPreset(ctx, s.store.DB, id, revision)
	if err != nil {
		return inference.Preset{}, err
	}
	if base.Role != "planner" {
		return inference.Preset{}, fmt.Errorf("planner preset %s@%d has role %q, expected planner", id, revision, base.Role)
	}

	existing, err := s.ResolveTaskPreset(ctx, profile, id, revision)
	if err == nil {
		return existing, nil
	}

	if profile.MaxOutputTokens <= 0 || profile.MaxOutputTokens >= base.GenerationCap ||
		base.GenerationCap < base.ReasoningTokenCap+base.AnswerReserve {
		return inference.Preset{}, err
	}

	cap := profile.MaxOutputTokens
	derived := base
	derived.Role = base.Role
	derived.ReasoningMode = "disabled"
	derived.ReasoningTokenCap = 0
	derived.AnswerReserve = cap
	derived.GenerationCap = cap
	available := profile.ContextWindow - cap - 512
	if available < 1 {
		return inference.Preset{}, fmt.Errorf("planner output leaves no input budget")
	}
	derived.ContextMax = min(base.ContextMax, available)
	derived.ContextTarget = min(base.ContextTarget, derived.ContextMax)
	derived.Revision = 1
	derived.ContentDigest = ""

	sum := sha256.Sum256([]byte(fmt.Sprintf("planner-output-fit-v1:%s:%d:%d", base.ContentDigest, cap, derived.ContextMax)))
	derived.ID = fmt.Sprintf("planner-output-fit-%x", sum[:12])

	if err = inference.ValidatePreset(derived, profile.ContextWindow, profile.MaxOutputTokens, 512); err != nil {
		return inference.Preset{}, err
	}

	loadExisting := func() (inference.Preset, error) {
		existing, loadErr := inference.LoadPreset(ctx, s.store.DB, derived.ID, derived.Revision)
		if loadErr != nil {
			return inference.Preset{}, loadErr
		}
		comparison := existing
		comparison.ContentDigest = ""
		if comparison != derived {
			return inference.Preset{}, fmt.Errorf("derived planner preset identity has a conflicting immutable contract")
		}
		return existing, nil
	}

	if existing, loadErr := loadExisting(); loadErr == nil {
		return existing, nil
	} else if !errors.Is(loadErr, sql.ErrNoRows) {
		return inference.Preset{}, loadErr
	}

	created, err := inference.CreatePreset(ctx, s.store.DB, derived)
	if err == nil {
		return created, nil
	}

	if existing, loadErr := loadExisting(); loadErr == nil {
		return existing, nil
	}
	return inference.Preset{}, err
}

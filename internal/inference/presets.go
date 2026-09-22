package inference

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Preset struct {
	ID                string  `json:"id"`
	Revision          int     `json:"revision"`
	Role              string  `json:"role"`
	ContextTarget     int     `json:"context_target"`
	ContextMax        int     `json:"context_max"`
	ReasoningMode     string  `json:"reasoning_mode"`
	ReasoningTokenCap int     `json:"reasoning_token_cap"`
	AnswerReserve     int     `json:"answer_reserve"`
	GenerationCap     int     `json:"generation_cap"`
	Temperature       float64 `json:"temperature"`
	SchemaVersion     int     `json:"schema_version"`
	ContentDigest     string  `json:"content_digest"`
}

func ListPresets(ctx context.Context, db *sql.DB) ([]Preset, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,revision,role,context_target,context_max,reasoning_mode,reasoning_token_cap,
		answer_reserve,generation_cap,temperature,schema_version,content_digest FROM inference_presets ORDER BY id,revision DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Preset{}
	for rows.Next() {
		var item Preset
		if err = rows.Scan(&item.ID, &item.Revision, &item.Role, &item.ContextTarget, &item.ContextMax, &item.ReasoningMode,
			&item.ReasoningTokenCap, &item.AnswerReserve, &item.GenerationCap, &item.Temperature, &item.SchemaVersion, &item.ContentDigest); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CreatePreset appends an immutable revision. The database triggers reject
// update/delete so historical session and attempt snapshots remain auditable.
func CreatePreset(ctx context.Context, db *sql.DB, input Preset) (Preset, error) {
	input.ID, input.Role, input.ReasoningMode = strings.TrimSpace(input.ID), strings.TrimSpace(input.Role), strings.TrimSpace(input.ReasoningMode)
	if input.ID == "" || len(input.ID) > 80 || input.Role == "" || len(input.Role) > 80 {
		return Preset{}, fmt.Errorf("preset id and role are required and bounded")
	}
	if input.ReasoningMode != "disabled" && input.ReasoningMode != "bounded" {
		return Preset{}, fmt.Errorf("reasoning_mode must be disabled or bounded")
	}
	if input.ContextTarget < 1 || input.ContextMax < input.ContextTarget || input.GenerationCap < 1 || input.AnswerReserve < 1 || input.ReasoningTokenCap < 0 || input.GenerationCap < input.AnswerReserve+input.ReasoningTokenCap {
		return Preset{}, fmt.Errorf("preset context and generation budgets are invalid")
	}
	if input.Temperature < 0 || input.Temperature > 2 {
		return Preset{}, fmt.Errorf("preset temperature must be between 0 and 2")
	}
	if input.SchemaVersion == 0 {
		input.SchemaVersion = 1
	}
	if input.SchemaVersion != 1 {
		return Preset{}, fmt.Errorf("unsupported preset schema version")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Preset{}, err
	}
	defer tx.Rollback()
	var next int
	if input.Revision > 0 {
		next = input.Revision
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM inference_presets WHERE id=? AND revision=?`, input.ID, next).Scan(&exists); err != nil {
			return Preset{}, err
		}
		if exists > 0 {
			return Preset{}, fmt.Errorf("preset revision already exists")
		}
	} else {
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM inference_presets WHERE id=?`, input.ID).Scan(&next); err != nil {
			return Preset{}, err
		}
	}
	input.Revision = next
	digestInput := struct {
		ID                string  `json:"id"`
		Revision          int     `json:"revision"`
		Role              string  `json:"role"`
		ContextTarget     int     `json:"context_target"`
		ContextMax        int     `json:"context_max"`
		ReasoningMode     string  `json:"reasoning_mode"`
		ReasoningTokenCap int     `json:"reasoning_token_cap"`
		AnswerReserve     int     `json:"answer_reserve"`
		GenerationCap     int     `json:"generation_cap"`
		Temperature       float64 `json:"temperature"`
		SchemaVersion     int     `json:"schema_version"`
	}{input.ID, input.Revision, input.Role, input.ContextTarget, input.ContextMax, input.ReasoningMode, input.ReasoningTokenCap, input.AnswerReserve, input.GenerationCap, input.Temperature, input.SchemaVersion}
	encoded, _ := json.Marshal(digestInput)
	sum := sha256.Sum256(encoded)
	input.ContentDigest = hex.EncodeToString(sum[:])
	_, err = tx.ExecContext(ctx, `INSERT INTO inference_presets(id,revision,role,context_target,context_max,reasoning_mode,reasoning_token_cap,answer_reserve,generation_cap,temperature,schema_version,content_digest,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, input.ID, input.Revision, input.Role, input.ContextTarget, input.ContextMax, input.ReasoningMode, input.ReasoningTokenCap, input.AnswerReserve, input.GenerationCap, input.Temperature, input.SchemaVersion, input.ContentDigest, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return Preset{}, err
	}
	if err = tx.Commit(); err != nil {
		return Preset{}, err
	}
	return input, nil
}

func LoadPreset(ctx context.Context, db *sql.DB, id string, revision int) (Preset, error) {
	var item Preset
	err := db.QueryRowContext(ctx, `SELECT id,revision,role,context_target,context_max,reasoning_mode,reasoning_token_cap,
		answer_reserve,generation_cap,temperature,schema_version,content_digest FROM inference_presets WHERE id=? AND revision=?`,
		id, revision).Scan(&item.ID, &item.Revision, &item.Role, &item.ContextTarget, &item.ContextMax, &item.ReasoningMode,
		&item.ReasoningTokenCap, &item.AnswerReserve, &item.GenerationCap, &item.Temperature, &item.SchemaVersion, &item.ContentDigest)
	if err != nil {
		return Preset{}, fmt.Errorf("load inference preset %s@%d: %w", id, revision, err)
	}
	return item, nil
}

// ResolvePresetForContext keeps the role's output and reasoning reservation
// intact while reducing its input ceiling to the configured runtime window.
// The reduced contract has its own immutable identity; the original revision
// and historical usage are never rewritten or silently reinterpreted.
func ResolvePresetForContext(ctx context.Context, db *sql.DB, id string, revision, contextWindow, outputCap, safetyReserve int) (Preset, error) {
	base, err := LoadPreset(ctx, db, id, revision)
	if err != nil {
		return Preset{}, err
	}
	available := contextWindow - base.GenerationCap - safetyReserve
	if base.ContextMax <= available || available < 1 || base.GenerationCap > outputCap || base.GenerationCap < base.ReasoningTokenCap+base.AnswerReserve || safetyReserve < 0 {
		if safetyReserve < 0 {
			return Preset{}, fmt.Errorf("preset safety reserve cannot be negative")
		}
		if err = ValidatePreset(base, contextWindow, outputCap, safetyReserve); err != nil {
			return Preset{}, err
		}
		return base, nil
	}
	identity := sha256.Sum256([]byte(fmt.Sprintf("context-fit-v1:%s:%d:%s:%d", base.ID, base.Revision, base.ContentDigest, available)))
	derived := base
	derived.ID = fmt.Sprintf("%s-fit-%x", base.ID[:min(len(base.ID), 40)], identity[:12])
	derived.Revision, derived.ContextMax = 1, available
	derived.ContextTarget = min(base.ContextTarget, available)
	derived.ContentDigest = ""
	if err = ValidatePreset(derived, contextWindow, outputCap, safetyReserve); err != nil {
		return Preset{}, err
	}
	loadExisting := func() (Preset, error) {
		existing, loadErr := LoadPreset(ctx, db, derived.ID, derived.Revision)
		if loadErr != nil {
			return Preset{}, loadErr
		}
		comparison := existing
		comparison.ContentDigest = ""
		if comparison != derived {
			return Preset{}, fmt.Errorf("derived preset identity has a conflicting immutable contract")
		}
		return existing, nil
	}
	if existing, loadErr := loadExisting(); loadErr == nil {
		return existing, nil
	} else if !errors.Is(loadErr, sql.ErrNoRows) {
		return Preset{}, loadErr
	}
	created, err := CreatePreset(ctx, db, derived)
	if err == nil {
		return created, nil
	}
	// Another request may have persisted the same deterministic contract.
	if existing, loadErr := loadExisting(); loadErr == nil {
		return existing, nil
	}
	return Preset{}, err
}

func ValidatePreset(preset Preset, qualifiedContext, providerOutputCap, safetyReserve int) error {
	if preset.GenerationCap < preset.ReasoningTokenCap+preset.AnswerReserve {
		return fmt.Errorf("preset %s@%d generation cap %d is below reasoning plus answer reserve %d", preset.ID, preset.Revision,
			preset.GenerationCap, preset.ReasoningTokenCap+preset.AnswerReserve)
	}
	if preset.GenerationCap > providerOutputCap {
		return fmt.Errorf("preset %s@%d needs %d output tokens but provider permits %d", preset.ID, preset.Revision,
			preset.GenerationCap, providerOutputCap)
	}
	available := qualifiedContext - preset.GenerationCap - safetyReserve
	if available < 0 || preset.ContextMax > available {
		return fmt.Errorf("preset %s@%d input maximum %d exceeds effective context budget %d", preset.ID, preset.Revision,
			preset.ContextMax, available)
	}
	return nil
}

func EffectiveParameterDigest(preset Preset) string {
	payload := struct {
		PresetDigest               string  `json:"preset_digest"`
		MaxTokens                  int     `json:"max_tokens"`
		Temperature                float64 `json:"temperature"`
		ReasoningTransportRevision int     `json:"reasoning_transport_revision"`
	}{preset.ContentDigest, preset.GenerationCap, preset.Temperature, 1}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

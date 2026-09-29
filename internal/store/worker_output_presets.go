package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Large multi-file proposals need room for the complete structured tool call.
// Keep the original worker preset immutable for existing attempt snapshots.
func migrateV67(ctx context.Context, tx *sql.Tx) error {
	type preset struct {
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
	}
	for _, item := range []preset{
		{"worker-medium", 1, "worker", 4000, 30000, "disabled", 0, 3072, 3072, 0.1, 1},
		{"worker-large", 1, "worker", 8000, 30000, "disabled", 0, 6144, 6144, 0.1, 1},
	} {
		encoded, _ := json.Marshal(item)
		sum := sha256.Sum256(encoded)
		digest := hex.EncodeToString(sum[:])
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO inference_presets(id,revision,role,context_target,context_max,reasoning_mode,
			reasoning_token_cap,answer_reserve,generation_cap,temperature,schema_version,content_digest,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.Revision, item.Role, item.ContextTarget, item.ContextMax, item.ReasoningMode,
			item.ReasoningTokenCap, item.AnswerReserve, item.GenerationCap, item.Temperature, item.SchemaVersion,
			digest, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		var actual string
		if err := tx.QueryRowContext(ctx, `SELECT content_digest FROM inference_presets WHERE id=? AND revision=?`, item.ID, item.Revision).Scan(&actual); err != nil {
			return err
		}
		if actual != digest {
			return fmt.Errorf("existing preset %s@%d conflicts with migration", item.ID, item.Revision)
		}
	}
	return nil
}

func migrateV68(ctx context.Context, tx *sql.Tx) error {
	item := struct {
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
	}{"worker-max", 1, "worker", 7000, 30000, "disabled", 0, 8192, 8192, 0.1, 1}
	encoded, _ := json.Marshal(item)
	sum := sha256.Sum256(encoded)
	digest := hex.EncodeToString(sum[:])
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO inference_presets(id,revision,role,context_target,context_max,reasoning_mode,
		reasoning_token_cap,answer_reserve,generation_cap,temperature,schema_version,content_digest,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.Revision, item.Role, item.ContextTarget, item.ContextMax, item.ReasoningMode,
		item.ReasoningTokenCap, item.AnswerReserve, item.GenerationCap, item.Temperature, item.SchemaVersion,
		digest, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	var actual string
	if err := tx.QueryRowContext(ctx, `SELECT content_digest FROM inference_presets WHERE id=? AND revision=?`, item.ID, item.Revision).Scan(&actual); err != nil {
		return err
	}
	if actual != digest {
		return fmt.Errorf("existing preset %s@%d conflicts with migration", item.ID, item.Revision)
	}
	return nil
}

func migrateV69(ctx context.Context, tx *sql.Tx) error {
	item := struct {
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
	}{"planner-large", 1, "planner", 7000, 30000, "disabled", 0, 8192, 8192, 0.2, 1}
	encoded, _ := json.Marshal(item)
	sum := sha256.Sum256(encoded)
	digest := hex.EncodeToString(sum[:])
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO inference_presets(id,revision,role,context_target,context_max,reasoning_mode,
		reasoning_token_cap,answer_reserve,generation_cap,temperature,schema_version,content_digest,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.Revision, item.Role, item.ContextTarget, item.ContextMax, item.ReasoningMode,
		item.ReasoningTokenCap, item.AnswerReserve, item.GenerationCap, item.Temperature, item.SchemaVersion,
		digest, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	var actual string
	if err := tx.QueryRowContext(ctx, `SELECT content_digest FROM inference_presets WHERE id=? AND revision=?`, item.ID, item.Revision).Scan(&actual); err != nil {
		return err
	}
	if actual != digest {
		return fmt.Errorf("existing preset %s@%d conflicts with migration", item.ID, item.Revision)
	}
	return nil
}

package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

func behavioralHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func isLowerHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < 64; i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func isTrimExactUTF8(s string, maxLen int) bool {
	if s != strings.TrimSpace(s) {
		return false
	}
	if len(s) > maxLen {
		return false
	}
	return utf8.ValidString(s)
}

func validateBehavioralRunBindings(b BehavioralRunBindings) error {
	if !isTrimExactUTF8(b.ProviderID, 160) || b.ProviderID == "" {
		return fmt.Errorf("%w: provider_id", ErrBehavioralRunBindings)
	}
	if !isTrimExactUTF8(b.ProviderRevision, 160) || b.ProviderRevision == "" {
		return fmt.Errorf("%w: provider_revision", ErrBehavioralRunBindings)
	}
	if !isTrimExactUTF8(b.PresetID, 160) || b.PresetID == "" {
		return fmt.Errorf("%w: preset_id", ErrBehavioralRunBindings)
	}
	if !isTrimExactUTF8(b.EvaluatorRevision, 160) || b.EvaluatorRevision == "" {
		return fmt.Errorf("%w: evaluator_revision", ErrBehavioralRunBindings)
	}
	if !isTrimExactUTF8(b.Model, 240) || b.Model == "" {
		return fmt.Errorf("%w: model", ErrBehavioralRunBindings)
	}
	if b.PresetRevision <= 0 {
		return fmt.Errorf("%w: preset_revision", ErrBehavioralRunBindings)
	}
	if b.ProjectID != "" && !isTrimExactUTF8(b.ProjectID, 160) {
		return fmt.Errorf("%w: project_id", ErrBehavioralRunBindings)
	}
	if b.SessionID != "" && !isTrimExactUTF8(b.SessionID, 160) {
		return fmt.Errorf("%w: session_id", ErrBehavioralRunBindings)
	}
	if b.DatasetSplit != "discovery" && b.DatasetSplit != "validation" && b.DatasetSplit != "holdout" {
		return fmt.Errorf("%w: dataset_split", ErrBehavioralRunBindings)
	}
	for _, f := range []string{
		b.ModelConfigSHA256, b.CatalogSHA256, b.DatasetSHA256,
		b.EnvironmentSHA256, b.BudgetSHA256,
	} {
		if !isLowerHex64(f) {
			return fmt.Errorf("%w: sha256 field", ErrBehavioralRunBindings)
		}
	}
	if len(b.CaseIDs) < 1 || len(b.CaseIDs) > 256 {
		return fmt.Errorf("%w: case_ids", ErrBehavioralRunBindings)
	}
	seen := make(map[string]struct{}, len(b.CaseIDs))
	for _, id := range b.CaseIDs {
		if !isTrimExactUTF8(id, 160) || id == "" {
			return fmt.Errorf("%w: case_id", ErrBehavioralRunBindings)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%w: duplicate case_id", ErrBehavioralRunBindings)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func behavioralManifestJSON(m BehavioralRunManifest) ([]byte, string, error) {
	if err := validateBehavioralRunBindings(m.Bindings); err != nil {
		return nil, "", err
	}
	if !isTrimExactUTF8(m.CandidateID, 160) || m.CandidateID == "" {
		return nil, "", fmt.Errorf("%w: candidate_id", ErrBehavioralRunBindings)
	}
	if !isTrimExactUTF8(m.OwnerPrincipalID, 160) || m.OwnerPrincipalID == "" {
		return nil, "", fmt.Errorf("%w: owner_principal_id", ErrBehavioralRunBindings)
	}
	if m.CandidateRevision <= 0 {
		return nil, "", fmt.Errorf("%w: candidate_revision", ErrBehavioralRunBindings)
	}
	if !isLowerHex64(m.CandidateHash) || !isLowerHex64(m.CandidateBlobRef) {
		return nil, "", fmt.Errorf("%w: candidate hash/blob", ErrBehavioralRunBindings)
	}
	if m.BaseVersionID == "" {
		if m.BaseHash != "" || m.BaseBlobRef != "" {
			return nil, "", fmt.Errorf("%w: base hash/blob", ErrBehavioralRunBindings)
		}
	} else {
		if !isTrimExactUTF8(m.BaseVersionID, 160) || m.BaseVersionID == "" {
			return nil, "", fmt.Errorf("%w: base_version_id", ErrBehavioralRunBindings)
		}
		if !isLowerHex64(m.BaseHash) || !isLowerHex64(m.BaseBlobRef) {
			return nil, "", fmt.Errorf("%w: base hash/blob", ErrBehavioralRunBindings)
		}
	}
	if !isTrimExactUTF8(m.ScopeKind, 32) || m.ScopeKind == "" {
		return nil, "", fmt.Errorf("%w: scope_kind", ErrBehavioralRunBindings)
	}
	if !isTrimExactUTF8(m.ScopeRef, 512) {
		return nil, "", fmt.Errorf("%w: scope_ref", ErrBehavioralRunBindings)
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		return nil, "", fmt.Errorf("%w: marshal", ErrBehavioralRunBindings)
	}
	if len(encoded) > 64*1024 {
		return nil, "", fmt.Errorf("%w: manifest too large", ErrBehavioralRunBindings)
	}
	return encoded, behavioralHash(encoded), nil
}

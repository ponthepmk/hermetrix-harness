package skills

import (
	"encoding/json"
	"fmt"
)

func decodeBehavioralRunManifest(body, expectedHash string) (BehavioralRunManifest, error) {
	var m BehavioralRunManifest
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return BehavioralRunManifest{}, fmt.Errorf("decode behavioral run manifest: %w", ErrBehavioralRunBindings)
	}
	canonical, actualHash, err := behavioralManifestJSON(m)
	if err != nil {
		return BehavioralRunManifest{}, fmt.Errorf("validate behavioral run manifest: %w", err)
	}
	if !isLowerHex64(expectedHash) {
		return BehavioralRunManifest{}, fmt.Errorf("expectedHash must be lower hex 64: %w", ErrBehavioralRunBindings)
	}
	if actualHash != expectedHash {
		return BehavioralRunManifest{}, fmt.Errorf("manifest hash mismatch: %w", ErrBehavioralRunBindings)
	}
	if string(canonical) != body {
		return BehavioralRunManifest{}, fmt.Errorf("manifest canonical bytes mismatch: %w", ErrBehavioralRunBindings)
	}
	return m, nil
}

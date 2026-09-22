//go:build !windows

package secrets

import "encoding/json"

func decodeVault(data []byte) (map[string]string, bool, error) {
	values := map[string]string{}
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, false, err
	}
	if values == nil {
		values = map[string]string{}
	}
	return values, false, nil
}

func encodeVault(values map[string]string) ([]byte, error) {
	return json.MarshalIndent(values, "", "  ")
}

func vaultProtection() string { return "posix-owner-only" }

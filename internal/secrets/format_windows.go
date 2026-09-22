//go:build windows

package secrets

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsVaultEnvelope struct {
	Format     int               `json:"format"`
	Protection string            `json:"protection"`
	Values     map[string]string `json:"values"`
}

func decodeVault(data []byte) (map[string]string, bool, error) {
	var marker struct {
		Format int `json:"format"`
	}
	if err := json.Unmarshal(data, &marker); err != nil {
		return nil, false, err
	}
	if marker.Format == 0 {
		legacy := map[string]string{}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, false, err
		}
		return legacy, true, nil
	}
	var envelope windowsVaultEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, false, err
	}
	if envelope.Format != 2 || envelope.Protection != "windows-dpapi-current-user" || envelope.Values == nil {
		return nil, false, fmt.Errorf("unsupported Windows credential vault format")
	}
	values := make(map[string]string, len(envelope.Values))
	for ref, encoded := range envelope.Values {
		ciphertext, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, false, fmt.Errorf("decode protected credential %s: %w", ref, err)
		}
		plaintext, err := dpapiUnprotect(ciphertext)
		if err != nil {
			return nil, false, fmt.Errorf("unprotect credential %s: %w", ref, err)
		}
		values[ref] = string(plaintext)
	}
	return values, false, nil
}

func encodeVault(values map[string]string) ([]byte, error) {
	protected := make(map[string]string, len(values))
	for ref, value := range values {
		ciphertext, err := dpapiProtect([]byte(value))
		if err != nil {
			return nil, fmt.Errorf("protect credential %s: %w", ref, err)
		}
		protected[ref] = base64.StdEncoding.EncodeToString(ciphertext)
	}
	return json.MarshalIndent(windowsVaultEnvelope{Format: 2, Protection: "windows-dpapi-current-user", Values: protected}, "", "  ")
}

func dpapiProtect(plaintext []byte) ([]byte, error) {
	input := dataBlob(plaintext)
	var output windows.DataBlob
	if err := windows.CryptProtectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}

func dpapiUnprotect(ciphertext []byte) ([]byte, error) {
	input := dataBlob(ciphertext)
	var output windows.DataBlob
	if err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}

func dataBlob(value []byte) windows.DataBlob {
	if len(value) == 0 {
		return windows.DataBlob{}
	}
	return windows.DataBlob{Size: uint32(len(value)), Data: &value[0]}
}

func vaultProtection() string { return "windows-dpapi-current-user" }

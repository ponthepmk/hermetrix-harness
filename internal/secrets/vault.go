// Package secrets holds the API tokens a local-first install needs to reach a
// provider or an MCP server.
//
// Until now the only supported answer was "set an environment variable and
// restart the server", which meant a token could not be entered from the
// control center at all. The vault is the missing half: a token typed into the
// UI is written here, and an environment variable still works for anyone who
// prefers one, or who runs Hermetrix from a process manager that injects it.
//
// The file is the only place a token value is ever written. It never enters
// SQLite, a backup export, a log line or an API response -- the API reports
// only whether a credential is present.
package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileName is the vault's name inside the data directory. The backup exporter
// must never include it.
const FileName = "secrets.json"

// Vault is a process-wide map of secret reference to token value, persisted to
// one 0600 file. Callers address a token by a stable reference -- "provider:ID"
// or "mcp:ID" -- so a renamed profile keeps its credential.
type Vault struct {
	path   string
	mu     sync.RWMutex
	values map[string]string
}

// Ref builds the reference for one owner. Kind is "provider" or "mcp".
func Ref(kind, id string) string { return kind + ":" + id }

// Open reads the vault under dataRoot, creating neither the file nor the
// directory until something is stored. A malformed file is an error rather
// than a silent reset: quietly starting empty would look exactly like a
// working install whose every credential had vanished.
func Open(dataRoot string) (*Vault, error) {
	vault := &Vault{path: filepath.Join(dataRoot, FileName), values: map[string]string{}}
	data, err := os.ReadFile(vault.path)
	if errors.Is(err, os.ErrNotExist) {
		return vault, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read credential vault: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return vault, nil
	}
	if err := json.Unmarshal(data, &vault.values); err != nil {
		return nil, fmt.Errorf("credential vault %s is not readable JSON: %w", vault.path, err)
	}
	if vault.values == nil {
		vault.values = map[string]string{}
	}
	return vault, nil
}

// Get returns the stored token for ref. The second result is false for both a
// missing entry and a blank one, so a caller never has to test for empty.
func (v *Vault) Get(ref string) (string, bool) {
	if v == nil {
		return "", false
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	value, ok := v.values[ref]
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

// Has reports whether a token is stored, without handling the value.
func (v *Vault) Has(ref string) bool {
	_, ok := v.Get(ref)
	return ok
}

// Set stores a token. A blank token deletes the entry, which is what makes the
// UI's empty field mean "clear this credential" rather than "store nothing".
func (v *Vault) Set(ref, token string) error {
	if v == nil {
		return errors.New("credential vault is unavailable")
	}
	if strings.TrimSpace(ref) == "" {
		return errors.New("credential reference is required")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if strings.TrimSpace(token) == "" {
		delete(v.values, ref)
	} else {
		v.values[ref] = token
	}
	return v.persistLocked()
}

// Delete removes every token stored for ref.
func (v *Vault) Delete(ref string) error { return v.Set(ref, "") }

// persistLocked writes the whole map through a temporary file in the same
// directory, so a crash mid-write leaves the previous vault intact rather than
// a truncated one. The permissions are set before any secret is written.
func (v *Vault) persistLocked() error {
	directory := filepath.Dir(v.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create credential vault directory: %w", err)
	}
	encoded, err := json.MarshalIndent(v.values, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credential vault: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".secrets-*.tmp")
	if err != nil {
		return fmt.Errorf("create credential vault: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("restrict credential vault permissions: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return fmt.Errorf("write credential vault: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("flush credential vault: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close credential vault: %w", err)
	}
	if err := os.Rename(name, v.path); err != nil {
		return fmt.Errorf("replace credential vault: %w", err)
	}
	return os.Chmod(v.path, 0o600)
}

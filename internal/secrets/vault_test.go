package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultStoresReadsAndClearsATokenOnDisk(t *testing.T) {
	root := t.TempDir()
	vault, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	ref := Ref("provider", "p1")
	if vault.Has(ref) {
		t.Fatal("a fresh vault reported a stored credential")
	}
	if err := vault.Set(ref, "sk-secret-value"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	value, ok := reopened.Get(ref)
	if !ok || value != "sk-secret-value" {
		t.Fatalf("reopened vault = %q %v, want the stored token", value, ok)
	}
	// A blank token is how the UI clears a credential; storing "" would leave
	// an entry that reports ready while sending an empty Authorization header.
	if err := reopened.Set(ref, "   "); err != nil {
		t.Fatal(err)
	}
	if reopened.Has(ref) {
		t.Error("a blank token did not clear the credential")
	}
}

func TestVaultFileIsOwnerOnly(t *testing.T) {
	root := t.TempDir()
	vault, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Set(Ref("mcp", "m1"), "token"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("credential vault mode = %o, want 600", mode)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".secrets-") {
			t.Errorf("a temporary vault file was left behind: %s", entry.Name())
		}
	}
}

// TestVaultRefusesAMalformedFile keeps a corrupt vault from looking exactly
// like an install whose credentials were all deleted.
func TestVaultRefusesAMalformedFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, FileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("Open accepted a malformed credential vault")
	}
}

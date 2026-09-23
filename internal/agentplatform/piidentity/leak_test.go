package piidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLeakScanFindsPlaintextAndDigestWithoutEchoingThem(t *testing.T) {
	const token = "unit-test-secret-never-print-this-123456"
	caller := &MCPCaller{token: token}
	workspace, data := t.TempDir(), t.TempDir()
	if err := caller.ScanForLeak(workspace, data, []byte(`{"safe":true}`)); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{token, func() string { sum := sha256.Sum256([]byte(token)); return hex.EncodeToString(sum[:]) }()} {
		path := filepath.Join(workspace, "output.txt")
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		err := caller.ScanForLeak(workspace, data, nil)
		if err == nil {
			t.Fatal("leak was not found")
		}
		if strings.Contains(err.Error(), value) {
			t.Fatal("leak was echoed")
		}
	}
}

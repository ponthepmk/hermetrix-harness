//go:build windows

package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/capabilities"
	"hermetrix-harness/internal/store"
)

func TestLocalComfyLauncherUsesOnlyMatchingVirtualEnvironment(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".venv", "Scripts")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(root, "comfy-mcp.exe")
	for _, name := range []string{"comfy-mcp.exe", "comfy.exe", "python.exe"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := validateStdioCommand(launcher, nil); err != nil {
		t.Fatal(err)
	}
	values := stdioEnvironment(Server{Executable: launcher}, "")
	found := false
	for _, value := range values {
		if strings.EqualFold(value, "COMFY_BIN="+filepath.Join(root, "comfy.exe")) {
			found = true
		}
	}
	if !found {
		t.Fatal("Comfy child did not receive its matching CLI path")
	}
	if _, _, err := validateStdioCommand(filepath.Join(root, "arbitrary.exe"), nil); err == nil {
		t.Fatal("arbitrary executable accepted")
	}
	if err := os.Remove(filepath.Join(root, "comfy.exe")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateStdioCommand(launcher, nil); err == nil {
		t.Fatal("launcher accepted without matching CLI")
	}
}

// Opt-in smoke test against the user's installed first-party Comfy MCP. It
// verifies Hermetrix's own stdio process and discovery path, not a Codex host
// connection and not an actual video workflow.
func TestInstalledComfyMCPDiscovery(t *testing.T) {
	path := os.Getenv("HERMETRIX_COMFY_MCP_PATH")
	if path == "" {
		t.Skip("set HERMETRIX_COMFY_MCP_PATH for the installed local Comfy MCP")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dataStore, err := store.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := NewService(dataStore, capabilities.NewCatalog(), nil)
	defer service.Close()
	server, err := service.Save(ctx, SaveInput{Name: "Comfy Local", TransportKind: TransportStdio, Executable: path, ProtocolMode: ProtocolAuto, RequestTimeoutMS: 30000})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Discover(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tools < 5 {
		t.Fatalf("Comfy tool discovery returned %d tools", result.Tools)
	}
}

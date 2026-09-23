package piidentity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/mcp"
	"hermetrix-harness/internal/secrets"
)

func TestRealMCPClientIsRestrictedToFourPiReads(t *testing.T) {
	const token = "unit-test-credential-keep-private-12345"
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("missing scoped bearer")
		}
		var request struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "tools/list":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"whoami","inputSchema":{"type":"object"}},{"name":"get_node","inputSchema":{"type":"object"}},{"name":"get_agent","inputSchema":{"type":"object"}},{"name":"list_capabilities","inputSchema":{"type":"object"}},{"name":"add_task","inputSchema":{"type":"object"}}]}}`, request.ID)
		case "tools/call":
			calls = append(calls, request.Params.Name)
			body := validFake().responses[request.Params.Name]
			encoded, _ := json.Marshal(body)
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":%s}],"isError":false}}`, request.ID, encoded)
		default:
			t.Errorf("unexpected MCP method %s", request.Method)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	vault, err := secrets.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Set(mcp.CredentialRef(ServerID), token); err != nil {
		t.Fatal(err)
	}
	caller, err := NewMCPCaller(root, server.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	if _, err := caller.Call(context.Background(), "add_task", json.RawMessage(`{}`)); err == nil {
		t.Fatal("write tool allowed")
	}
	r, err := Probe(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	if !r.RealPiContacted || strings.Join(calls, ",") != "whoami,get_node,get_agent,list_capabilities" {
		t.Fatalf("unexpected calls %v", calls)
	}
}

func TestMissingVaultCredentialNeverUsesEnvironment(t *testing.T) {
	t.Setenv("KANBAN_WRITE_KEY", "legacy-key-that-must-not-be-used")
	_, err := NewMCPCaller(t.TempDir(), "http://127.0.0.1:8902/mcp")
	if err == nil || !strings.Contains(err.Error(), "credential missing") {
		t.Fatalf("missing credential did not fail closed: %v", err)
	}
}

func TestMalformedCredentialIsRejectedBeforeNetwork(t *testing.T) {
	root := t.TempDir()
	vault, err := secrets.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Set(mcp.CredentialRef(ServerID), "too-short"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMCPCaller(root, "http://127.0.0.1:8902/mcp"); err == nil || !strings.Contains(err.Error(), "format rejected") {
		t.Fatalf("malformed credential accepted: %v", err)
	}
}

func TestNetworkUnavailableAndTimeoutAreTransientWithoutCredentialLeak(t *testing.T) {
	const token = "unit-test-credential-keep-private-12345"
	root := t.TempDir()
	vault, err := secrets.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Set(mcp.CredentialRef(ServerID), token); err != nil {
		t.Fatal(err)
	}
	caller, err := NewMCPCaller(root, "http://127.0.0.1:1/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	_, err = caller.Call(context.Background(), "whoami", json.RawMessage(`{}`))
	if !errors.Is(err, ErrTransient) || strings.Contains(err.Error(), token) {
		t.Fatalf("bad network failure classification: %v", err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(100 * time.Millisecond) }))
	defer slow.Close()
	timed, err := NewMCPCaller(root, slow.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer timed.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = timed.Call(ctx, "whoami", json.RawMessage(`{}`))
	if !errors.Is(err, ErrTransient) || strings.Contains(err.Error(), token) {
		t.Fatalf("bad timeout classification: %v", err)
	}
}

func TestLegacyStreamableHTTPFullProbe(t *testing.T) {
	const token = "unit-test-legacy-credential-keep-private-12345"
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("legacy request lost credential")
		}
		var request struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.Method == "tools/list" && r.Header.Get("MCP-Protocol-Version") == mcp.ProtocolCurrent {
			http.Error(w, "server not initialized", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"Pi","version":"1"}}}`, request.ID)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"whoami","inputSchema":{"type":"object"}},{"name":"get_node","inputSchema":{"type":"object"}},{"name":"get_agent","inputSchema":{"type":"object"}},{"name":"list_capabilities","inputSchema":{"type":"object"}}]}}`, request.ID)
		case "tools/call":
			calls = append(calls, request.Params.Name)
			body := validFake().responses[request.Params.Name]
			encoded, _ := json.Marshal(body)
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":%s}],"isError":false}}`, request.ID, encoded)
		default:
			t.Errorf("unexpected method %s", request.Method)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	vault, err := secrets.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Set(mcp.CredentialRef(ServerID), token); err != nil {
		t.Fatal(err)
	}
	caller, err := NewMCPCaller(root, server.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	if _, err := Probe(context.Background(), caller); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "whoami,get_node,get_agent,list_capabilities" {
		t.Fatalf("legacy calls %v", calls)
	}
}

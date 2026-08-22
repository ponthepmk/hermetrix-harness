package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hermetrix-harness/internal/capabilities"
	"hermetrix-harness/internal/store"
)

func TestCurrentProtocolDiscoveryPaginationSSECallAndPersistence(t *testing.T) {
	t.Setenv("HERMETRIX_MCP_TEST_KEY", "mcp-runtime-secret")
	var listCalls, toolCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mcp-runtime-secret" {
			http.Error(w, "missing credential", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || !strings.Contains(r.Header.Get("Accept"), "application/json") ||
			!strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			http.Error(w, "bad transport envelope", http.StatusBadRequest)
			return
		}
		var request struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if r.Header.Get("MCP-Protocol-Version") != ProtocolCurrent || r.Header.Get("Mcp-Method") != request.Method {
			http.Error(w, "missing modern request metadata", http.StatusBadRequest)
			return
		}
		meta, _ := request.Params["_meta"].(map[string]any)
		if meta["io.modelcontextprotocol/protocolVersion"] != ProtocolCurrent {
			http.Error(w, "body/header version mismatch", http.StatusBadRequest)
			return
		}
		switch request.Method {
		case "tools/list":
			page := listCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			if page == 1 {
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"resultType":"complete","tools":[{"name":"echo","title":"Echo","description":"Echo text from a real MCP HTTP server","inputSchema":{"type":"object","properties":{"text":{"type":"string"},"region":{"type":"string","x-mcp-header":"Region"}},"required":["text"]},"outputSchema":{"type":"object","properties":{"echoed":{"type":"string"}},"required":["echoed"],"additionalProperties":false},"annotations":{"readOnlyHint":true,"openWorldHint":false}}],"nextCursor":"page-2"}}`, request.ID)
				return
			}
			if request.Params["cursor"] != "page-2" {
				t.Errorf("cursor = %v", request.Params["cursor"])
			}
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"resultType":"complete","tools":[{"name":"mutate","description":"Effectful tool","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":false,"destructiveHint":false}}]}}`, request.ID)
		case "tools/call":
			toolCalls.Add(1)
			if r.Header.Get("Mcp-Name") != "echo" {
				t.Errorf("Mcp-Name = %q", r.Header.Get("Mcp-Name"))
			}
			expectedRegion := "=?base64?" + base64.StdEncoding.EncodeToString([]byte("กรุงเทพ")) + "?="
			if r.Header.Get("Mcp-Param-Region") != expectedRegion {
				t.Errorf("Mcp-Param-Region = %q want %q", r.Header.Get("Mcp-Param-Region"), expectedRegion)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progress\":0.5}}\n\n")
			fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"resultType\":\"complete\",\"content\":[{\"type\":\"text\",\"text\":\"สวัสดี credential=mcp-runtime-secret\"}],\"structuredContent\":{\"echoed\":\"hello\"},\"isError\":false}}\n\n", request.ID)
		default:
			http.Error(w, "unknown method", http.StatusNotFound)
		}
	}))
	defer server.Close()

	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	catalog := capabilities.NewCatalog()
	service := NewService(dataStore, catalog, nil)
	profile, err := service.Save(context.Background(), SaveInput{Name: "current-test", Endpoint: server.URL,
		APIKeyEnv: "HERMETRIX_MCP_TEST_KEY", ProtocolMode: ProtocolCurrent, TrustAnnotations: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Discover(context.Background(), profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Protocol != ProtocolCurrent || result.Tools != 2 || result.Rejected != 0 || listCalls.Load() != 2 {
		t.Fatalf("discovery=%+v listCalls=%d", result, listCalls.Load())
	}
	search := catalog.Search("echo", "", 10)
	if len(search) != 1 || search[0].RequiresApproval || search[0].Effect != "read" {
		t.Fatalf("search = %+v", search)
	}
	searchJSON, _ := json.Marshal(search)
	if strings.Contains(string(searchJSON), "input_schema") || strings.Contains(string(searchJSON), "revision") {
		t.Fatalf("search leaked deferred schema/revision: %s", searchJSON)
	}
	entry, err := catalog.Describe(search[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalog.Call(context.Background(), entry.ID, entry.Revision,
		json.RawMessage(`{"region":"กรุงเทพ"}`)); err == nil || toolCalls.Load() != 0 {
		t.Fatalf("invalid arguments reached remote server: err=%v calls=%d", err, toolCalls.Load())
	}
	call, _, err := catalog.Call(context.Background(), entry.ID, entry.Revision,
		json.RawMessage(`{"text":"hello","region":"กรุงเทพ"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(call.Output, "สวัสดี") || strings.Contains(call.Output, "mcp-runtime-secret") ||
		!strings.Contains(call.Output, "[REDACTED]") || call.Metadata["automatic_retry"] != false || toolCalls.Load() != 1 {
		t.Fatalf("call=%+v toolCalls=%d", call, toolCalls.Load())
	}
	mutations := catalog.Search("mutate", "", 10)
	if len(mutations) != 1 || !mutations[0].RequiresApproval || mutations[0].Effect != "external_mutation" {
		t.Fatalf("mutation classification = %+v", mutations)
	}
	if _, _, err := catalog.Call(context.Background(), entry.ID, "stale", json.RawMessage(`{}`)); !errors.Is(err, capabilities.ErrRevisionConflict) {
		t.Fatalf("stale revision error = %v", err)
	}
	var storedEnv, endpoint string
	if err := dataStore.DB.QueryRow(`SELECT api_key_env,endpoint FROM mcp_servers WHERE id=?`, profile.ID).Scan(&storedEnv, &endpoint); err != nil {
		t.Fatal(err)
	}
	if storedEnv != "HERMETRIX_MCP_TEST_KEY" || endpoint != server.URL || strings.Contains(storedEnv, "runtime-secret") {
		t.Fatalf("unsafe secret persistence env=%q endpoint=%q", storedEnv, endpoint)
	}
	server.Close()
	if _, err := service.Discover(context.Background(), profile.ID); err == nil {
		t.Fatal("failed refresh unexpectedly succeeded")
	}
	stale, err := catalog.Describe(entry.ID)
	if err != nil || stale.Readiness != capabilities.ReadinessStale {
		t.Fatalf("failed atomic refresh left callable entry: %+v err=%v", stale, err)
	}

	reloadedCatalog := capabilities.NewCatalog()
	reloaded := NewService(dataStore, reloadedCatalog, nil)
	if err := reloaded.ReloadCatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reloadedCatalog.Summary().Total != 2 {
		t.Fatalf("persisted catalog did not reload: %+v", reloadedCatalog.Summary())
	}
}

func TestDeclaredOutputSchemaRejectsInvalidRealHTTPResult(t *testing.T) {
	var toolCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "tools/list":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"resultType":"complete","tools":[{"name":"typed","inputSchema":{"type":"object","additionalProperties":false},"outputSchema":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}}]}}`, request.ID)
		case "tools/call":
			toolCalls.Add(1)
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"resultType":"complete","structuredContent":{"value":42},"isError":false}}`, request.ID)
		default:
			http.Error(w, "unknown method", http.StatusNotFound)
		}
	}))
	defer server.Close()

	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	catalog := capabilities.NewCatalog()
	service := NewService(dataStore, catalog, nil)
	profile, err := service.Save(context.Background(), SaveInput{Name: "typed-output", Endpoint: server.URL,
		ProtocolMode: ProtocolCurrent, TrustAnnotations: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Discover(context.Background(), profile.ID); err != nil {
		t.Fatal(err)
	}
	entry, err := catalog.Describe(catalog.Search("typed", "", 1)[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = catalog.Call(context.Background(), entry.ID, entry.Revision, json.RawMessage(`{}`))
	var typed *Error
	if !errors.As(err, &typed) || typed.Kind != ErrorProtocol || typed.Operation != "validate tool result" || toolCalls.Load() != 1 {
		t.Fatalf("output validation error=%#v calls=%d", typed, toolCalls.Load())
	}
}

func TestAutoProtocolFallsBackToLegacyHandshakeWithoutRetryingToolCall(t *testing.T) {
	var modernProbe, initializeCount, initializedCount, callCount, deleteCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if r.Header.Get("Mcp-Session-Id") == "legacy-session" {
				deleteCount.Add(1)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var request struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if request.Method == "tools/list" && r.Header.Get("MCP-Protocol-Version") == ProtocolCurrent {
			modernProbe.Add(1)
			w.Header().Set("Content-Type", "text/plain")
			http.Error(w, "server not initialized", http.StatusBadRequest)
			return
		}
		switch request.Method {
		case "initialize":
			initializeCount.Add(1)
			w.Header().Set("Mcp-Session-Id", "legacy-session")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"%s","capabilities":{"tools":{}},"serverInfo":{"name":"legacy","version":"1"}}}`, request.ID, ProtocolLegacy)
		case "notifications/initialized":
			if r.Header.Get("Mcp-Session-Id") != "legacy-session" {
				t.Errorf("initialized notification missing session")
			}
			initializedCount.Add(1)
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if r.Header.Get("Mcp-Session-Id") != "legacy-session" || r.Header.Get("MCP-Protocol-Version") != ProtocolLegacy {
				t.Errorf("legacy list binding missing")
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"legacy_echo","description":"legacy echo","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}}]}}`, request.ID)
		case "tools/call":
			callCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"legacy ok"}],"isError":false}}`, request.ID)
		default:
			http.Error(w, "unknown", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	catalog := capabilities.NewCatalog()
	service := NewService(dataStore, catalog, nil)
	profile, err := service.Save(context.Background(), SaveInput{Name: "legacy-auto", Endpoint: server.URL,
		ProtocolMode: ProtocolAuto, TrustAnnotations: true})
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := service.Discover(context.Background(), profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if discovered.Protocol != ProtocolLegacy || modernProbe.Load() != 1 || initializeCount.Load() != 1 || initializedCount.Load() != 1 {
		t.Fatalf("fallback discovery=%+v probe=%d init=%d ready=%d", discovered, modernProbe.Load(), initializeCount.Load(), initializedCount.Load())
	}
	entry, err := catalog.Describe(catalog.Search("legacy_echo", "", 1)[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalog.Call(context.Background(), entry.ID, entry.Revision, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if callCount.Load() != 1 || initializeCount.Load() != 2 || initializedCount.Load() != 2 || modernProbe.Load() != 1 || deleteCount.Load() != 2 {
		t.Fatalf("unexpected lifecycle probe=%d init=%d ready=%d calls=%d delete=%d", modernProbe.Load(), initializeCount.Load(), initializedCount.Load(), callCount.Load(), deleteCount.Load())
	}
}

func TestTimeoutCancellationAndErrorTaxonomy(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	client := NewClient(nil)
	profile := Server{ID: "timeout-server", Endpoint: server.URL, ProtocolMode: ProtocolCurrent}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Millisecond)
	defer cancel()
	_, _, err := client.ListTools(ctx, profile, "")
	if err == nil {
		t.Fatal("expected timeout")
	}
	var typed *Error
	if !errors.As(err, &typed) || typed.Kind != ErrorTimeout || typed.Operation != "tools/list" {
		t.Fatalf("timeout taxonomy = %#v err=%v", typed, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request never reached real HTTP server")
	}

	cancelCtx, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	_, _, err = client.ListTools(cancelCtx, profile, "")
	if !errors.As(err, &typed) || typed.Kind != ErrorCancelled {
		t.Fatalf("cancellation taxonomy = %#v err=%v", typed, err)
	}
}

func TestInvalidCurrentHeaderAnnotationIsRejectedWithoutDroppingValidTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"resultType":"complete","tools":[{"name":"valid","inputSchema":{"type":"object"}},{"name":"invalid","inputSchema":{"type":"object","oneOf":[{"properties":{"x":{"type":"string","x-mcp-header":"Bad"}}}]}},{"name":"external_ref","inputSchema":{"$ref":"https://attacker.invalid/schema.json"}}]}}`, request.ID)
	}))
	defer server.Close()
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	catalog := capabilities.NewCatalog()
	service := NewService(dataStore, catalog, nil)
	profile, err := service.Save(context.Background(), SaveInput{Name: "invalid-header", Endpoint: server.URL, ProtocolMode: ProtocolCurrent})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Discover(context.Background(), profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tools != 1 || result.Rejected != 2 || catalog.Summary().Total != 1 {
		t.Fatalf("discovery = %+v summary=%+v", result, catalog.Summary())
	}
}

func TestServerValidationAndCredentialReadiness(t *testing.T) {
	t.Setenv("HERMETRIX_MISSING_MCP_KEY", "")
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := NewService(dataStore, capabilities.NewCatalog(), nil)
	if _, err := service.Save(context.Background(), SaveInput{Name: "bad", Endpoint: "http://example.com/mcp"}); err == nil {
		t.Fatal("non-loopback plaintext endpoint was accepted")
	}
	profile, err := service.Save(context.Background(), SaveInput{Name: "missing-key", Endpoint: "http://127.0.0.1:65530/mcp",
		APIKeyEnv: "HERMETRIX_MISSING_MCP_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.CredentialReady {
		t.Fatal("missing credential reported ready")
	}
	_, err = service.Discover(context.Background(), profile.ID)
	var typed *Error
	if !errors.As(err, &typed) || typed.Kind != ErrorNotReady {
		t.Fatalf("credential error = %#v %v", typed, err)
	}
	var count int
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM mcp_tools`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("credential failure created %d tools", count)
	}
}

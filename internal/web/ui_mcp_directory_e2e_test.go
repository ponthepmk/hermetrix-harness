package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hermetrix-harness/internal/mcp"
)

func TestMCPDirectoryInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	mcpRuntime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode MCP request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"resultType":"complete","tools":[{"name":"directory_echo","description":"Directory browser fixture","inputSchema":{"type":"object"}}]}}`, request.ID)
	}))
	defer mcpRuntime.Close()
	server := testHTTPServer(t)
	requestJSON(t, server.URL+"/api/projects", http.MethodPost, map[string]any{"name": "MCP directory project"}, http.StatusCreated)
	for _, item := range []struct{ name, endpoint string }{
		{"Ready directory", mcpRuntime.URL},
		{"Other server", "http://127.0.0.1:18444/mcp"},
	} {
		requestJSON(t, server.URL+"/api/mcp/servers", http.MethodPost, map[string]any{
			"name": item.name, "transport_kind": "streamable-http", "endpoint": item.endpoint,
			"protocol_mode": mcp.ProtocolCurrent, "request_timeout_ms": 5000,
		}, http.StatusCreated)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	debugURL, stop, err := startE2EChrome(ctx, chrome, filepath.Join(t.TempDir(), "chrome-profile"))
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	target, err := createE2ETarget(ctx, debugURL, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := &e2eCDPClient{connection: connection}
	if err := client.call(ctx, "Runtime.enable", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1280, "height": 800, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-open-project]').click(); 'opened'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#openConfig') && !document.querySelector('#appShell').hidden ? 'shell' : ''`, "shell")
	if _, err := client.evaluate(ctx, `document.querySelector('#openConfig').click(); 'settings'`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('[data-config-page="mcp"]')?.click(); 'mcp'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('.mcp-server-list > article').length.toString()`, "2")
	filtered, err := client.evaluate(ctx, `(() => {const input=document.querySelector('#mcpServerQuery');input.value='Ready directory';input.dispatchEvent(new Event('input',{bubbles:true}));return [...document.querySelectorAll('.mcp-server-list > article')].filter(el=>!el.hidden).map(el=>el.textContent.includes('Ready directory')).join(',')})()`)
	if err != nil || filtered != "true" {
		t.Fatalf("server search = %q, %v", filtered, err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('.mcp-server-list > article:not([hidden]) .mcp-server-detail summary').click(); 'opened'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.mcp-server-list > article:not([hidden]) .mcp-server-detail').open && document.querySelector('.mcp-server-list > article:not([hidden]) .mcp-server-detail [data-discover-mcp]') ? 'details' : ''`, "details")
	if directory := os.Getenv("HERMETRIX_E2E_SCREENSHOT_DIR"); directory != "" {
		waitBrowserValue(t, ctx, client, `getComputedStyle(document.querySelector('#configOverlay')).opacity === '1' ? 'settled' : ''`, "settled")
		var screenshot struct {
			Data string `json:"data"`
		}
		if err := client.call(ctx, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": false}, &screenshot); err != nil {
			t.Fatal(err)
		}
		data, err := base64.StdEncoding.DecodeString(screenshot.Data)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "mcp-directory.png")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Log("MCP screenshot:", path)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#mcpDiscoverAll').click(); 'discovering'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.mcp_servers.find(server => server.name === 'Ready directory')?.status === 'ready' ? 'ready' : ''`, "ready")
	query, err := client.evaluate(ctx, `document.querySelector('#mcpServerQuery')?.value || ''`)
	if err != nil || !strings.EqualFold(query, "Ready directory") {
		t.Fatalf("server query after discovery = %q, %v", query, err)
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	overflow, err := client.evaluate(ctx, `document.documentElement.scrollWidth > innerWidth ? 'overflow' : 'fits'`)
	if err != nil || overflow != "fits" {
		t.Fatalf("mobile MCP layout = %q, %v", overflow, err)
	}
}

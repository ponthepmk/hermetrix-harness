package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// This opt-in check uses the real TLS gateway, its existing login prompt and the
// running backend. The token is read from a private local file, never logged.
func TestTailnetPreviewLoginInRealBrowser(t *testing.T) {
	pageURL := os.Getenv("HERMETRIX_TAILNET_E2E_URL")
	if pageURL == "" {
		t.Skip("live tailnet gateway not selected")
	}
	token, err := os.ReadFile(os.Getenv("HERMETRIX_TAILNET_E2E_TOKEN_FILE"))
	if err != nil {
		t.Fatal("cannot read gateway login token")
	}
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Fatal("Chrome required for tailnet acceptance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	debugURL, stop, err := startE2EChrome(ctx, chrome, filepath.Join(t.TempDir(), "chrome-profile"))
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	target, err := createE2ETarget(ctx, debugURL, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := &e2eCDPClient{connection: connection}
	secretJSON, _ := json.Marshal(strings.TrimSpace(string(token)))
	for _, method := range []string{"Runtime.enable", "Page.enable"} {
		if err := client.call(ctx, method, map[string]any{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": "window.prompt = () => " + string(secretJSON) + ";"}, nil); err != nil {
		t.Fatal("cannot prepare login prompt")
	}
	if err := client.call(ctx, "Page.navigate", map[string]any{"url": pageURL}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.authPrincipal && state.projects.length > 0 && document.querySelector('[data-open-project]') ? 'authenticated-and-hydrated' : ''`, "authenticated-and-hydrated")
	waitBrowserValue(t, ctx, client, `document.querySelector('.picker-brand')?.naturalWidth > 0 ? 'logo-loaded' : ''`, "logo-loaded")
	if _, err := client.evaluate(ctx, `fetch('/api/auth/session').then(async r => { const b = await r.json(); window.tailnetCookieCheck = r.status===200 && b.principal===state.authPrincipal ? 'cookie-authenticated' : 'failed'; }); 'checking'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `window.tailnetCookieCheck || ''`, "cookie-authenticated")
	t.Log("HTTPS certificate trusted; existing login prompt, project hydration, assets and session cookie passed")
}

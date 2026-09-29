package web

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestBrowserInspectionFlowInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	server := testHTTPServer(t)
	requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Inspection fixture", "root_path": t.TempDir()}, http.StatusCreated)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	if err := client.call(ctx, "Page.enable", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-open-project]').click(); 'opened'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.currentProject?.name === 'Inspection fixture' ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, `openContentPane('browser'); 'pane-open'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#browserOpenForm') ? 'ready' : ''`, "ready")
	waitBrowserValue(t, ctx, client, `document.querySelector('#desktopListWindows') && document.querySelector('.desktop-observation') ? 'desktop-panel' : ''`, "desktop-panel")
	if _, err := client.evaluate(ctx, `document.querySelector('#desktopListWindows').click(); 'list-windows'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.desktopWindowsLoaded ? 'desktop-list-loaded' : ''`, "desktop-list-loaded")
	openExpression := fmt.Sprintf(`(() => {const form=document.querySelector('#browserOpenForm'); form.querySelector('[name="url"]').value=%q; form.querySelector('[name="allow_private"]').checked=true; form.requestSubmit(); return 'submitted';})()`, server.URL)
	if _, err := client.evaluate(ctx, openExpression); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-browser-action="inspect"]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-browser-action="inspect"]').click(); 'inspecting'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.inspect-section summary')?.textContent.includes('Observed fetch/XHR') ? 'ready' : ''`, "ready")
	visible := ""
	for attempt := 0; attempt < 8; attempt++ {
		visible, err = client.evaluate(ctx, `document.querySelector('.inspect-section')?.textContent || ''`)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(visible, "/api/projects") {
			break
		}
		time.Sleep(300 * time.Millisecond)
		if _, err := client.evaluate(ctx, `document.querySelector('[data-browser-action="inspect"]').click(); 'retry-inspect'`); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
	}
	if !strings.Contains(visible, "Passive page inspection") || !strings.Contains(visible, "Console errors seen in tab") || !strings.Contains(visible, "DOM excerpt") || !strings.Contains(visible, "/api/projects") {
		t.Fatalf("inspection panel did not show evidence sections: %q", visible)
	}
	for _, viewport := range []struct {
		name          string
		width, height int
	}{{"desktop", 1440, 900}, {"mobile", 390, 844}} {
		if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
			"width": viewport.width, "height": viewport.height, "deviceScaleFactor": 1, "mobile": viewport.name == "mobile"}, nil); err != nil {
			t.Fatal(err)
		}
		if viewport.name == "mobile" {
			if _, err := client.evaluate(ctx, `openContentPane('browser'); 'mobile-pane'`); err != nil {
				t.Fatal(err)
			}
		}
		waitBrowserValue(t, ctx, client, `document.querySelector('[data-browser-action="inspect"]') ? 'ready' : ''`, "ready")
		if _, err := client.evaluate(ctx, `(() => {document.querySelector('.pane[data-kind="browser"] [data-pane-max]')?.click(); const section=[...document.querySelectorAll('.inspect-section')].find(el=>el.textContent.includes('Passive page inspection')); section?.scrollIntoView({block:'start'}); return 'focused';})()`); err != nil {
			t.Fatal(err)
		}
		if outputDir := os.Getenv("HERMETRIX_E2E_SCREENSHOT_DIR"); outputDir != "" {
			var screenshot struct {
				Data string `json:"data"`
			}
			if err := client.call(ctx, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": false}, &screenshot); err != nil {
				t.Fatal(err)
			}
			image, err := base64.StdEncoding.DecodeString(screenshot.Data)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(outputDir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(outputDir, "browser-inspection-"+viewport.name+".png")
			if err := os.WriteFile(path, image, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Log("inspection screenshot:", path)
		}
	}
}

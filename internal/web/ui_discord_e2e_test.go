package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// This test configures only the isolated local service. It never saves a real
// token, follows the invite link, connects to Discord, or sends a message.
func TestDiscordSetupInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome is required for the UI E2E job")
		}
		t.Skip("Chrome is not installed")
	}
	handler := testHandler(t)
	var mu sync.Mutex
	mutations := make(map[string]int)
	failSave := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && len(r.URL.Path) >= len("/api/remote/discord") && r.URL.Path[:len("/api/remote/discord")] == "/api/remote/discord" {
			mu.Lock()
			mutations[r.Method+" "+r.URL.Path]++
			fail := r.Method == http.MethodPut && r.URL.Path == "/api/remote/discord" && failSave
			if fail {
				failSave = false
			}
			mu.Unlock()
			if fail {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"fixture save unavailable"}`))
				return
			}
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	requestJSON(t, server.URL+"/api/providers", http.MethodPost, map[string]any{
		"name": "Remote excluded", "adapter_kind": "openai-compatible", "base_url": "https://example.test/v1",
		"model": "remote-fixture", "context_window": 32768, "context_evidence": "declared", "max_output_tokens": 4096}, http.StatusCreated)
	providerBody := requestJSON(t, server.URL+"/api/providers", http.MethodPost, map[string]any{
		"name": "Local Discord fixture", "adapter_kind": "openai-compatible", "base_url": "http://127.0.0.1:8080/v1",
		"model": "local-fixture", "context_window": 32768, "context_evidence": "declared", "max_output_tokens": 4096}, http.StatusCreated)
	projectBody := requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Discord setup fixture", "root_path": t.TempDir()}, http.StatusCreated)
	var provider, project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(providerBody, &provider); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(projectBody, &project); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
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
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1280, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	evaluate := func(expression string) string {
		t.Helper()
		value, err := client.evaluate(ctx, expression)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	click := func(selector string) {
		t.Helper()
		geometry := evaluate(fmt.Sprintf(`(() => {const el=document.querySelector(%q);el.scrollIntoView({block:'center'});const r=el.getBoundingClientRect(),x=r.x+r.width/2,y=r.y+r.height/2,at=document.elementFromPoint(x,y);return JSON.stringify({x,y,reachable:!!at&&(at===el||el.contains(at))})})()`, selector))
		var point struct {
			X, Y      float64
			Reachable bool
		}
		if err := json.Unmarshal([]byte(geometry), &point); err != nil {
			t.Fatal(err)
		}
		if !point.Reachable {
			t.Fatalf("Discord setup control cannot be clicked: %s %s", selector, geometry)
		}
		for _, kind := range []string{"mousePressed", "mouseReleased"} {
			if err := client.call(ctx, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": point.X, "y": point.Y, "button": "left", "clickCount": 1}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'project' : ''`, "project")
	click("[data-open-project]")
	waitBrowserValue(t, ctx, client, `document.querySelector('#startPrompt') ? 'opened' : ''`, "opened")
	click("#openConfig")
	click(`[data-config-page="discord"]`)
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-discord-status]')?.textContent.includes('ยังไม่เปิดใช้งาน') ? 'disabled' : ''`, "disabled")
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('[name="provider_id"] option').length === 2 && !document.querySelector('[name="provider_id"]').textContent.includes('Remote excluded') ? 'local-only' : ''`, "local-only")
	evaluate(fmt.Sprintf(`(() => {const form=document.querySelector('[data-discord-scope]');for(const [name,value] of Object.entries({application_id:'123',guild_ids:'22345678901234567',channel_ids:'32345678901234567',user_ids:'42345678901234567',project_id:%q,provider_id:%q})){const el=form.elements.namedItem(name);el.value=value;el.dispatchEvent(new Event('input',{bubbles:true}));if(name==='provider_id')el.dispatchEvent(new Event('change',{bubbles:true}));}return 'filled';})()`, project.ID, provider.ID))
	click("[data-discord-save]")
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-discord-feedback]')?.textContent.includes('17–20') ? 'id-error' : ''`, "id-error")
	mu.Lock()
	invalidCalls := mutations["PUT /api/remote/discord"]
	mu.Unlock()
	if invalidCalls != 0 {
		t.Fatalf("invalid Application ID made %d API mutations", invalidCalls)
	}
	evaluate(`const appID=document.querySelector('[name="application_id"]');appID.value='12345678901234567';appID.dispatchEvent(new Event('input',{bubbles:true}));'valid-id'`)
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-discord-invite]')?.getAttribute('href') || ''`, "https://discord.com/oauth2/authorize?client_id=12345678901234567&scope=bot%20applications.commands&permissions=0&integration_type=0")
	click("[data-discord-save]")
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-discord-feedback]')?.textContent.includes('fixture save unavailable') && document.querySelector('[name="application_id"]').value==='12345678901234567' ? 'failed-draft-retained' : ''`, "failed-draft-retained")
	click("[data-discord-save]")
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-discord-feedback]')?.textContent.includes('บันทึกขอบเขตแล้ว') && document.querySelector('[data-discord-start]').disabled ? 'saved-not-connected' : ''`, "saved-not-connected")
	response, err := http.Get(server.URL + "/api/remote/discord")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var status struct {
		Config struct {
			Enabled       bool     `json:"enabled"`
			ApplicationID string   `json:"application_id"`
			ProjectID     string   `json:"project_id"`
			ProviderID    string   `json:"provider_id"`
			ChannelIDs    []string `json:"channel_ids"`
		} `json:"config"`
		TokenStored bool   `json:"token_stored"`
		State       string `json:"state"`
	}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Config.Enabled || status.TokenStored || status.State == "ready" || status.Config.ApplicationID != "12345678901234567" || status.Config.ProjectID != project.ID || status.Config.ProviderID != provider.ID || len(status.Config.ChannelIDs) != 1 {
		t.Fatalf("setup did not persist a disabled, tokenless scope: %+v", status)
	}
	// An unsaved password is never copied into setup state or replayed on render.
	evaluate(`document.querySelector('[data-discord-token]').value='fixture-not-a-real-token';'password-draft'`)
	click("[data-discord-refresh]")
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-discord-token]')?.value === '' ? 'password-cleared' : ''`, "password-cleared")
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	click("[data-discord-refresh]")
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-discord-token]') ? 'mobile-settings' : ''`, "mobile-settings")
	if problems := evaluate(`(() => {const nodes=[...document.querySelectorAll('#view-discord input,#view-discord select,#view-discord textarea,#view-discord button')];return JSON.stringify(nodes.map(el=>({name:el.name||el.textContent,r:el.getBoundingClientRect()})).filter(({r})=>r.width>0&&(r.left<0||r.right>innerWidth+1)).map(({name,r})=>({name,left:r.left,right:r.right})));})()`); problems != "[]" {
		t.Errorf("Discord setup controls overflow the 390px viewport: %s", problems)
	}
	mu.Lock()
	defer mu.Unlock()
	if mutations["PUT /api/remote/discord"] != 2 || len(mutations) != 1 {
		t.Fatalf("setup triggered an unexpected mutation (must never connect or save a token): %+v", mutations)
	}
}

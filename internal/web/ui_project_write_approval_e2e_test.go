package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Actual UI -> owner API -> persisted consent -> real file writes. The fixture
// controls model output so the permission path is reproducible, not probabilistic.
func TestProjectAutomaticWritesInRealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome required")
		}
		t.Skip("Chrome unavailable")
	}
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			tools := []any{}
			for index, name := range []string{"alpha", "beta"} {
				args, _ := json.Marshal(map[string]string{"path": name + ".txt", "content": name, "expected_sha256": "absent"})
				tools = append(tools, map[string]any{"index": index, "id": name, "type": "function", "function": map[string]string{"name": "workspace.write_file", "arguments": string(args)}})
			}
			body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": tools}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", body)
			return
		}
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"PROJECT_WRITES_OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer model.Close()
	configured := testConfiguredServer(t)
	server := httptest.NewServer(configured.Handler())
	defer server.Close()
	root := t.TempDir()
	var provider, project, session struct {
		ID string `json:"id"`
	}
	json.Unmarshal(requestJSON(t, server.URL+"/api/providers", http.MethodPost, map[string]any{"name": "Project write fixture", "adapter_kind": "openai-compatible", "base_url": model.URL + "/v1", "model": "write-fixture", "context_window": 65536, "context_evidence": "declared", "max_output_tokens": 4096}, http.StatusCreated), &provider)
	json.Unmarshal(requestJSON(t, server.URL+"/api/projects", http.MethodPost, map[string]any{"name": "Automatic project files", "root_path": root}, http.StatusCreated), &project)
	json.Unmarshal(requestJSON(t, server.URL+"/api/sessions", http.MethodPost, map[string]any{"title": "Manual permission baseline", "provider_id": provider.ID, "project_id": project.ID, "context_profile": "compact-32k"}, http.StatusCreated), &session)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	debugURL, stop, err := startE2EChrome(ctx, chrome, filepath.Join(t.TempDir(), "chrome"))
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	target, err := createE2ETarget(ctx, debugURL, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := &e2eCDPClient{connection: conn}
	eval := func(js string) string {
		t.Helper()
		value, err := client.evaluate(ctx, js)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	if err := client.call(ctx, "Runtime.enable", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `typeof state!=='undefined'&&state.providers.length?'loaded':''`, "loaded")
	eval(fmt.Sprintf(`selectSession(%q).then(()=>switchView('code'));'select'`, session.ID))
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-assistant-auto-write]')?'ready':''`, "ready")
	if eval(`document.querySelector('[data-assistant-auto-write]').checked?'on':'off'`) != "off" {
		t.Fatal("existing chat gained authority")
	}
	eval(`document.querySelector('[data-assistant-auto-write]').click();document.querySelector('.ide-assistant textarea').value='Create alpha.txt and beta.txt';document.querySelector('.ide-assistant textarea').dispatchEvent(new Event('input'));document.querySelector('.pane-chat-form').requestSubmit();'sent'`)
	waitControlValue(t, ctx, client, `state.sessionDetail?.session.state==='active'&&!state.sending&&state.sessionDetail.events.some(e=>e.role==='assistant'&&e.content==='PROJECT_WRITES_OK')?'done':''`, "done")
	if calls.Load() != 2 {
		t.Fatalf("model dispatches=%d want 2", calls.Load())
	}
	if eval(`JSON.stringify({newChat:state.sessionDetail.session.id!==`+fmt.Sprintf("%q", session.ID)+`,consent:!!state.sessionDetail.session.contract.workspace_write_approval,pending:state.sessionDetail.approvals.filter(a=>a.state==='pending').length})`) != "{\"newChat\":true,\"consent\":true,\"pending\":0}" {
		t.Fatal("consent was not scoped to a new completed chat")
	}
	for _, name := range []string{"alpha", "beta"} {
		content, err := os.ReadFile(filepath.Join(root, name+".txt"))
		if err != nil || string(content) != name {
			t.Fatalf("real file %s: %q %v", name, content, err)
		}
	}
	for _, width := range []int{390, 1440} {
		if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
			t.Fatal(err)
		}
		fit := eval(`(()=>{const b=document.querySelector('[data-assistant-auto-write]'),r=b.closest('label').getBoundingClientRect();return !b.disabled&&b.checked&&r.width>0&&r.right<=innerWidth&&r.bottom<=innerHeight&&document.documentElement.scrollWidth<=innerWidth?'fits':JSON.stringify({rect:r,disabled:b.disabled,viewport:innerWidth})})()`)
		if fit != "fits" {
			t.Fatalf("permission control at %d: %s", width, fit)
		}
		if dir := os.Getenv("HERMETRIX_PROJECT_WRITE_AUDIT_DIR"); dir != "" {
			var shot struct {
				Data string `json:"data"`
			}
			if err := client.call(ctx, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": false}, &shot); err != nil {
				t.Fatal(err)
			}
			pixels, err := base64.StdEncoding.DecodeString(shot.Data)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("project-writes-%d.png", width)), pixels, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Turning it off creates a new manual chat on the next send, not a silent
	// mutation of the previous session's immutable contract.
	eval(`document.querySelector('[data-assistant-auto-write]').click();document.querySelector('.ide-assistant textarea').value='Explain the files';document.querySelector('.ide-assistant textarea').dispatchEvent(new Event('input'));document.querySelector('.pane-chat-form').requestSubmit();'manual'`)
	waitControlValue(t, ctx, client, `state.sessionDetail?.session.state==='active'&&!state.sending&&!state.sessionDetail.session.contract.workspace_write_approval&&state.sessionDetail.events.some(e=>e.content==='PROJECT_WRITES_OK')?'manual':''`, "manual")
	t.Log("Checkbox -> new authorized chat -> two actual files -> no pending approvals -> new manual chat; local deterministic model fixture")
}

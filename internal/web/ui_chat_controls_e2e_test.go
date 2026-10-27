package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// This exercises actual controls and drafts against the owner-bound API.
// Optional live mode uses only a newly created synthetic session/project.
func TestChatStopQueueSteerInRealBrowser(t *testing.T) {
	t.Run("Chat", func(t *testing.T) { runChatControlsBrowser(t, false) })
	t.Run("Workspace", func(t *testing.T) { runChatControlsBrowser(t, true) })
}

func runChatControlsBrowser(t *testing.T, workspace bool) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome required")
		}
		t.Skip("Chrome unavailable")
	}
	liveURL := os.Getenv("HERMETRIX_CONTROLS_LIVE_URL")
	baseURL := liveURL
	modelURL, modelName := "", "control-fixture"
	var calls atomic.Int32
	liveModel := os.Getenv("HERMETRIX_CONTROLS_LIVE_MODEL") == "1"
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	if liveURL == "" {
		if !liveModel {
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/slots" {
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, `[{"id":0,"is_processing":false}]`)
					return
				}
				if r.URL.Path != "/v1/chat/completions" {
					http.NotFound(w, r)
					return
				}
				n := calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				if n == 1 {
					io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"CONTROL_ACCEPTANCE_OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer model.Close()
			modelURL = model.URL + "/v1"
		} else {
			modelURL = "http://127.0.0.1:8088/v1"
			modelName = `D:\llm-server\Bonsai-demo\models\bonsai2-gguf\27B\Ternary-Bonsai-2-27B-PQ2_0.gguf`
		}
		configured := testConfiguredServer(t)
		go configured.agent.RunMessageQueue(ctx)
		server := httptest.NewServer(configured.Handler())
		defer server.Close()
		baseURL = server.URL
	} else {
		modelURL = "http://127.0.0.1:8088/v1"
		modelName = `D:\llm-server\Bonsai-demo\models\bonsai2-gguf\27B\Ternary-Bonsai-2-27B-PQ2_0.gguf`
	}
	var provider, project, session struct {
		ID string `json:"id"`
	}
	profileName := fmt.Sprintf("Controls %s %d", map[bool]string{true: "Workspace", false: "Chat"}[workspace], time.Now().UnixNano())
	json.Unmarshal(requestJSON(t, baseURL+"/api/providers", http.MethodPost, map[string]any{"name": profileName, "adapter_kind": "openai-compatible", "base_url": modelURL, "model": modelName, "context_window": 65536, "context_evidence": "declared", "max_output_tokens": 4096}, http.StatusCreated), &provider)
	projectRoot := t.TempDir()
	if root := os.Getenv("HERMETRIX_CONTROLS_FIXTURE_ROOT"); liveURL != "" && root != "" {
		projectRoot = filepath.Join(root, provider.ID)
		if err := os.MkdirAll(projectRoot, 0700); err != nil {
			t.Fatal(err)
		}
	}
	json.Unmarshal(requestJSON(t, baseURL+"/api/projects", http.MethodPost, map[string]any{"name": "Synthetic " + profileName, "root_path": projectRoot}, http.StatusCreated), &project)
	sessionInput := map[string]any{"title": "Stop Queue Steer acceptance", "provider_id": provider.ID, "context_profile": "compact-32k"}
	if workspace {
		sessionInput["project_id"] = project.ID
		sessionInput["context_profile"] = "certified-64k"
		sessionInput["qualification_override"] = map[string]any{"actor": "isolated-test-operator", "reason": "Reviewed isolated controls probe; tests one small context and does not certify full model context capacity"}
	}
	json.Unmarshal(requestJSON(t, baseURL+"/api/sessions", http.MethodPost, sessionInput, http.StatusCreated), &session)
	debugURL, stop, err := startE2EChrome(ctx, chrome, filepath.Join(t.TempDir(), "chrome"))
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	target, err := createE2ETarget(ctx, debugURL, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := &e2eCDPClient{connection: conn}
	call := func(method string, args map[string]any) {
		t.Helper()
		if err := client.call(ctx, method, args, nil); err != nil {
			t.Fatal(err)
		}
	}
	eval := func(js string) string {
		t.Helper()
		s, err := client.evaluate(ctx, js)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	call("Runtime.enable", map[string]any{})
	waitBrowserValue(t, ctx, client, `typeof state!=='undefined'&&state.providers.length?'loaded':''`, "loaded")
	inputSelector, formSelector, view := "#chatInput", "#chatForm", "chat"
	if workspace {
		inputSelector = ".ide-assistant textarea"
		formSelector = ".pane-chat-form"
		view = "code"
	}
	eval(fmt.Sprintf(`window.__controlsInput=%q;window.__controlsForm=%q;selectSession(%q).then(()=>switchView(%q));'select'`, inputSelector, formSelector, session.ID, view))
	waitBrowserValue(t, ctx, client, `document.querySelector(window.__controlsInput)?'composer':''`, "composer")
	eval(`window.__controlsSawDelta=false;const previousControlsStream=window.HermetrixAssistant.onStream;window.HermetrixAssistant.onStream=item=>{if(item.type==='delta')window.__controlsSawDelta=true;previousControlsStream(item)};document.querySelector(window.__controlsInput).value='Write a long numbered list of 500 distinct debugging tips. Continue until all 500 are written. Do not use tools.';document.querySelector(window.__controlsInput).dispatchEvent(new Event('input'));document.querySelector(window.__controlsForm).requestSubmit();'sent'`)
	waitControlValue(t, ctx, client, `state.sending && window.__controlsSawDelta && state.sessionDetail.session.active_turn_id && !document.querySelector('[data-chat-stop]')?.disabled?'running':''`, "running")
	for _, width := range []int{390, 768, 1440} {
		call("Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": 900, "deviceScaleFactor": 1, "mobile": false})
		fit := eval(`(()=>{const b=document.querySelector('[data-chat-stop]'),r=b.getBoundingClientRect();return !document.querySelector(window.__controlsInput).disabled&&r.height>=44&&r.width>=44&&r.bottom<=innerHeight&&document.documentElement.scrollWidth<=innerWidth?'fits':JSON.stringify({rect:r,w:document.documentElement.scrollWidth,viewport:innerWidth})})()`)
		if fit != "fits" {
			t.Fatalf("controls unusable at %d: %s", width, fit)
		}
		if dir := os.Getenv("HERMETRIX_CONTROLS_AUDIT_DIR"); dir != "" {
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
			if err = os.WriteFile(filepath.Join(dir, fmt.Sprintf("chat-controls-%d.png", width)), pixels, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	eval(`document.querySelector(window.__controlsInput).value='obsolete queued direction';document.querySelector(window.__controlsInput).dispatchEvent(new Event('input'));document.querySelector(window.__controlsForm).requestSubmit();'queued'`)
	waitBrowserValue(t, ctx, client, `state.sessionDetail.queued_messages?.length===1?'queued':''`, "queued")
	if liveURL == "" && !liveModel && calls.Load() != 1 {
		t.Fatal("queue started before active execution ended")
	}
	// Steer must retire the old queued direction and stop the exact current turn.
	eval(`document.querySelector(window.__controlsInput).value='Reply with exactly CONTROL_ACCEPTANCE_OK and nothing else. Do not use tools.';document.querySelector(window.__controlsInput).dispatchEvent(new Event('input'));document.querySelector('[data-chat-steer]').click();'steered'`)
	waitControlValue(t, ctx, client, `state.sessionDetail?.session.state==='active'&&!state.sending&&state.sessionDetail.events.some(e=>e.role==='assistant'&&e.content.includes('CONTROL_ACCEPTANCE_OK'))?'finished':''`, "finished")
	if liveURL == "" && !liveModel && calls.Load() != 2 {
		t.Fatalf("dispatches=%d want 2", calls.Load())
	}
	result := eval(`JSON.stringify({stop:state.sessionDetail.events.filter(e=>e.event_kind==='turn_stopped').length,retired:state.sessionDetail.queued_messages.some(q=>q.content==='obsolete queued direction'&&q.state==='needs_review'),draft:document.querySelector(window.__controlsInput).value})`)
	var receipt struct {
		Stop    int    `json:"stop"`
		Retired bool   `json:"retired"`
		Draft   string `json:"draft"`
	}
	if err = json.Unmarshal([]byte(result), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Stop != 1 || !receipt.Retired || receipt.Draft != "" {
		t.Fatalf("receipt=%s", result)
	}
	t.Logf("Stop→Queue→Steer acceptance session=%s mode=%s", session.ID, map[bool]string{true: "Bonsai local", false: "fixture"}[liveURL != "" || liveModel])
}

func waitControlValue(t *testing.T, ctx context.Context, client *e2eCDPClient, expression, want string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		value, err := client.evaluate(ctx, expression)
		if err == nil && value == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	waitBrowserValue(t, ctx, client, expression, want)
}

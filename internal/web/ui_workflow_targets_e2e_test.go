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
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hermetrix-harness/internal/providers"
	toolruntime "hermetrix-harness/internal/tools"
)

func TestProjectWorkflowBrowserInputsInRealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome required")
		}
		t.Skip("Chrome unavailable")
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<!doctype html><title>Owned movement fixture</title><canvas tabindex="0" role="application" aria-label="Movement canvas" width="300" height="150"></canvas><p id="position">Moved 0</p><script>let x=0,timer;const c=document.querySelector('canvas'),g=c.getContext('2d');g.fillRect(20,20,20,20);c.addEventListener('keydown',e=>{if(e.key==='w'&&!timer)timer=setInterval(()=>{x++;g.clearRect(0,0,300,150);g.fillRect(20+x,20,20,20)},16)});c.addEventListener('keyup',()=>{clearInterval(timer);timer=null;document.querySelector('#position').textContent='Moved '+x})</script>`)
	}))
	defer fixture.Close()
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Messages []providers.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		number := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(args map[string]any) {
			encoded, _ := json.Marshal(args)
			body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("input-%d", number), "type": "function", "function": map[string]string{"name": "browser", "arguments": string(encoded)}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", body)
		}
		if number == 1 {
			emit(map[string]any{"action": "open", "url": fixture.URL + "/"})
			return
		}
		var receipt toolruntime.Receipt
		for _, message := range request.Messages {
			if message.Role == "tool" {
				var current toolruntime.Receipt
				if json.Unmarshal([]byte(message.Content), &current) == nil && current.Name == "browser" {
					receipt = current
				}
			}
		}
		if receipt.Status != "succeeded" {
			t.Errorf("browser did not complete: %+v", receipt)
		}
		if number == 2 {
			hash := regexp.MustCompile(`observation_hash ([0-9a-f]{64})`).FindStringSubmatch(receipt.Output)
			ref := regexp.MustCompile(`(?m)^\s+(\d+)\. canvas`).FindStringSubmatch(receipt.Output)
			if len(hash) != 2 || len(ref) != 2 {
				t.Errorf("fresh canvas receipt unavailable: %s", receipt.Output)
				return
			}
			reference, _ := strconv.Atoi(ref[1])
			emit(map[string]any{"action": "key", "tab_id": receipt.Metadata["tab_id"], "ref": reference, "text": "w", "duration_ms": 500, "observation_hash": hash[1]})
			return
		}
		if !regexp.MustCompile(`Moved [1-9][0-9]*`).MatchString(receipt.Output) {
			t.Errorf("real held-key movement missing: %s", receipt.Output)
		}
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"WORKFLOW_BROWSER_OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer model.Close()
	configured := testConfiguredServer(t)
	configured.agent.WithRuntime(configured.product, configured.product).WithDesktop(configured.product)
	server := httptest.NewServer(configured.Handler())
	defer server.Close()
	var provider, project, session struct {
		ID string `json:"id"`
	}
	json.Unmarshal(requestJSON(t, server.URL+"/api/providers", http.MethodPost, map[string]any{"name": "Workflow fixture", "adapter_kind": "openai-compatible", "base_url": model.URL + "/v1", "model": "workflow-fixture", "context_window": 65536, "context_evidence": "declared", "max_output_tokens": 4096}, http.StatusCreated), &provider)
	json.Unmarshal(requestJSON(t, server.URL+"/api/projects", http.MethodPost, map[string]any{"name": "Owned workflow target", "root_path": t.TempDir()}, http.StatusCreated), &project)
	json.Unmarshal(requestJSON(t, server.URL+"/api/sessions", http.MethodPost, map[string]any{"title": "Manual baseline", "provider_id": provider.ID, "project_id": project.ID, "context_profile": "compact-32k"}, http.StatusCreated), &session)
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
	eval(fmt.Sprintf(`document.querySelector('[data-assistant-auto-write]').click();document.querySelector('[data-assistant-targets]').open=true;const target=document.querySelector('[data-assistant-origin]');target.value=%q;target.dispatchEvent(new Event('input'));document.querySelector('.ide-assistant textarea').value='Open the selected site and hold W for 500ms';document.querySelector('.ide-assistant textarea').dispatchEvent(new Event('input'));document.querySelector('.pane-chat-form').requestSubmit();'sent'`, fixture.URL))
	waitControlValue(t, ctx, client, `state.sessionDetail?.session.state==='active'&&!state.sending&&state.sessionDetail.events.some(e=>e.content==='WORKFLOW_BROWSER_OK')?'done':''`, "done")
	if calls.Load() != 3 {
		t.Fatalf("model dispatches=%d want 3", calls.Load())
	}
	result := eval(`JSON.stringify({revision:state.sessionDetail.session.contract.workspace_write_approval.revision,origins:state.sessionDetail.session.contract.workspace_write_approval.browser_origins,states:state.sessionDetail.approvals.map(a=>a.state)})`)
	if !strings.Contains(result, `"project-workflow-v1"`) || !strings.Contains(result, fixture.URL) || strings.Contains(result, `pending`) {
		t.Fatalf("incorrect scope/outcome: %s", result)
	}
	for _, width := range []int{390, 1440} {
		if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
			t.Fatal(err)
		}
		if err := client.waitForPaint(ctx); err != nil {
			t.Fatal(err)
		}
		fit := eval(`(()=>{const b=document.querySelector('[data-assistant-send]'),r=b.getBoundingClientRect();return r.width>0&&r.right<=innerWidth&&r.bottom<=innerHeight&&document.documentElement.scrollWidth<=innerWidth?'fits':JSON.stringify(r)})()`)
		if fit != "fits" {
			t.Fatalf("workflow composer at %d: %s", width, fit)
		}
		if dir := os.Getenv("HERMETRIX_WORKFLOW_AUDIT_DIR"); dir != "" {
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
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("workflow-%d.png", width)), pixels, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Log("Actual UI-selected origin -> owned browser -> actual held key -> visible movement -> terminal exact receipt, without per-action pause")
}

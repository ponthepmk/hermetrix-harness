package web

import (
	"context"
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

// Use actual pointer down/up and touch events: element.click() misses focus
// changes that can move a submit button before its click is dispatched.
func TestWorkspaceSendByPointerAndTouch(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome required")
		}
		t.Skip("Chrome unavailable")
	}
	var calls atomic.Int32
	var unavailable atomic.Bool
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		if unavailable.Load() {
			http.Error(w, "test model unavailable", http.StatusServiceUnavailable)
			return
		}
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"POINTER-SEND-OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":8,\"total_tokens\":28}}\n\ndata: [DONE]\n\n")
	}))
	defer model.Close()
	server := testHTTPServer(t)
	var provider, project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/providers", http.MethodPost, map[string]any{
		"name": "Pointer local fixture", "adapter_kind": "openai-compatible", "base_url": model.URL + "/v1",
		"model": "pointer-fixture", "context_window": 32768, "context_evidence": "declared", "max_output_tokens": 4096}, http.StatusCreated), &provider); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/projects", http.MethodPost, map[string]any{
		"name": "Pointer project", "root_path": t.TempDir()}, http.StatusCreated), &project); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	call := func(method string, args map[string]any) {
		t.Helper()
		if err := client.call(ctx, method, args, nil); err != nil {
			t.Fatal(err)
		}
	}
	eval := func(js string) string {
		t.Helper()
		value, err := client.evaluate(ctx, js)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	call("Runtime.enable", map[string]any{})
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]')?'ready':''`, "ready")
	eval(fmt.Sprintf(`openProject(%q).then(()=>switchView('code'));'open'`, project.ID))
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-assistant-send]')?'assistant':''`, "assistant")
	eval(fmt.Sprintf(`document.querySelector('[data-assistant-provider]').value=%q;document.querySelector('[data-assistant-provider]').dispatchEvent(new Event('change',{bubbles:true}));'model'`, provider.ID))
	for index, size := range []struct {
		width, height int
		touch         bool
	}{{390, 1200, false}, {390, 550, true}, {414, 844, true}, {768, 900, false}, {1440, 900, false}} {
		call("Emulation.setDeviceMetricsOverride", map[string]any{"width": size.width, "height": size.height, "deviceScaleFactor": 1, "mobile": false})
		eval(`window.__resizeSettled=false;requestAnimationFrame(()=>requestAnimationFrame(()=>window.__resizeSettled=true));'resize'`)
		waitBrowserValue(t, ctx, client, `window.__resizeSettled?'settled':''`, "settled")
		if size.width <= 700 {
			fit := eval(`(()=>{const pane=document.querySelector('.pane:not(.pane-hidden)'),body=pane.querySelector('.pane-body');return Math.abs(body.getBoundingClientRect().bottom-pane.getBoundingClientRect().bottom)<2?'fills':getComputedStyle(pane).gridTemplateRows})()`)
			if fit != "fills" {
				t.Errorf("chat leaves a blank row at %dx%d: %s", size.width, size.height, fit)
			}
		}
		prompt := fmt.Sprintf("pointer message %d", index)
		eval(`document.querySelector('.ide-assistant textarea').focus();'focus'`)
		call("Input.insertText", map[string]any{"text": prompt})
		var point struct{ X, Y float64 }
		if err := json.Unmarshal([]byte(eval(`(()=>{const r=document.querySelector('[data-assistant-send]').getBoundingClientRect();return JSON.stringify({X:r.x+r.width/2,Y:r.y+r.height/2})})()`)), &point); err != nil {
			t.Fatal(err)
		}
		if point.Y > float64(size.height) {
			t.Fatalf("Send outside viewport: %+v", point)
		}
		before := calls.Load()
		if target := eval(fmt.Sprintf(`document.elementFromPoint(%f,%f)?.closest('[data-assistant-send]')?'send':'covered'`, point.X, point.Y)); target != "send" {
			t.Fatalf("Send covered at %dx%d", size.width, size.height)
		}
		if size.touch {
			call("Input.dispatchTouchEvent", map[string]any{"type": "touchStart", "touchPoints": []map[string]any{{"x": point.X, "y": point.Y}}})
			call("Input.dispatchTouchEvent", map[string]any{"type": "touchEnd", "touchPoints": []map[string]any{}})
		} else {
			call("Input.dispatchMouseEvent", map[string]any{"type": "mousePressed", "x": point.X, "y": point.Y, "button": "left", "clickCount": 1})
			position := eval(fmt.Sprintf(`(()=>{const r=document.querySelector('[data-assistant-send]').getBoundingClientRect();return Math.abs(r.x+r.width/2-%f)<2&&Math.abs(r.y+r.height/2-%f)<2?'stable':JSON.stringify(r)})()`, point.X, point.Y))
			call("Input.dispatchMouseEvent", map[string]any{"type": "mouseReleased", "x": point.X, "y": point.Y, "button": "left", "clickCount": 1})
			if position != "stable" {
				t.Fatalf("Send moved during pointer down at %dx%d: %s", size.width, size.height, position)
			}
		}
		waitBrowserValue(t, ctx, client, fmt.Sprintf(`!state.sending && !document.querySelector('[data-assistant-send]').disabled && [...document.querySelectorAll('.ide-assistant-log .chat-message.user')].some(el=>el.textContent.includes(%q)) && document.querySelector('.ide-assistant-log').textContent.includes('POINTER-SEND-OK')?'replied':''`, prompt), "replied")
		if calls.Load() != before+1 {
			t.Fatalf("send issued %d model requests, want 1", calls.Load()-before)
		}
		if eval(`document.querySelector('.ide-assistant textarea').value`) != "" {
			t.Fatal("successful send did not clear draft")
		}
	}
	// Failure remains visible, keeps the text, and never silently replays it.
	unavailable.Store(true)
	call("Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": false})
	eval(`window.__resizeSettled=false;requestAnimationFrame(()=>requestAnimationFrame(()=>window.__resizeSettled=true));'resize-error'`)
	waitBrowserValue(t, ctx, client, `window.__resizeSettled?'settled':''`, "settled")
	eval(`document.querySelector('.ide-assistant textarea').focus();'focus-error'`)
	call("Input.insertText", map[string]any{"text": "keep failed draft"})
	var point struct{ X, Y float64 }
	if err := json.Unmarshal([]byte(eval(`(()=>{const r=document.querySelector('[data-assistant-send]').getBoundingClientRect();return JSON.stringify({X:r.x+r.width/2,Y:r.y+r.height/2})})()`)), &point); err != nil {
		t.Fatal(err)
	}
	call("Input.dispatchTouchEvent", map[string]any{"type": "touchStart", "touchPoints": []map[string]any{{"x": point.X, "y": point.Y}}})
	call("Input.dispatchTouchEvent", map[string]any{"type": "touchEnd", "touchPoints": []map[string]any{}})
	waitBrowserValue(t, ctx, client, `!state.sending && document.querySelector('.ide-assistant textarea').value==='keep failed draft' && document.querySelector('.ide-assistant-status.error')?.getBoundingClientRect().height>0?'error-visible':''`, "error-visible")
	if failure := eval(`state.chatTurnError`); failure == "" {
		t.Fatal("visible failure has no persisted turn error")
	}
	// The sibling Chat composer must also receive a real pointer click while
	// the request-status notice is present, not only requestSubmit().
	unavailable.Store(false)
	eval(`document.querySelector('#viewSwitch [data-view="chat"]').click();'chat-view'`)
	waitBrowserValue(t, ctx, client, `document.querySelector('#chatInput')?'composer':''`, "composer")
	for index, size := range []struct {
		width, height int
		touch         bool
	}{{390, 550, true}, {390, 844, true}, {390, 1200, false}, {768, 900, false}, {1440, 900, false}} {
		call("Emulation.setDeviceMetricsOverride", map[string]any{"width": size.width, "height": size.height, "deviceScaleFactor": 1, "mobile": false})
		eval(`window.__resizeSettled=false;requestAnimationFrame(()=>requestAnimationFrame(()=>window.__resizeSettled=true));'resize-main'`)
		waitBrowserValue(t, ctx, client, `window.__resizeSettled?'settled':''`, "settled")
		geometry := eval(`(()=>{const f=document.querySelector('#chatForm'),m=f.querySelector('.session-usage'),i=f.querySelector('textarea'),tools=f.querySelector('.composer-tools'),buttons=[...tools.querySelectorAll('button')],r=f.getBoundingClientRect(),mr=m.getBoundingClientRect(),ir=i.getBoundingClientRect(),tr=tools.getBoundingClientRect(),log=document.querySelector('#messageList').getBoundingClientRect();return r.bottom<=innerHeight && r.left>=0 && r.right<=innerWidth && log.bottom<=r.top+2 && mr.top>=ir.bottom && ir.height>=48 && ir.height<100 && tr.height>=44 && r.height<innerHeight*.35 ? 'fits' : JSON.stringify({r,mr,ir,tr,log,scroll:tools.scrollWidth,client:tools.clientWidth})})()`)
		if geometry != "fits" {
			t.Fatalf("composer layout at %dx%d: %s", size.width, size.height, geometry)
		}
		if size.width <= 700 {
			eval(`document.querySelector('.composer-tool-menu summary').click();document.querySelector('#chatForm .voice-entry').focus();'last-tool'`)
			waitBrowserValue(t, ctx, client, `(()=>{const row=document.querySelector('#chatForm .composer-tools'),b=row.querySelector('.voice-entry').getBoundingClientRect(),r=row.getBoundingClientRect();return document.querySelector('.composer-tool-menu').open && b.width>=44 && b.height>=44 && b.left>=0 && b.right<=innerWidth?'last-tool-visible':''})()`, "last-tool-visible")
			eval(`document.querySelector('#composerCapabilityButton').focus();document.querySelector('.composer-tool-menu').open=false;'first-tool'`)
		}
		// A long draft grows only to the viewport-relative cap; Send stays visible.
		eval(`document.querySelector('#chatInput').value='long draft\n'.repeat(80);document.querySelector('#chatInput').dispatchEvent(new Event('input',{bubbles:true}));'long-draft'`)
		if eval(`(()=>{const i=document.querySelector('#chatInput'),b=document.querySelector('#chatForm > button.primary').getBoundingClientRect();return i.scrollHeight>i.clientHeight && i.getBoundingClientRect().height<=Math.max(64,Math.min(180,innerHeight*.22))+1 && b.bottom<=innerHeight?'bounded':''})()`) != "bounded" {
			t.Fatal("long draft hides Send or grows without a cap")
		}
		eval(`document.querySelector('#chatInput').value='';document.querySelector('#chatInput').dispatchEvent(new Event('input',{bubbles:true}));document.querySelector('#chatInput').focus();'focus-main'`)
		prompt := fmt.Sprintf("main chat pointer message %d", index)
		call("Input.insertText", map[string]any{"text": prompt})
		if err := json.Unmarshal([]byte(eval(`(()=>{const r=document.querySelector('#chatForm > button.primary').getBoundingClientRect();return JSON.stringify({X:r.x+r.width/2,Y:r.y+r.height/2})})()`)), &point); err != nil {
			t.Fatal(err)
		}
		if eval(fmt.Sprintf(`document.elementFromPoint(%f,%f)?.closest('#chatForm > button.primary')?'send':'covered'`, point.X, point.Y)) != "send" {
			t.Fatal("main Chat Send is covered")
		}
		before := calls.Load()
		if size.touch {
			call("Input.dispatchTouchEvent", map[string]any{"type": "touchStart", "touchPoints": []map[string]any{{"x": point.X, "y": point.Y}}})
			call("Input.dispatchTouchEvent", map[string]any{"type": "touchEnd", "touchPoints": []map[string]any{}})
		} else {
			for _, kind := range []string{"mousePressed", "mouseReleased"} {
				call("Input.dispatchMouseEvent", map[string]any{"type": kind, "x": point.X, "y": point.Y, "button": "left", "clickCount": 1})
			}
		}
		waitBrowserValue(t, ctx, client, fmt.Sprintf(`!state.sending && [...document.querySelectorAll('#messageList .chat-message.user')].some(el=>el.textContent.includes(%q)) && document.querySelector('#chatInput')?.value===''?'main-replied':''`, prompt), "main-replied")
		if calls.Load() != before+1 {
			t.Fatal("main Chat did not send exactly once")
		}
	}
}

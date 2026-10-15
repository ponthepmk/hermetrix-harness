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
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestCompactChatReadingAndDisclosuresInRealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Skip("Chrome unavailable")
	}
	pageURL := os.Getenv("HERMETRIX_COMPACT_CHAT_LIVE_URL")
	sessionID := os.Getenv("HERMETRIX_COMPACT_CHAT_SESSION")
	live := pageURL != ""
	if !live {
		model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			answer := "## แผนสร้างโปรเจกต์\n\n" + strings.Repeat("อ่านผลและตรวจคำสั่งก่อนเริ่มทำงาน ข้อความนี้ควรอยู่ในพื้นที่บทสนทนาที่เลื่อนได้\n\n", 35)
			answer += "## ความสัมพันธ์ของระบบ\n\n1. ผู้ใช้ส่งเป้าหมาย\n   - ตรวจบริบทของโปรเจกต์\n   - ตรวจสิทธิ์ก่อนทำงาน\n\n| ระบบ | หน้าที่ |\n| --- | --- |\n| Hermetrix | จัดการงาน |\n| Project Brain | ให้ความรู้ผ่าน MCP |\n\n```mermaid\nflowchart TD\nA[ผู้ใช้] --> B[Hermetrix]\nB --> C[MCP]\nC --> D[Project Brain]\n```\n\n```mermaid\n%%{init: {securityLevel: 'loose'}}%%\nflowchart TD\nA --> B\n```"
			payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": answer}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 200, "completion_tokens": 100, "total_tokens": 300}})
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
		}))
		defer model.Close()
		configured := testConfiguredServer(t)
		server := httptest.NewServer(configured.Handler())
		defer server.Close()
		pageURL = server.URL
		var provider, project, session struct {
			ID string `json:"id"`
		}
		json.Unmarshal(requestJSON(t, pageURL+"/api/providers", "POST", map[string]any{"name": "Compact local fixture", "adapter_kind": "openai-compatible", "base_url": model.URL + "/v1", "model": "compact-fixture", "context_window": 32768, "context_evidence": "declared", "max_output_tokens": 4096}, 201), &provider)
		json.Unmarshal(requestJSON(t, pageURL+"/api/projects", "POST", map[string]any{"name": "โปรเจกต์ที่มีชื่อยาวมากสำหรับตรวจหน้าจอมือถือและแท็บเล็ต 1791629753166524400", "root_path": t.TempDir()}, 201), &project)
		json.Unmarshal(requestJSON(t, pageURL+"/api/sessions", "POST", map[string]any{"provider_id": provider.ID, "project_id": project.ID, "context_profile": "compact-32k", "title": "แผนสร้างโปรเจกต์และผลตรวจที่ต้องอ่านให้ครบ"}, 201), &session)
		sessionID = session.ID
		requestJSON(t, pageURL+"/api/sessions/"+sessionID+"/turns", "POST", map[string]any{"content": "อธิบายแผนสร้างโปรเจกต์"}, 200)
		requestJSON(t, pageURL+"/api/sessions/"+sessionID+"/queue", "POST", map[string]any{"content": "คำสั่งเดิมที่พักไว้หลังเปลี่ยนทิศทาง ให้ตรวจหรือยกเลิกก่อนทำต่อ", "mode": "queue", "idempotency_key": "compact-queue-fixture"}, 202)
		if _, err := configured.store.DB.Exec(`UPDATE agent_message_queue SET state='needs_review',error='Direction changed: review earlier queued messages' WHERE session_id=?`, sessionID); err != nil {
			t.Fatal(err)
		}
	}
	if sessionID == "" {
		t.Fatal("live acceptance requires an exact session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	debugURL, stop, err := startE2EChrome(ctx, chrome, filepath.Join(t.TempDir(), "chrome"))
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	target, err := createE2ETarget(ctx, debugURL, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := &e2eCDPClient{connection: conn}
	defer func() {
		if t.Failed() {
			for _, browserError := range client.browserErrors {
				t.Log("browser diagnostic:", string(browserError))
			}
			value, _ := client.evaluate(ctx, `JSON.stringify([...document.querySelectorAll('.chat-diagram')].map(f=>({mounted:f.dataset.mounted,rendered:f.dataset.rendered,html:f.querySelector('.diagram-frame').innerHTML.slice(0,200)})))`)
			t.Log("diagram state:", value)
			messages, _ := client.evaluate(ctx, `JSON.stringify(window.diagramMessages)`)
			t.Log("diagram messages:", messages)
		}
	}()
	for _, method := range []string{"Runtime.enable", "Page.enable", "Log.enable"} {
		if err := client.call(ctx, method, map[string]any{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": `window.diagramMessages=[];window.addEventListener('message',e=>{if(String(e.data?.kind||'').startsWith('hermetrix-diagram'))window.diagramMessages.push({kind:e.data.kind,stage:e.data.stage,origin:e.origin,type:typeof e.data.svg,length:e.data.svg?.length,prefix:e.data.svg?.slice(0,100)});});`}, nil); err != nil {
		t.Fatal(err)
	}
	if tokenFile := os.Getenv("HERMETRIX_COMPACT_CHAT_TOKEN_FILE"); tokenFile != "" {
		token, err := os.ReadFile(tokenFile)
		if err != nil {
			t.Fatal("cannot read login token")
		}
		encoded, _ := json.Marshal(strings.TrimSpace(string(token)))
		if err := client.call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": "window.prompt=()=>" + string(encoded) + ";"}, nil); err != nil {
			t.Fatal("cannot prepare login")
		}
	}
	if err := client.call(ctx, "Page.navigate", map[string]any{"url": pageURL}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.sessionReady ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, fmt.Sprintf(`void selectSession(%q); 'selecting'`, sessionID)); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`state.sessionDetail?.session?.id===%q && document.querySelector('#chatForm') ? 'selected' : ''`, sessionID), "selected")
	if !live {
		if _, err := client.evaluate(ctx, `document.querySelector('.chat-diagram').scrollIntoView({block:'center'}); 'diagram-visible'`); err != nil {
			t.Fatal(err)
		}
		waitBrowserValue(t, ctx, client, `document.querySelector('.chat-diagram')?.dataset.rendered==='true' ? 'diagram-rendered' : ''`, "diagram-rendered")
		waitBrowserValue(t, ctx, client, `document.querySelectorAll('.chat-diagram')[1]?.dataset.rendered==='error' ? 'diagram-config-rejected' : ''`, "diagram-config-rejected")
		waitBrowserValue(t, ctx, client, `document.querySelector('#messageList h2') && document.querySelector('#messageList ol ul') && document.querySelector('#messageList table tbody td') ? 'structured-answer' : ''`, "structured-answer")
	}
	if live && os.Getenv("HERMETRIX_COMPACT_CHAT_REQUIRE_DIAGRAM") == "1" {
		client.evaluate(ctx, `document.querySelector('.chat-diagram')?.scrollIntoView({block:'center'}); 'show-diagram'`)
		waitBrowserValue(t, ctx, client, `document.querySelector('.chat-diagram')?.dataset.rendered==='true' && document.querySelector('#messageList table tbody td') && document.querySelector('#messageList h2') ? 'live-rich-answer' : ''`, "live-rich-answer")
	}
	queuePresent, _ := client.evaluate(ctx, `document.querySelector('.chat-queue-menu') ? 'yes' : ''`)
	if !live || os.Getenv("HERMETRIX_COMPACT_CHAT_REQUIRE_DIAGRAM") == "1" {
		client.evaluate(ctx, `document.querySelector('[data-diagram-expand]').click(); 'expand-diagram'`)
		waitBrowserValue(t, ctx, client, `document.querySelector('.diagram-viewer')?.open && document.querySelector('.diagram-viewer img')?.naturalWidth>0 ? 'expanded-diagram-visible' : ''`, "expanded-diagram-visible")
		client.evaluate(ctx, `document.querySelector('.diagram-viewer button').click(); 'close-diagram'`)
		waitBrowserValue(t, ctx, client, `!document.querySelector('.diagram-viewer').open && document.activeElement.matches('[data-diagram-expand]') ? 'diagram-focus-restored' : ''`, "diagram-focus-restored")
	}
	for _, viewport := range []struct{ width, height int }{{390, 640}, {768, 700}, {1024, 700}, {1440, 900}, {390, 420}} {
		if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": viewport.width, "height": viewport.height, "deviceScaleFactor": 1, "mobile": viewport.width < 701}, nil); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
		if live && os.Getenv("HERMETRIX_COMPACT_CHAT_REQUIRE_DIAGRAM") == "1" {
			client.evaluate(ctx, `document.querySelector('.chat-diagram').scrollIntoView({block:'center'}); 'diagram-in-viewport'`)
		}
		metrics, err := client.evaluate(ctx, `JSON.stringify((()=>{const form=document.querySelector('#chatForm'), log=document.querySelector('#messageList'), send=form.querySelector('button.primary'), header=document.querySelector('#appHeader'); const f=form.getBoundingClientRect(),l=log.getBoundingClientRect(),s=send.getBoundingClientRect(),h=header.getBoundingClientRect(); return {form:f.height,log:l.height,formTop:f.top,logBottom:l.bottom,sendBottom:s.bottom,header:h.height,width:innerWidth,height:innerHeight,overflow:document.documentElement.scrollWidth-innerWidth,grid:getComputedStyle(form).gridTemplateRows,railVisible:document.querySelector('#zoneRail').getBoundingClientRect().width>0&&getComputedStyle(document.querySelector('#zoneRail')).display!=='none'};})())`)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%dx%d %s", viewport.width, viewport.height, metrics)
		if output := os.Getenv("HERMETRIX_E2E_SCREENSHOT_DIR"); output != "" {
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
			if err := os.MkdirAll(output, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(output, fmt.Sprintf("compact-chat-%d-%d.png", viewport.width, viewport.height)), pixels, 0644); err != nil {
				t.Fatal(err)
			}
			if live && os.Getenv("HERMETRIX_COMPACT_CHAT_REQUIRE_DIAGRAM") == "1" {
				client.evaluate(ctx, `document.querySelector('#messageList .chat-message.assistant:not(.streaming)').scrollIntoView({block:'start'}); 'answer-top'`)
				if err := client.call(ctx, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": false}, &shot); err != nil {
					t.Fatal(err)
				}
				pixels, err = base64.StdEncoding.DecodeString(shot.Data)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(output, fmt.Sprintf("rich-answer-%d-%d.png", viewport.width, viewport.height)), pixels, 0644); err != nil {
					t.Fatal(err)
				}
			}
		}
		var bounds struct {
			Form, Log, FormTop, LogBottom, SendBottom, Header, Width, Height, Overflow float64
			RailVisible                                                                bool
		}
		if err := json.Unmarshal([]byte(metrics), &bounds); err != nil {
			t.Fatal(err)
		}
		if bounds.Form > bounds.Height*.34 || bounds.Log < bounds.Height*.4 || bounds.SendBottom > bounds.Height || bounds.LogBottom > bounds.FormTop+1 || bounds.Overflow > 1 {
			t.Fatalf("reading or send obstructed at %dx%d: %s", viewport.width, viewport.height, metrics)
		}
		if viewport.width <= 1080 && (bounds.Header > 56 || bounds.RailVisible) {
			t.Fatalf("compact navigation crowded: %s", metrics)
		}
		waitBrowserValue(t, ctx, client, `document.querySelector('#projectName').textContent===state.sessionDetail.session.title && !document.querySelector('.chat-head')?.getClientRects().length ? 'single-conversation-title' : ''`, "single-conversation-title")
		if viewport.width <= 1080 {
			client.evaluate(ctx, `document.querySelector('#compactNavigation summary').click(); 'navigation-open'`)
			waitBrowserValue(t, ctx, client, `document.querySelector('#compactNavigation').open && [...document.querySelectorAll('[data-compact-view]')].every(b=>{ const r=b.getBoundingClientRect(),hit=document.elementFromPoint(r.x+r.width/2,r.y+r.height/2); return r.height>=44 && (hit===b||b.contains(hit)); }) ? 'navigation-reachable' : ''`, "navigation-reachable")
			client.evaluate(ctx, `document.querySelector('#compactNavigation').open=false; document.querySelector('#toggleRail').click(); 'drawer-open'`)
			waitBrowserValue(t, ctx, client, `document.querySelector('#zones').classList.contains('mobile-rail-open') && getComputedStyle(document.querySelector('#zoneRail')).display!=='none' ? 'drawer-visible' : ''`, "drawer-visible")
			client.evaluate(ctx, `document.querySelector('#toggleRail').click(); 'drawer-closed'`)
		}
		if _, err := client.evaluate(ctx, `document.querySelector('.composer-tool-menu summary').click(); 'tools-open'`); err != nil {
			t.Fatal(err)
		}
		waitBrowserValue(t, ctx, client, `document.querySelector('.composer-tool-menu').open && document.querySelector('#composerFilesButton').getBoundingClientRect().height>=44 ? 'tools-visible' : ''`, "tools-visible")
		client.evaluate(ctx, `document.querySelector('.composer-tool-menu').open=false; 'tools-closed'`)
		if queuePresent == "yes" {
			client.evaluate(ctx, `document.querySelector('.chat-queue-menu summary').click(); 'queue-open'`)
			waitBrowserValue(t, ctx, client, `document.querySelector('.chat-queue-menu')?.open && document.querySelector('[data-chat-cancel]')?.getBoundingClientRect().height>=44 ? 'queue-visible' : ''`, "queue-visible")
			client.evaluate(ctx, `document.querySelector('.chat-queue-menu').open=false; 'queue-closed'`)
		}
		client.evaluate(ctx, `document.querySelector('.composer-usage summary').click(); 'tokens-open'`)
		waitBrowserValue(t, ctx, client, `document.querySelector('.composer-usage .token-meter')?.open && document.querySelector('.composer-usage .token-meter-detail').getBoundingClientRect().height>0 ? 'tokens-visible' : ''`, "tokens-visible")
		client.evaluate(ctx, `document.querySelector('.composer-usage .token-meter').open=false; 'closed'`)
	}
	client.evaluate(ctx, `document.querySelector('#chatInput').focus(); document.querySelector('#chatInput').value='ข้อความร่างยาว\n'.repeat(80); document.querySelector('#chatInput').dispatchEvent(new Event('input')); 'long-draft'`)
	waitBrowserValue(t, ctx, client, `(()=>{const form=document.querySelector('#chatForm'),input=form.querySelector('textarea'),send=form.querySelector('button.primary');return input.scrollHeight>input.clientHeight && send.getBoundingClientRect().bottom<=innerHeight && document.querySelector('#messageList').getBoundingClientRect().height>100 ? 'long-draft-readable' : ''})()`, "long-draft-readable")
	client.evaluate(ctx, `document.querySelector('#chatInput').value=''; document.querySelector('#chatInput').dispatchEvent(new Event('input')); document.querySelector('#chatInput').blur(); 'draft-cleared'`)
	if !live {
		client.evaluate(ctx, `document.querySelector('.chat-queue-menu summary').click(); document.querySelector('[data-chat-cancel]').click(); 'cancelled'`)
		waitBrowserValue(t, ctx, client, `state.sessionDetail.queued_messages.length===0 ? 'queue-cleared' : ''`, "queue-cleared")
		response, err := http.Get(pageURL + "/api/sessions/" + sessionID)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		content, _ := io.ReadAll(response.Body)
		var detail struct {
			QueuedMessages []json.RawMessage `json:"queued_messages"`
		}
		if err := json.Unmarshal(content, &detail); err != nil {
			t.Fatal(err)
		}
		if len(detail.QueuedMessages) != 0 {
			t.Fatalf("queue cancellation not persisted: %s", content)
		}
	}
}

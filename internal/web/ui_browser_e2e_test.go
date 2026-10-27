package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"hermetrix-harness/internal/taskengine"

	"github.com/gorilla/websocket"
)

// TestCockpitHydratesInARealBrowser is deliberately not another source-text
// assertion. Chrome loads the actual index, enforces its CSP, executes both
// scripts, calls the real Projects API and serialises the resulting DOM. This
// catches script ordering, browser-only syntax, CSP and bootstrap failures that
// node --check and ui_contract_test cannot see.
func TestCockpitHydratesInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome is required for the UI E2E job")
		}
		t.Skip("Chrome is not installed")
	}
	server := testHTTPServer(t)
	requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "E2E <project>", "root_path": ""}, http.StatusCreated)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	deadline := time.Now().Add(10 * time.Second)
	hydrated := ""
	for time.Now().Before(deadline) {
		hydrated, err = client.evaluate(ctx, `document.querySelector("[data-open-project]")?.textContent || ""`)
		if err == nil && strings.Contains(hydrated, "E2E <project>") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.picker-brand')?.complete && document.querySelector('.picker-brand')?.naturalWidth > 0 ? 'picker-logo-loaded' : ''`, "picker-logo-loaded")
	if !strings.Contains(hydrated, "E2E <project>") {
		t.Fatalf("cockpit did not hydrate the project picker: value=%q err=%v", hydrated, err)
	}
	dom, err := client.evaluate(ctx, `document.documentElement.outerHTML`)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{`class="picker-card"`, `data-open-project=`, `E2E &lt;project&gt;`} {
		if !strings.Contains(dom, marker) {
			t.Fatalf("hydrated DOM is missing %q; scripts did not complete\n%s", marker, abbreviateDOM(dom))
		}
	}
	if strings.Contains(dom, `<strong>E2E <project>`) {
		t.Fatal("project name entered the hydrated DOM as markup")
	}
	if _, err := client.evaluate(ctx, `document.querySelector('[data-open-project]').click(); 'opening'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#projectName')?.textContent.includes('E2E <project>') ? 'opened' : ''`, "opened")
	waitBrowserValue(t, ctx, client, `Array.from(document.querySelectorAll('.project-mark,.runtime-mark,.task-start-heading img')).every(img => img.complete && img.naturalWidth > 0) ? 'shell-logos-loaded' : ''`, "shell-logos-loaded")
	if _, err := client.evaluate(ctx, `switchTab('projects'); 'projects'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#projectArchive') && document.querySelector('#projectRename') && document.querySelector('.project-add-details') && document.querySelector('.project-advanced:not([open])') ? 'manage-project' : ''`, "manage-project")
	if _, err := client.evaluate(ctx, `document.querySelector('#projectArchive').click(); document.querySelector('#actionConfirm').click(); 'archiving'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-restore-project]') && !document.querySelector('#projectArchive') ? 'archived-project' : ''`, "archived-project")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-restore-project]').click(); 'restoring'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#projectArchive') ? 'restored-project' : ''`, "restored-project")
	if _, err := client.evaluate(ctx, `switchTab('library'); 'skills'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#studioImportFile') && document.querySelector('#studioChatSkillButton') ? 'import-skill' : ''`, "import-skill")
}

func TestCapabilityCenterSearchDoesNotCloseOpenToolDetailsInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome is required for the UI E2E job")
		}
		t.Skip("Chrome is not installed")
	}
	server := testHTTPServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	waitBrowserValue(t, ctx, client, `typeof switchTab === 'function' && state.direct_tools?.length ? 'hydrated' : ''`, "hydrated")
	if _, err := client.evaluate(ctx, `switchTab('capabilities'); 'opened'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#capabilityCenterTools details') ? 'tools-ready' : ''`, "tools-ready")
	if _, err := client.evaluate(ctx, `(() => { const input = document.querySelector('#capabilityCenterQuery'); input.value = 'workspace.read_file'; input.dispatchEvent(new Event('input', {bubbles:true})); document.querySelector('#capabilityCenterTools details').open = true; return 'expanded'; })()`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#capabilityCenterMCP')?.textContent.includes('กำลังค้นหา') ? '' : 'search-complete'`, "search-complete")
	open, err := client.evaluate(ctx, `document.querySelector('#capabilityCenterTools details')?.open ? 'still-open' : 'closed'`)
	if err != nil {
		t.Fatal(err)
	}
	if open != "still-open" {
		t.Fatalf("MCP search response closed the tool detail: %s", open)
	}
	if _, err := client.evaluate(ctx, `switchTab('tools'); state.directToolQuery = 'workspace.read_file'; renderDirectTools(); document.querySelector('[data-tool-text^="workspace.read_file"]').open = true; 'tool-open'`); err != nil {
		t.Fatal(err)
	}
	blocked, err := client.evaluate(ctx, `document.querySelector('[data-use-direct-tool="workspace.read_file"]')?.disabled && document.querySelector('[data-tool-text^="workspace.read_file"]')?.textContent.includes('เปิดแชทที่ผูกโปรเจกต์') ? 'honest-unavailable' : 'misleading-action'`)
	if err != nil {
		t.Fatal(err)
	}
	if blocked != "honest-unavailable" {
		t.Fatalf("direct tool offered an unusable chat action: %s", blocked)
	}
}

func TestCandidateDialogCancelSkipsValidationInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	server := testHTTPServer(t)
	requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Dialog audit", "root_path": ""}, http.StatusCreated)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
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
	for _, method := range []string{"Runtime.enable", "Page.enable"} {
		if err := client.call(ctx, method, map[string]any{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]')?.textContent.includes('Dialog audit') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-open-project]').click(); switchTab('library'); 'opened'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#studioCreateButton') ? 'ready' : ''`, "ready")
	for _, selector := range []string{`#candidateDialog .dialog-actions [data-close-candidate]`, `#candidateDialog .dialog-head [data-close-candidate]`} {
		if _, err := client.evaluate(ctx, `document.querySelector('#studioCreateButton').focus(); document.querySelector('#studioCreateButton').click(); 'dialog-open'`); err != nil {
			t.Fatal(err)
		}
		waitBrowserValue(t, ctx, client, `document.querySelector('#candidateDialog').open && !document.querySelector('#candidateForm').checkValidity() ? 'invalid-form-open' : ''`, "invalid-form-open")
		if _, err := client.evaluate(ctx, fmt.Sprintf(`document.querySelector(%q).click(); 'clicked'`, selector)); err != nil {
			t.Fatal(err)
		}
		waitBrowserValue(t, ctx, client, `!document.querySelector('#candidateDialog').open && document.activeElement?.id === 'studioCreateButton' ? 'closed-without-validation' : ''`, "closed-without-validation")
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#studioCreateButton').click(); document.querySelector('#saveCandidate').click(); 'submitted-invalid'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#candidateDialog').open && !document.querySelector('#candidateForm').checkValidity() ? 'save-blocked' : ''`, "save-blocked")
	for _, eventType := range []string{"keyDown", "keyUp"} {
		if err := client.call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": eventType, "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27}, nil); err != nil {
			t.Fatal(err)
		}
	}
	waitBrowserValue(t, ctx, client, `!document.querySelector('#candidateDialog').open ? 'escape-closed' : ''`, "escape-closed")
	if _, err := client.evaluate(ctx, `(() => {document.querySelector('#studioCreateButton').click(); const form=document.querySelector('#candidateForm'); form.elements.name.value='browser-audit-skill'; form.elements.description.value='Use for browser audit work'; form.elements.reason.value='Verify the proposal flow'; form.elements.body.value='# Procedure\n\n1. Inspect the browser.'; form.requestSubmit(); return 'valid-proposal-submitted';})()`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!document.querySelector('#candidateDialog').open && state.activeTab === 'proposals' && state.candidates.some(item => item.canonical_name === 'browser-audit-skill') ? 'proposal-created' : ''`, "proposal-created")
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `switchTab('library'); document.querySelector('#studioCreateButton').click(); 'mobile-dialog'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const dialog=document.querySelector('#candidateDialog'), box=dialog.getBoundingClientRect(); return dialog.open && box.left >= -1 && box.right <= innerWidth + 1 && document.documentElement.scrollWidth <= innerWidth + 1 ? 'mobile-dialog-fits' : ''; })()`, "mobile-dialog-fits")
}

func TestLongChatReplyStaysReadableAboveComposerInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	server := testHTTPServer(t)
	requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Chat layout audit", "root_path": ""}, http.StatusCreated)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	for _, method := range []string{"Runtime.enable", "Page.enable"} {
		if err := client.call(ctx, method, map[string]any{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-open-project]').click(); 'opened'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.currentProject?.id ? 'ready' : ''`, "ready")
	seed := `state.sessionDetail={session:{id:'chat-layout-audit',title:'Long answer',project_id:state.currentProject.id,provider_name:'fixture',context_profile:'compact-32k',contract_revision:'rev',cache_epoch:0,contract:{},state:'active',model:'fixture'},events:[{event_kind:'message',role:'assistant',content:'## Summary\n\n**Two IDs** must stay distinct.\n- Board ID\n- Platform ID\n\n'+'A readable answer. '.repeat(900),metadata:{}}],approvals:[]}; state.selectedSession='chat-layout-audit'; renderChat(); 'seeded'`
	if _, err := client.evaluate(ctx, seed); err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{1440, 390, 1024} {
		height := 844
		if width == 1024 {
			height = 556
			if _, err := client.evaluate(ctx, `document.querySelector("#hardwareMonitor").open=true; "monitor-expanded"`); err != nil {
				t.Fatal(err)
			}
		}
		if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": height, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
			t.Fatal(err)
		}
		waitBrowserValue(t, ctx, client, `(() => { const list=document.querySelector('#messageList'), composer=document.querySelector('#chatForm'), last=[...list?.querySelectorAll('.chat-message')||[]].filter(node=>!node.classList.contains('hidden')).at(-1), input=document.querySelector('#chatInput'); if(!list||!composer||!last||!input)return ''; list.scrollTop=list.scrollHeight; const l=list.getBoundingClientRect(),c=composer.getBoundingClientRect(),m=last.getBoundingClientRect(),i=input.getBoundingClientRect(); const hit=document.elementFromPoint(i.left+i.width/2,i.top+i.height/2); return l.height>40 && l.bottom<=c.top+1 && m.bottom<=l.bottom+1 && i.top>=0 && i.bottom<=innerHeight && i.width>180 && (hit===input||input.contains(hit)) ? 'readable' : ''; })()`, "readable")
		if width == 390 {
			waitBrowserValue(t, ctx, client, `(() => { const form=document.querySelector('#chatForm'), menu=form?.querySelector('.composer-tool-menu'), input=form?.querySelector('#chatInput'); return menu&&!menu.open && !menu.querySelector('.composer-tool-options').getClientRects().length && document.documentElement.scrollWidth<=innerWidth && form.getBoundingClientRect().height<innerHeight*.35 && input.getBoundingClientRect().height>=48 && input.getAttribute('aria-label') ? 'mobile-composer' : ''; })()`, "mobile-composer")
			if _, err := client.evaluate(ctx, `document.querySelector('.composer-tool-menu > summary').click(); 'open-tools'`); err != nil {
				t.Fatal(err)
			}
			waitBrowserValue(t, ctx, client, `(() => { const buttons=[...document.querySelectorAll('.composer-tool-button')]; return buttons.length>=5 && buttons.every(button=>{const r=button.getBoundingClientRect(),hit=document.elementFromPoint(r.x+r.width/2,r.y+r.height/2);return r.height>=44 && r.left>=0 && r.right<=innerWidth && (hit===button||button.contains(hit));}) ? 'tools-reachable' : ''; })()`, "tools-reachable")
			if _, err := client.evaluate(ctx, `document.querySelector('.composer-tool-menu > summary').click(); 'close-tools'`); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := client.evaluate(ctx, `(() => {const box=document.querySelector('#chatInput');box.value='/';box.dispatchEvent(new Event('input',{bubbles:true}));return 'commands';})()`); err != nil {
			t.Fatal(err)
		}
		waitBrowserValue(t, ctx, client, `(() => {const menu=document.querySelector('.slash-menu'), input=document.querySelector('#chatInput'), bubble=document.querySelector('.character-bubble');const m=menu?.getBoundingClientRect(),i=input?.getBoundingClientRect(),b=bubble?.getBoundingClientRect();return m && i && !menu.hidden && m.top>=0 && m.bottom<=innerHeight && i.bottom<=innerHeight ? 'command-popup-visible' : '';})()`, "command-popup-visible")
		if _, err := client.evaluate(ctx, `document.querySelector('#chatInput').value='';document.querySelector('#chatInput').dispatchEvent(new Event('input',{bubbles:true}));'commands-closed'`); err != nil {
			t.Fatal(err)
		}

		waitBrowserValue(t, ctx, client, `document.querySelector('#messageList .message-body strong')?.textContent === 'Two IDs' && document.querySelectorAll('#messageList .message-body li').length === 2 ? 'formatted' : ''`, "formatted")
		if _, err := client.evaluate(ctx, `document.querySelector('#messageList').scrollTop=0; renderChat(); 'rerendered'`); err != nil {
			t.Fatal(err)
		}
		waitBrowserValue(t, ctx, client, `document.querySelector('#messageList')?.scrollTop === 0 ? 'reading-position-preserved' : ''`, "reading-position-preserved")
		if _, err := client.evaluate(ctx, `state.followSubmittedTurn=true; renderChat(); state.followSubmittedTurn=false; 'submitted-follow'`); err != nil {
			t.Fatal(err)
		}
		waitBrowserValue(t, ctx, client, `(() => {const log=document.querySelector('#messageList');return log.scrollHeight-log.scrollTop-log.clientHeight<2?'latest-visible':''})()`, "latest-visible")
		if directory := os.Getenv("HERMETRIX_SETTINGS_AUDIT_DIR"); directory != "" {
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
			if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("chat-%d.png", width)), pixels, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestWorkspacePaneSizesAreIndependentInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	server := testHTTPServer(t)
	requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Resizable workspace", "root_path": ""}, http.StatusCreated)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
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
	for _, method := range []string{"Runtime.enable", "Page.enable"} {
		if err := client.call(ctx, method, map[string]any{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1600, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Page.navigate", map[string]any{"url": server.URL}, nil); err != nil {
		t.Fatal(err)
	}
	capture := func(name string) {
		directory := os.Getenv("HERMETRIX_SETTINGS_AUDIT_DIR")
		if directory == "" {
			return
		}
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
		if err := os.WriteFile(filepath.Join(directory, name), pixels, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-open-project]').click(); 'project-open'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.currentProject?.name === 'Resizable workspace' ? 'project-ready' : ''`, "project-ready")
	if _, err := client.evaluate(ctx, `state.panes=['chat','review','files']; document.querySelector('#toggleSide').click(); 'side-open'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const side=document.querySelector('#zoneSide'); return !document.querySelector('#zones').classList.contains('side-hidden') && side.querySelectorAll('.pane:not(.pane-hidden)').length===1 && side.scrollWidth<=side.clientWidth ? 'single-side-pane' : ''; })()`, "single-side-pane")
	if _, err := client.evaluate(ctx, `document.querySelector('#paneAdd').click(); document.querySelector('[data-open-side-pane="browser"]').click(); 'browser-open'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const side=document.querySelector('#zoneSide'); return side.querySelector('.pane:not(.pane-hidden)')?.dataset.kind==='browser' && side.querySelectorAll('.pane:not(.pane-hidden)').length===1 && side.scrollWidth<=side.clientWidth && document.querySelector('#workspacePaneMenu')?.hidden ? 'browser-side-pane' : ''; })()`, "browser-side-pane")
	capture("chat-side-workbench.png")
	if _, err := client.evaluate(ctx, `switchView('code'); state.panes=['chat','editor','environment']; state.paneLayout='adaptive'; renderPanes(); 'panes-open'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const strip=document.querySelector('.pane-grid'), handle=strip?.querySelector('[data-pane-size="0"]'); if(!strip||!handle||getComputedStyle(strip).display!=='flex')return ''; const r=handle.getBoundingClientRect(),hit=document.elementFromPoint(r.left+r.width/2,r.top+r.height/2); return hit===handle ? 'resize-handles-reachable' : ''; })()`, "resize-handles-reachable")
	if err := client.waitForPaint(ctx); err != nil {
		t.Fatal(err)
	}
	capture("workspace-before-resize.png")
	coords, err := client.evaluate(ctx, `(() => {const r=document.querySelector('[data-pane-size="0"]').getBoundingClientRect();return JSON.stringify({x:r.left+r.width/2,y:r.top+r.height/2});})()`)
	if err != nil {
		t.Fatal(err)
	}
	var point struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if err := json.Unmarshal([]byte(coords), &point); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		kind string
		x    float64
	}{{"mouseMoved", point.X}, {"mousePressed", point.X}, {"mouseMoved", point.X + 115}, {"mouseReleased", point.X + 115}} {
		params := map[string]any{"type": input.kind, "x": input.x, "y": point.Y}
		if input.kind == "mouseMoved" && input.x > point.X {
			params["buttons"] = 1
		}
		if input.kind == "mousePressed" || input.kind == "mouseReleased" {
			params["button"] = "left"
			params["clickCount"] = 1
		}
		if err := client.call(ctx, "Input.dispatchMouseEvent", params, nil); err != nil {
			t.Fatal(err)
		}
	}
	capture("workspace-after-drag.png")
	waitBrowserValue(t, ctx, client, `state.paneWidths.chat > 500 && document.querySelector('.pane-index-0').getBoundingClientRect().width > 500 ? 'drag-resized' : JSON.stringify({saved:state.paneWidths.chat,width:document.querySelector('.pane-index-0').getBoundingClientRect().width})`, "drag-resized")
	capture("workspace-dynamic.png")
	if _, err := client.evaluate(ctx, `state.panes=['chat','editor','environment','output']; renderPanes(); saveLayout(); 'four-panes'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const panes=[...document.querySelectorAll('.pane-grid>.pane')],strip=document.querySelector('.workspace-pane-host'); return panes.length===4 && panes.every(p=>p.getBoundingClientRect().width>=320) && strip.scrollWidth>strip.clientWidth ? 'readable-scroll' : ''; })()`, "readable-scroll")
	waitBrowserValue(t, ctx, client, `(() => { const body=document.querySelector('.pane-index-0 .pane-body[data-pane-kind="chat"]'),composer=body?.querySelector('.pane-chat-input'); if(!body||!composer)return ''; body.scrollTop=body.scrollHeight; const box=body.getBoundingClientRect(),input=composer.getBoundingClientRect(); return input.top>=box.top && input.bottom<=box.bottom ? 'composer-reachable' : ''; })()`, "composer-reachable")
	capture("workspace-four.png")
	if _, err := client.evaluate(ctx, `HermetrixWorkspace.open('terminal'); 'ide'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.ide-host [data-pane-size="0"]') && getComputedStyle(document.querySelector('.ide-host .pane-grid')).display==='flex' ? 'ide-resizable' : ''`, "ide-resizable")
	capture("workspace-editor.png")
	if _, err := client.evaluate(ctx, `const divider=document.querySelector('[data-pane-size="0"]'); divider.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowLeft',bubbles:true})); 'keyboard-resize'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.paneWidths[state.panes[0]] < 600 && JSON.parse(localStorage.getItem(layoutKey())).paneWidths[state.panes[0]] === state.paneWidths[state.panes[0]] ? 'size-persisted' : ''`, "size-persisted")
	if _, err := client.evaluate(ctx, `document.querySelector('#workspaceIDE').click(); 'flexible'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.paneLayout === 'adaptive' && document.querySelector('.pane-arrangement-adaptive') && state.panes.includes('terminal') ? 'flexible-restored' : ''`, "flexible-restored")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-ide-panel="terminal"]').click(); 'terminal'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.paneLayout === 'adaptive' && state.panes.includes('terminal') ? 'terminal-keeps-layout' : ''`, "terminal-keeps-layout")
	if _, err := client.evaluate(ctx, `splitPane(); splitPane(); 'more-than-four'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.panes.length>4 && document.querySelectorAll('.pane-grid>.pane').length===state.panes.length ? 'more-than-four' : ''`, "more-than-four")
	if _, err := client.evaluate(ctx, `window.__paneAuditErrors=[];window.addEventListener("error",e=>window.__paneAuditErrors.push(e.error?.stack||e.message)); window.__paneCountBefore = state.panes.length; document.querySelector('#paneAdd').click(); 'create-pane'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!document.querySelector('#workspacePaneMenu').hidden && state.panes.length===window.__paneCountBefore ? 'choose-before-create' : ''`, "choose-before-create")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-open-side-pane="notes"]').click(); 'create-selected-tab'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => {const facts={before:window.__paneCountBefore,after:state.panes.length,preset:!!document.querySelector('#workspacePreset'),rendered:document.querySelectorAll('.pane-grid>.pane').length,saved:JSON.parse(localStorage.getItem(layoutKey()))?.panes?.length,scrollLeft:document.querySelector('#workspacePaneHost').scrollLeft};return facts.after===facts.before+1 && !facts.preset && facts.rendered===facts.after && facts.saved===facts.after && facts.scrollLeft>0 ? 'pane-created-visible' : JSON.stringify(facts);})()`, "pane-created-visible")
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": true}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#workspaceToolbarToggle')?.getAttribute('aria-expanded') === 'false' && getComputedStyle(document.querySelector('.workspace-actions')).display === 'none' ? 'mobile-menu-collapsed' : ''`, "mobile-menu-collapsed")
	if _, err := client.evaluate(ctx, `document.querySelector('#workspaceToolbarToggle').click(); 'open-workspace-menu'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => {const r=document.querySelector('#paneAdd')?.getBoundingClientRect();return r&&r.width>=44&&r.left>=-1&&r.right<=innerWidth+1&&r.height>=36?'mobile-create-reachable':''})()`, "mobile-create-reachable")
	if _, err := client.evaluate(ctx, `document.querySelector('#paneAdd').click(); document.querySelector('[data-open-side-pane="notes"]').click(); 'create-mobile-pane'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => {const last=state.panes.at(-1),pane=document.querySelector('.pane-grid>.pane:last-of-type');return state.panes.length===window.__paneCountBefore+2 && state.compactPane===last && pane&&!pane.classList.contains('pane-hidden') && pane.querySelector('.pane-body')?.childElementCount>0 && document.querySelector('[data-compact-pane="'+last+'"]')?.getAttribute('aria-pressed')==='true'?'mobile-pane-open':''})()`, "mobile-pane-open")
	capture("workspace-created-mobile.png")
}

func TestModelManagementFlowInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"HERMETRIX_OK"},"finish_reason":"stop"}]}`)
	}))
	defer gateway.Close()
	server := testHTTPServer(t)
	requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Models flow", "root_path": ""}, http.StatusCreated)
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
	expectBrowser := func(expression, want string) {
		t.Helper()
		value, err := client.evaluate(ctx, expression)
		if err != nil || value != want {
			t.Fatalf("browser action: got %q, want %q: %v", value, want, err)
		}
	}
	for _, method := range []string{"Runtime.enable", "Page.enable"} {
		if err := client.call(ctx, method, map[string]any{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1440, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Page.navigate", map[string]any{"url": server.URL}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'picker' : ''`, "picker")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-open-project]').click(); 'opened'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#startPrompt') ? 'workspace' : ''`, "workspace")
	if _, err := client.evaluate(ctx, `document.querySelector('#openConfig').click(); document.querySelector('[data-config-page="providers"]').click(); 'models'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#modelAdd') ? 'models' : ''`, "models")
	expectBrowser(`document.querySelector('#modelAdd').click(); document.querySelector('#modelConnect').open && document.activeElement.name==='name' ? 'add-ready' : ''`, "add-ready")
	if _, err := client.evaluate(ctx, fmt.Sprintf(`(() => {const f=document.querySelector('#providerForm');f.elements.namedItem('name').value='Local test';f.elements.namedItem('base_url').value=%q;f.elements.namedItem('model').value='test-model';f.requestSubmit();return 'submitted';})()`, gateway.URL+"/v1")); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.model-card h3')?.textContent==='Local test' ? 'added' : ''`, "added")
	expectBrowser(`(() => {document.querySelector('[data-edit-provider]').click(); const form=document.querySelector('#providerForm'); form.elements.namedItem('name').value='Do not save'; document.querySelector('#providerCancel').click(); return document.querySelector('.model-card h3').textContent==='Local test' && !document.querySelector('#modelConnect').open ? 'cancel-kept-model' : '';})()`, "cancel-kept-model")
	if _, err := client.evaluate(ctx, `(() => {document.querySelector('[data-edit-provider]').click(); const form=document.querySelector('#providerForm'); form.elements.namedItem('name').value='Local edited'; form.requestSubmit(); return 'edited';})()`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.model-card h3')?.textContent==='Local edited' ? 'edit-saved' : ''`, "edit-saved")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-test-provider]').click(); 'testing'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-provider-test-result]')?.textContent.includes('เชื่อมต่อสำเร็จ') ? 'test-visible' : ''`, "test-visible")
	expectBrowser(`(() => {document.querySelector('[data-open-qualification]').click(); const details=document.querySelector('[data-model-details]'),form=details.querySelector('form');form.elements.namedItem('runtime').value='ollama';form.elements.namedItem('runtime').dispatchEvent(new Event('change'));return details.open && !form.querySelector('.model-runtime-endpoint').hidden ? 'qualification-options' : '';})()`, "qualification-options")
	if directory := os.Getenv("HERMETRIX_SETTINGS_AUDIT_DIR"); directory != "" {
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
		if err := os.WriteFile(filepath.Join(directory, "models-flow.png"), pixels, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": true}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.documentElement.scrollWidth <= window.innerWidth && document.querySelector('.model-card').getBoundingClientRect().width <= window.innerWidth ? 'mobile-fits' : ''`, "mobile-fits")
}

func TestSettingsPagesAndBackgroundJobsInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	server := testHTTPServer(t)
	requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Visual audit", "root_path": ""}, http.StatusCreated)
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
	for _, method := range []string{"Runtime.enable", "Page.enable"} {
		if err := client.call(ctx, method, map[string]any{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.call(ctx, "Page.navigate", map[string]any{"url": server.URL}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]')?.textContent.includes('Visual audit') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, `window.__settingsAuditErrors=[]; window.addEventListener('error', event => window.__settingsAuditErrors.push(event.message)); window.addEventListener('unhandledrejection', event => window.__settingsAuditErrors.push(String(event.reason))); 'error-listeners-ready'`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `openProject(document.querySelector('[data-open-project]').dataset.openProject).then(async () => { switchTab('office'); await hydrateSurface('office'); window.__officeNavigationReady=true; }).catch(error => {window.__e2eOpenProjectError=String(error)}); 'opened'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `window.__officeNavigationReady && document.querySelector('#officeList') && readySurfaces.has('office') ? 'office' : ''`, "office")
	// This test injects its own job history. Keep the live five-second poll from
	// replacing that fixture with the server's unrelated background jobs.
	if _, err := client.evaluate(ctx, `stopOfficePoll(); scheduleOfficePoll = () => {}; 'poll-paused'`); err != nil {
		t.Fatal(err)
	}
	seed := `state.jobs = [
		{id:'job_running',state:'running',created_at:'2026-09-24T08:00:00Z',started_at:'2026-09-24T08:01:00Z',payload:{executable:'go',arguments:['test','./...'],project_id:state.projects[0].id,actor:'tester'},result:{}},
		{id:'job_interrupted',state:'interrupted',created_at:'2026-09-24T07:00:00Z',started_at:'2026-09-24T07:01:00Z',payload:{executable:'node',arguments:['build'],project_id:state.projects[0].id,actor:'tester'},result:{}}
	]; state.reviews = [{id:'review_queued',state:'queued',created_at:'2026-09-24T09:00:00Z',trigger_kind:'successful_milestone',digest:{goal_and_constraints:'Review the completed work'},attempts:0}]; renderOffice(); clearTimeout(officePollTimer); 'seeded'`
	if _, err := client.evaluate(ctx, seed); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('.office-entry').length === 3 && document.querySelector('#view-office').textContent.includes('จะไม่รันคำสั่งนี้ซ้ำเอง') && document.querySelector('#view-office').textContent.includes('กลับเข้าคิวหลังเปิดแอป') ? 'clear-states' : ''`, "clear-states")
	if path := os.Getenv("HERMETRIX_OFFICE_SCREENSHOT"); path != "" {
		if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1740, "height": 1000, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
			t.Fatal(err)
		}
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
		if err := os.WriteFile(path, pixels, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.evaluate(ctx, `document.querySelector('[data-office-filter="attention"]').click(); 'filtered'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('.office-entry').length === 1 && document.querySelector('.office-entry').textContent.includes('node build') ? 'filtered' : ''`, "filtered")
	if _, err := client.evaluate(ctx, `document.querySelector('.office-entry summary').click(); 'expanded'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.office-entry').open && document.querySelector('.office-timeline').textContent.includes('เริ่ม') ? 'timeline' : ''`, "timeline")
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 700, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.documentElement.scrollWidth <= window.innerWidth ? 'fits' : ''`, "fits")
	if path := os.Getenv("HERMETRIX_OFFICE_MOBILE_SCREENSHOT"); path != "" {
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
		if err := os.WriteFile(path, pixels, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1740, "height": 1000, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	pageResult, err := client.evaluate(ctx, `(() => { state.jobs = Array.from({length:25}, (_, index) => ({id:'history_'+index,state:'completed',created_at:new Date(Date.UTC(2026,8,24,0,index)).toISOString(),payload:{executable:'git',arguments:['status'],project_id:state.projects[0].id},result:{exit_code:0}})); state.reviews = []; officeFilter='history'; officeHistoryLimit=20; renderOffice(); stopOfficePoll(); const first=document.querySelectorAll('.office-entry').length===20&&!document.querySelector('#officeMore').hidden; document.querySelector('#officeMore').click(); const all=document.querySelectorAll('.office-entry').length===25&&document.querySelector('#officeMore').hidden; return first&&all?'all-history':JSON.stringify({first,all,rows:document.querySelectorAll('.office-entry').length}); })()`)
	if err != nil || pageResult != "all-history" {
		t.Fatalf("office history pagination: result=%q err=%v", pageResult, err)
	}
	for _, page := range []string{"providers", "mcp", "tools", "library", "proposals", "learning", "insights", "archive", "context", "fidelity", "discord", "projects", "artifacts", "maintenance"} {
		expression := fmt.Sprintf(`document.querySelector('[data-config-page=%q]')?.click(); document.querySelector('#view-%s')?.textContent.trim().length > 0 ? 'rendered' : ''`, page, page)
		if _, err := client.evaluate(ctx, expression); err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		waitBrowserValue(t, ctx, client, fmt.Sprintf(`document.querySelector('#view-%s')?.textContent.trim().length > 0 ? 'rendered' : ''`, page), "rendered")
		unsafe, err := client.evaluate(ctx, fmt.Sprintf(`(() => {const view=document.querySelector('#view-%s');return [...view.querySelectorAll('form button')].filter(button => /^(cancel|close|ยกเลิก|ปิด)$/i.test((button.textContent.trim() || button.getAttribute('aria-label') || '')) && button.type !== 'button').map(button => button.outerHTML).join('\n');})()`, page))
		if err != nil {
			t.Fatal(err)
		}
		if unsafe != "" {
			t.Errorf("%s: dismiss button would submit its form: %s", page, unsafe)
		}
		if directory := os.Getenv("HERMETRIX_SETTINGS_AUDIT_DIR"); directory != "" {
			if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1740, "height": 1000, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
				t.Fatal(err)
			}
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
			if err := os.WriteFile(filepath.Join(directory, page+".png"), pixels, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	for _, page := range []string{"providers", "mcp", "tools", "library", "proposals", "learning", "insights", "archive", "context", "fidelity", "discord", "projects", "artifacts", "maintenance"} {
		width, err := client.evaluate(ctx, fmt.Sprintf(`(() => { const jump=document.querySelector('#configMobileJump'); jump.value=%q; jump.dispatchEvent(new Event('change',{bubbles:true})); return document.documentElement.scrollWidth > innerWidth + 1 ? String(document.documentElement.scrollWidth) : ''; })()`, page))
		if err != nil {
			t.Fatalf("%s mobile layout: %v", page, err)
		}
		if width != "" {
			t.Errorf("%s overflows 390px viewport: document width %s", page, width)
		}
		waitBrowserValue(t, ctx, client, fmt.Sprintf(`(() => { const jump=document.querySelector('#configMobileJump'), nav=document.querySelector('#configNav'); if (!jump || jump.value!==%q || getComputedStyle(nav).display!=='none') return ''; const rect=jump.getBoundingClientRect(), hit=document.elementFromPoint(rect.left+rect.width/2,rect.top+rect.height/2); return hit===jump ? 'nav-reachable' : ''; })()`, page), "nav-reachable")
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#configMobileJump').focus(); 'focused'`); err != nil {
		t.Fatal(err)
	}
	for _, eventType := range []string{"keyDown", "keyUp"} {
		if err := client.call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": eventType, "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27}, nil); err != nil {
			t.Fatal(err)
		}
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#configOverlay').hidden && document.activeElement?.id==='openConfig' && document.querySelector('#appHeader').getAttribute('aria-hidden')==='false' ? 'focus-returned' : ''`, "focus-returned")
	if errors, err := client.evaluate(ctx, `window.__settingsAuditErrors.join('\n')`); err != nil {
		t.Fatal(err)
	} else if errors != "" {
		t.Errorf("settings navigation raised browser errors: %s", errors)
	}
}

// TestGoalComposerStartsAConversationInARealBrowser exercises the first-run
// path with an unavailable provider ahead of a usable one. It verifies the
// actual streamed reply and persisted project binding, not just enabled UI.
func TestGoalComposerStartsAConversationInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome is required for the UI E2E job")
		}
		t.Skip("Chrome is not installed")
	}
	modelRuntime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Ready from the browser fixture.\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":8,\"total_tokens\":28}}\n\ndata: [DONE]\n\n")
	}))
	defer modelRuntime.Close()
	var requestMu sync.Mutex
	requests := make(map[string]int)
	handler := testHandler(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMu.Lock()
		requests[r.Method+" "+r.URL.Path]++
		requestMu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	requestJSON(t, server.URL+"/api/providers", http.MethodPost, map[string]any{
		"name": "A unavailable remote", "adapter_kind": "openai-compatible", "base_url": modelRuntime.URL + "/v1",
		"model": "missing-key", "api_key_env": "HERMETRIX_E2E_UNSET_KEY", "context_window": 32768,
		"context_evidence": "declared", "max_output_tokens": 4096}, http.StatusCreated)
	readyBody := requestJSON(t, server.URL+"/api/providers", http.MethodPost, map[string]any{
		"name": "B ready local", "adapter_kind": "openai-compatible", "base_url": modelRuntime.URL + "/v1",
		"model": "browser-fixture", "context_window": 32768, "context_evidence": "declared", "max_output_tokens": 4096}, http.StatusCreated)
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "README.md"), []byte("# Browser fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectBody := requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Goal composer project", "root_path": projectRoot}, http.StatusCreated)
	var provider, project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(readyBody, &provider); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(projectBody, &project); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
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
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1440, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, fmt.Sprintf(`document.querySelector('[data-open-project="%s"]').click(); 'opened'`, project.ID)); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#startTaskForm #startPrompt') && document.querySelector('#startChatButton') ? 'composer' : ''`, "composer")
	waitBrowserValue(t, ctx, client, `document.querySelector('#startModelOptions')?.textContent.includes('B ready local') && !document.querySelector('#startChatButton').disabled ? 'ready-model' : ''`, "ready-model")
	waitBrowserValue(t, ctx, client, `document.querySelector('#zones').classList.contains('side-hidden') && !document.querySelector('#view-providers').children.length && !document.querySelector('#view-office').children.length ? 'focused' : ''`, "focused")
	requestMu.Lock()
	for _, path := range []string{"GET /vendor/ide.js", "GET /vendor/ide.css", "GET /api/backups", "GET /api/memories", "GET /api/fidelity/runs", "POST /api/terminals"} {
		if requests[path] != 0 {
			t.Errorf("starting a conversation triggered unrelated work: %s (%d requests)", path, requests[path])
		}
	}
	requestMu.Unlock()
	const objective = "Check that this project can start a conversation."
	if _, err := client.evaluate(ctx, `document.querySelector('#startPrompt').focus(); 'focused'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.insertText", map[string]any{"text": objective}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `Array.from(document.querySelectorAll('.chat-message.assistant:not(.streaming) .message-body')).some(node => node.textContent.includes('Ready from the browser fixture.')) ? 'replied' : ''`, "replied")
	waitBrowserValue(t, ctx, client, `(() => { const meter=document.querySelector('.composer-usage .session-usage'); return meter && !meter.open && meter.querySelector('summary').textContent.includes('28') && !meter.querySelector('.token-meter-detail').getClientRects().length ? 'usage-collapsed' : ''; })()`, "usage-collapsed")
	if _, err := client.evaluate(ctx, `document.querySelector('.composer-usage .session-usage summary').click(); 'details-opened'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const meter=document.querySelector('.composer-usage .session-usage'); return meter?.open && meter.textContent.includes('รับเข้า / ตอบออก') && meter.textContent.includes('20 / 8 token') && meter.textContent.includes('ขั้นโมเดลของคำสั่งล่าสุด') ? 'usage-details-visible' : ''; })()`, "usage-details-visible")
	waitBrowserValue(t, ctx, client, `(() => { const meter=document.querySelector('.composer-usage .session-usage'), bar=meter?.querySelector('progress'); return meter?.textContent.includes('ใช้รวมในแชต 28 token') && bar?.value===28 && bar.max>28 && bar.getBoundingClientRect().width>150 ? 'usage-bar-visible' : ''; })()`, "usage-bar-visible")
	if _, err := client.evaluate(ctx, `renderChat(); 'rerendered'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.composer-usage .session-usage')?.open ? 'usage-details-survived' : ''`, "usage-details-survived")
	waitBrowserValue(t, ctx, client, `(() => { const sample=[{turn_id:'old',event_kind:'message',role:'user'},{turn_id:'old',event_kind:'tool_call',metadata:{usage:{total_tokens:11,prompt_tokens:9,completion_tokens:2}}},{turn_id:'old',event_kind:'model_step_bound'},{turn_id:'old',event_kind:'turn_failed',content:'agent exhausted its 12 model-step budget'}]; const html=sessionUsageHTML({contract:{task_budget:{max_model_steps:12,max_cumulative_tokens:98304}}},sample,null); return html.includes('อย่างน้อย 11 token') && html.includes('1 / 12 ขั้น') && html.includes('เพราะครบ 12 ขั้นโมเดล') ? 'old-failure-estimated' : ''; })()`, "old-failure-estimated")
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`Array.from(document.querySelectorAll('.chat-message.user .message-body')).some(node => node.textContent === %q) ? 'goal-sent' : ''`, objective), "goal-sent")
	response, err := http.Get(server.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var sessions []struct {
		ProjectID  string `json:"project_id"`
		ProviderID string `json:"provider_id"`
		Title      string `json:"title"`
	}
	if err := json.NewDecoder(response.Body).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ProjectID != project.ID || sessions[0].ProviderID != provider.ID || sessions[0].Title != objective {
		t.Fatalf("the goal composer did not persist the intended session: %+v", sessions)
	}
	// A selected text attachment reaches the turn as text, retains focus, and
	// does not require a vision model. Exercise the browser file-input API.
	attachmentPath := filepath.Join(projectRoot, "attached.txt")
	if err := os.WriteFile(attachmentPath, []byte("ATTACHMENT-E2E-927"), 0o600); err != nil {
		t.Fatal(err)
	}
	var dom struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := client.call(ctx, "DOM.getDocument", map[string]any{}, &dom); err != nil {
		t.Fatal(err)
	}
	var fileNode struct {
		NodeID int `json:"nodeId"`
	}
	if err := client.call(ctx, "DOM.querySelector", map[string]any{"nodeId": dom.Root.NodeID, "selector": "#composerFileInput"}, &fileNode); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "DOM.setFileInputFiles", map[string]any{"nodeId": fileNode.NodeID, "files": []string{attachmentPath}}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#composerAttachments')?.textContent.includes('attached.txt') && document.activeElement?.id === 'chatInput' ? 'attachment-focused' : ''`, "attachment-focused")
	if err := client.call(ctx, "Input.insertText", map[string]any{"text": "Read the attached file."}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!state.sending && state.sessionDetail.events.some(event=>event.role==='user' && JSON.stringify(event).includes('ATTACHMENT-E2E-927')) && !document.querySelector('#composerAttachments')?.textContent.trim() && document.activeElement?.id==='chatInput' ? 'attachment-sent-focused' : ''`, "attachment-sent-focused")
	// Ask next to an edited file, keeping its unsaved buffer intact. The local
	// pane must render the complete persisted reply, not just a toast or tail.
	if _, err := client.evaluate(ctx, `document.querySelector('#viewSwitch [data-view="code"]').click(); 'workspace'`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#paneAdd').click(); 'tool-chooser'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const menu=document.querySelector('#workspacePaneMenu'),option=menu?.querySelector('button:not(:disabled)'); return menu && !menu.hidden && getComputedStyle(menu).backgroundColor === 'rgb(37, 39, 43)' && option && getComputedStyle(option).color !== 'rgb(0, 0, 0)' ? 'dark-pane-menu' : ''; })()`, "dark-pane-menu")
	if _, err := client.evaluate(ctx, `document.querySelector('#paneAdd').click(); 'chooser-closed'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-workbench-file="README.md"]') ? 'readme' : ''`, "readme")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-workbench-file="README.md"]').click(); 'readme-open'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.cm-editor') ? 'editor' : ''`, "editor")
	if _, err := client.evaluate(ctx, `document.querySelector('.cm-content').focus(); 'editing'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.insertText", map[string]any{"text": "Unsaved local context\n"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#codeAskAI').click(); 'ask-local-ai'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`document.querySelector('[data-assistant-provider]')?.value === %q && document.querySelectorAll('[data-assistant-provider] option').length === 1 ? 'local-model-only' : ''`, provider.ID), "local-model-only")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-assistant-action="explain"]').click(); 'explain'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!state.sending && document.querySelectorAll('.ide-assistant-log .chat-message.assistant').length >= 3 ? 'coding-replied' : (document.querySelector('.ide-assistant-status')?.textContent || '')`, "coding-replied")
	if _, err := client.evaluate(ctx, `(() => { const meter=document.querySelector('.ide-assistant-usage .session-usage'); if(!meter.open) meter.querySelector('summary').click(); return 'coding-usage-open'; })()`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => {
  const meter = document.querySelector('.ide-assistant-usage .session-usage');
  const bar = meter.querySelector('progress');
  return (meter.textContent.includes('ใช้รวมในแชต 84 token') && bar.value === 28 && bar.getBoundingClientRect().width > 100) ? 'coding-usage-bar-visible' : '';
})()`, "coding-usage-bar-visible")
	if _, err := client.evaluate(ctx, `document.querySelector('.ide-assistant-usage .session-usage summary').click(); 'coding-usage-close'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `[...document.querySelectorAll('.ide-assistant-log .chat-message.user')].some(node=>node.textContent.includes('unsaved editor buffer') && node.textContent.includes('Unsaved local context')) && activeCodeEditor?.getValue().includes('Unsaved local context') ? 'context-and-draft-preserved' : ''`, "context-and-draft-preserved")
	if content, err := os.ReadFile(filepath.Join(projectRoot, "README.md")); err != nil || string(content) != "# Browser fixture\n" {
		t.Fatalf("asking the local assistant changed the unsaved file on disk: %q %v", content, err)
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('[data-compact-pane="chat"]')?.click(); 'mobile-chat-pane'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const meter=document.querySelector('.ide-assistant-usage .session-usage'), box=meter?.getBoundingClientRect(); return meter && !meter.open && box.left>=0 && box.right<=innerWidth+1 && meter.querySelector('summary').getBoundingClientRect().height>=36 ? 'mobile-usage-fits' : ''; })()`, "mobile-usage-fits")
	if _, err := client.evaluate(ctx, `document.querySelector('.ide-assistant .pane-chat-input').focus(); 'mobile-composer-focused'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const summary=document.querySelector('.ide-assistant-usage summary'); return summary?.getBoundingClientRect().width>100 && summary.textContent.includes('28') ? 'mobile-usage-stays-visible' : ''; })()`, "mobile-usage-stays-visible")
	if _, err := client.evaluate(ctx, `document.querySelector('.ide-assistant .pane-chat-input').blur(); 'mobile-composer-blurred'`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `for(let i=0;i<12;i++){const id='mobile-tool-'+i;state.sessionDetail.events.push({id:id+'-call',event_kind:'tool_call',role:'assistant',metadata:{tool_call_id:id,tool_name:'workspace.read_file'},content:'{}'},{id:id+'-result',event_kind:'tool_result',role:'tool',metadata:{tool_call_id:id,tool_name:'workspace.read_file',tool_status:'succeeded'},content:JSON.stringify({name:'workspace.read_file',status:'succeeded',output:'file read'})})} HermetrixAssistant.refresh(); 'tool-history'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const batch=document.querySelector('.ide-tool-batch'), form=document.querySelector('.ide-assistant .pane-chat-form'), input=form?.querySelector('textarea'), send=form?.querySelector('[data-assistant-send]'); if(!batch||!input||!send)return ''; const i=input.getBoundingClientRect(),s=send.getBoundingClientRect(); return batch.querySelectorAll('.tool-receipt').length===12 && i.width>=320 && s.top>=i.bottom && input.placeholder==='พิมพ์คำถามหรือสั่งแก้ไฟล์…' ? 'mobile-assistant' : ''; })()`, "mobile-assistant")
	waitBrowserValue(t, ctx, client, `!document.querySelector('.ide-assistant-title strong') && document.querySelector('[data-assistant-provider]') ? 'single-model-label' : ''`, "single-model-label")
	if _, err := client.evaluate(ctx, `document.querySelector('.ide-tool-batch summary').click(); 'expanded-tools'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const log=document.querySelector('.ide-assistant-log'), receipt=log?.querySelector('.ide-tool-batch .tool-receipt'); return receipt?.getBoundingClientRect().height>=40 && log.scrollHeight>log.clientHeight ? 'receipts-readable' : ''; })()`, "receipts-readable")
	waitBrowserValue(t, ctx, client, `(() => { const input=document.querySelector('.ide-assistant .pane-chat-input'); const key=new KeyboardEvent('keydown',{key:'Enter',shiftKey:true,bubbles:true,cancelable:true}); input?.dispatchEvent(key); return input && !key.defaultPrevented && !state.sending ? 'mobile-shift-enter-newline' : ''; })()`, "mobile-shift-enter-newline")
	if _, err := client.evaluate(ctx, `document.querySelector('.ide-assistant .pane-chat-input').focus(); 'typing'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 550, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const input=document.querySelector('.ide-assistant .pane-chat-input'), send=document.querySelector('[data-assistant-send]'), rect=input?.getBoundingClientRect(), button=send?.getBoundingClientRect(); return document.activeElement===input && rect.top>=0 && button.bottom<=innerHeight && getComputedStyle(document.querySelector('.app-header')).display==='none' && document.documentElement.scrollWidth<=innerWidth ? 'keyboard-space' : ''; })()`, "keyboard-space")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-assistant-dismiss]').click(); 'done-typing'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#viewSwitch [data-view="chat"]').click(); 'return-chat'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#messageList .ide-tool-batch')?.querySelectorAll('.tool-receipt').length === 12 ? 'chat-tools-grouped' : ''`, "chat-tools-grouped")
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#chatInput').focus(); 'focus-draft'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.insertText", map[string]any{"text": "Keep this chat draft after browsing files."}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('.composer-tool-menu summary').click(); 'open-tools'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => {const el=document.querySelector('#composerFilesButton');const r=el.getBoundingClientRect();const at=document.elementFromPoint(r.x+r.width/2,r.y+r.height/2);return at&&(at===el||el.contains(at))?'files-button-reachable':'';})()`, "files-button-reachable")
	if _, err := client.evaluate(ctx, `document.querySelector('#composerFilesButton').click(); 'browse-files'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#viewSwitch [data-view="code"]').classList.contains('on') && document.querySelector('[data-workbench-file="README.md"]')?.getClientRects().length ? 'files-visible' : ''`, "files-visible")
	if _, err := client.evaluate(ctx, `document.querySelector('#viewSwitch [data-view="chat"]').click(); 'return-chat'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#chatInput')?.value === 'Keep this chat draft after browsing files.' ? 'chat-draft-preserved' : ''`, "chat-draft-preserved")
	// Fail before the request reaches the server, then exercise the visible
	// retry action. The original draft must survive and commit exactly once.
	if _, err := client.evaluate(ctx, `window.__retryFetch=window.fetch; window.fetch=async (...args)=>{if(String(args[0]).endsWith('/turns')){window.fetch=window.__retryFetch;return new Response(JSON.stringify({error:'temporary gateway failure'}),{status:503,headers:{'Content-Type':'application/json'}})}return window.__retryFetch(...args)}; document.querySelector('#chatForm').requestSubmit(); 'failed-send'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#retryChatTurn') && document.querySelector('#chatInput')?.value==='Keep this chat draft after browsing files.' && !state.sending ? 'retry-ready':''`, "retry-ready")
	if _, err := client.evaluate(ctx, `document.querySelector('#retryChatTurn').click(); 'retry-clicked'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!state.sending && !state.chatTurnError && state.sessionDetail.events.filter(e=>e.role==='user'&&e.content==='Keep this chat draft after browsing files.').length===1 && document.querySelector('#chatInput')?.value==='' ? 'retried-once':''`, "retried-once")
	if err := client.call(ctx, "Emulation.clearDeviceMetricsOverride", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#railNewSession').click(); 'new-task'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#startPrompt') ? 'new-goal' : ''`, "new-goal")
	const plannedGoal = "Improve the project navigation."
	if _, err := client.evaluate(ctx, `document.querySelector('#startPrompt').focus(); 'focused'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.insertText", map[string]any{"text": plannedGoal}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#startPlanButton').click(); 'plan'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#actionDialog')?.open ? 'plan-review' : ''`, "plan-review")
	if _, err := client.evaluate(ctx, `document.querySelector('#actionConfirm').click(); 'confirmed'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`document.querySelector('#durableTaskForm textarea[name="objective"]')?.value === %q ? 'goal-preserved' : ''`, plannedGoal), "goal-preserved")
	const criterion = "A new user can start work from the project screen."
	if _, err := client.evaluate(ctx, `document.querySelector('#durableTaskForm textarea[name="criteria"]').focus(); 'focused'`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#durableTaskForm textarea[name="criteria"]').value = ''; 'cleared'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.insertText", map[string]any{"text": criterion}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#durableTaskForm details summary').click(); document.querySelector('#durableTaskForm textarea[name="scope"]').focus(); 'goal-contract-open'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.insertText", map[string]any{"text": "Project start navigation"}, nil); err != nil {
		t.Fatal(err)
	}
	if value, err := client.evaluate(ctx, `document.querySelector('#durableTaskForm input[name="require_user_acceptance"]').checked ? 'acceptance-required' : ''`); err != nil || value != "acceptance-required" {
		t.Fatalf("new work should ask the user to accept the result: %q %v", value, err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#durableTaskForm button.primary').click(); 'create-task'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`document.querySelector('#taskPlannerProvider')?.value === %q && !document.querySelector('#taskAutoPlan')?.disabled ? 'plan-ready' : ''`, provider.ID), "plan-ready")
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('.task-gate-row').length >= 4 && document.querySelector('.task-gate-card')?.textContent.includes('ตรวจความพร้อมปิดงาน') && !document.querySelector('#taskComplete') ? 'completion-gates-visible' : ''`, "completion-gates-visible")
	if _, err := client.evaluate(ctx, `document.querySelector('#taskBackToOverview').click(); 'open-overview'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('.plans-overview-card').length === 1 && document.querySelector('.plans-overview-card button[data-plans-task]') ? 'overview-ready' : ''`, "overview-ready")
	if _, err := client.evaluate(ctx, `document.querySelector('.plans-overview-card button[data-plans-task]').click(); 'open-task'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskCurrentAction') && document.querySelector('.task-gate-card') ? 'task-restored' : ''`, "task-restored")
	if path := os.Getenv("HERMETRIX_UI_SCREENSHOT"); path != "" {
		if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1740, "height": 1000, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
			t.Fatal(err)
		}
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
		if err := os.WriteFile(path, pixels, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": true}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => {const c=document.querySelector('.task-gate-card');const r=c?.getBoundingClientRect();return r&&r.width>0&&r.left>=-1&&r.right<=innerWidth+1?'mobile-gates-visible':''})()`, "mobile-gates-visible")
	if path := os.Getenv("HERMETRIX_TASK_MOBILE_SCREENSHOT"); path != "" {
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
		if err := os.WriteFile(path, pixels, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.evaluate(ctx, `document.querySelector('[data-task-jump]').click(); 'jump-to-plan'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => {const r=document.querySelector('#taskAutoPlan')?.getBoundingClientRect();return r&&r.top>=-1&&r.bottom<=innerHeight+1?'plan-action-reachable':JSON.stringify({r:r&&{top:r.top,bottom:r.bottom},height:innerHeight,scroll:document.scrollingElement?.scrollTop})})()`, "plan-action-reachable")
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1440, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	taskResponse, err := http.Get(server.URL + "/api/tasks?limit=100")
	if err != nil {
		t.Fatal(err)
	}
	defer taskResponse.Body.Close()
	var tasks []struct {
		ProjectID   string `json:"project_id"`
		Objective   string `json:"objective"`
		Requirement struct {
			Contract struct {
				Scope                 string `json:"scope"`
				RequirePlanReview     bool   `json:"require_plan_review"`
				RequireUserAcceptance bool   `json:"require_user_acceptance"`
			} `json:"contract"`
			Criteria []struct {
				Description string `json:"description"`
			} `json:"criteria"`
		} `json:"requirement"`
	}
	if err := json.NewDecoder(taskResponse.Body).Decode(&tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ProjectID != project.ID || tasks[0].Objective != plannedGoal ||
		len(tasks[0].Requirement.Criteria) != 1 || tasks[0].Requirement.Criteria[0].Description != criterion ||
		tasks[0].Requirement.Contract.Scope != "Project start navigation" || !tasks[0].Requirement.Contract.RequirePlanReview ||
		!tasks[0].Requirement.Contract.RequireUserAcceptance {
		t.Fatalf("the plan form did not persist the goal and observable success criterion: %+v", tasks)
	}
	if _, err := client.evaluate(ctx, `(() => {const details=document.querySelector('.task-revise');details.open=true;const outcome=details.querySelector('[name="outcome"]');outcome.value='Improve the navigation and search.';outcome.dispatchEvent(new Event('input',{bubbles:true}));const criteria=details.querySelector('[name="criteria"]');criteria.value='A user finds the project from search.';criteria.dispatchEvent(new Event('input',{bubbles:true}));details.querySelector('button[type="submit"]').click();return 'revise-requested'})()`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#actionDialog')?.open ? 'revision-confirmation-open' : ''`, "revision-confirmation-open")
	if _, err := client.evaluate(ctx, `document.querySelector('#actionCancel').click(); 'revision-cancelled'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.durableTasks.find(item=>item.id===state.selectedDurableTask)?.objective==='Improve the project navigation.' ? 'revision-not-saved' : ''`, "revision-not-saved")
	if _, err := client.evaluate(ctx, `document.querySelector('.task-revise button[type="submit"]').click(); document.querySelector('#actionConfirm').click(); 'revision-confirmed'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => {const t=state.durableTasks.find(item=>item.id===state.selectedDurableTask);return t?.objective==='Improve the navigation and search.'&&t.active_requirement_revision===2&&t.active_plan_revision===0&&t.requirement.criteria[0]?.id==='AC-2'&&t.requirement.contract.require_plan_review&&document.querySelector('#taskAutoPlan')?'revised-goal-ready':''})()`, "revised-goal-ready")
	if _, err := client.evaluate(ctx, `document.querySelector('#railNewSession').click(); 'new-from-workspace'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#startPrompt') && document.querySelector('#zones').classList.contains('side-hidden') ? 'focused-new-task' : ''`, "focused-new-task")
	requestMu.Lock()
	createdSessions := requests["POST /api/sessions"]
	requestMu.Unlock()
	if createdSessions != 1 {
		t.Fatalf("opening New task created an empty extra session: %d session creates", createdSessions)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#plansViewButton').click(); 'open-plans'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#plansViewButton').classList.contains('on') && document.querySelector('.planning-toolbar') && !document.querySelector('#paneAdd')?.getClientRects().length ? 'focused-plans' : ''`, "focused-plans")
	if _, err := client.evaluate(ctx, `document.querySelector('#viewSwitch [data-view="code"]').click(); 'open-workspace'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!document.querySelector('#plansViewButton').classList.contains('on') && document.querySelector('#paneAdd').getClientRects().length && document.querySelector('#workspacePaneTabs').getClientRects().length ? 'workspace-restored' : ''`, "workspace-restored")
	if _, err := client.evaluate(ctx, `document.querySelector('#railNewSession').click(); 'new-from-workspace'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#startPrompt') && document.querySelector('#zones').classList.contains('side-hidden') ? 'focused-new-task' : ''`, "focused-new-task")
}

func TestTaskCompletionConfirmationInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome is required for the UI E2E job")
		}
		t.Skip("Chrome is not installed")
	}
	handler := testHandler(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Preserve a realistic evidence delay across overview -> remembered detail.
		// The detail must repaint when the overview's in-flight request settles.
		if strings.HasSuffix(r.URL.Path, "/execution") {
			time.Sleep(200 * time.Millisecond)
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	projectBody := requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Completion flow project", "root_path": t.TempDir()}, http.StatusCreated)
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(projectBody, &project); err != nil {
		t.Fatal(err)
	}
	taskBody := requestJSON(t, server.URL+"/api/tasks", http.MethodPost, map[string]any{
		"project_id": project.ID, "title": "Verify completion flow", "objective": "Finish only with current evidence",
		"original_request": "Finish only with current evidence", "actor": "owner",
		"criteria": []map[string]string{{"id": "AC-1", "description": "The result is verified"}},
		"contract": map[string]any{"scope": "Current project", "affected_journeys": []string{"J1"}, "require_user_acceptance": true},
	}, http.StatusCreated)
	var task taskengine.Task
	if err := json.Unmarshal(taskBody, &task); err != nil {
		t.Fatal(err)
	}
	evidenceBody := requestJSON(t, server.URL+"/api/artifacts", http.MethodPost, map[string]any{
		"project_id": project.ID, "name": "completion-evidence.txt", "kind": "test_evidence",
		"mime_type": "text/plain", "content": "The browser fixture passed its check.",
	}, http.StatusCreated)
	var evidence struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(evidenceBody, &evidence); err != nil {
		t.Fatal(err)
	}
	updateTask := func(body []byte) {
		if err := json.Unmarshal(body, &task); err != nil {
			t.Fatal(err)
		}
	}
	updateTask(requestJSON(t, server.URL+"/api/tasks/"+task.ID+"/plans", http.MethodPost, map[string]any{
		"expected_task_revision": task.Revision, "requirement_revision": task.ActiveRequirementRevision,
		"reason": "Verify exact outcome", "actor": "planner",
		"steps": []map[string]any{{"key": "verify", "title": "Verify result", "instructions": "Run the check",
			"requirement_ids": []string{"AC-1"}, "checks": []string{"smoke"}}},
	}, http.StatusCreated))
	step := task.Plan.Steps[0]
	updateTask(requestJSON(t, server.URL+"/api/tasks/"+task.ID+"/steps/verify/transitions", http.MethodPost,
		map[string]any{"action": "start", "expected_task_revision": task.Revision, "expected_step_revision": step.Revision}, http.StatusOK))
	step = task.Plan.Steps[0]
	requestJSON(t, server.URL+"/api/tasks/"+task.ID+"/validations", http.MethodPost, map[string]any{
		"step_id": step.ID, "check_id": "smoke", "subject_revision": taskengine.StepSubjectRevision(step),
		"status": "pass", "evidence_refs": []string{"artifact:" + evidence.ID},
	}, http.StatusCreated)
	updateTask(requestJSON(t, server.URL+"/api/tasks/"+task.ID+"/steps/verify/transitions", http.MethodPost,
		map[string]any{"action": "complete", "expected_task_revision": task.Revision, "expected_step_revision": step.Revision}, http.StatusOK))
	requestJSON(t, server.URL+"/api/tasks/"+task.ID+"/validations", http.MethodPost, map[string]any{
		"requirement_id": "AC-1", "check_id": "acceptance", "subject_revision": taskengine.RequirementSubjectRevision(task),
		"status": "pass", "evidence_refs": []string{"receipt:accepted"},
	}, http.StatusCreated)
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
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, fmt.Sprintf(`document.querySelector('[data-open-project="%s"]').click(); 'opened'`, project.ID)); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`state.currentProject?.id === %q && document.querySelector('#plansViewButton') ? 'project-open' : ''`, project.ID), "project-open")
	if _, err := client.evaluate(ctx, `document.querySelector('#plansViewButton').click(); 'plans'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-plans-task]') ? 'overview-task' : ''`, "overview-task")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-plans-task]').click(); 'task-open'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.task-cockpit') ? 'task-cockpit' : ''`, "task-cockpit")
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskAcceptResult') && !document.querySelector('#taskComplete') ? 'awaiting-user-acceptance' : ''`, "awaiting-user-acceptance")
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`document.querySelector('.task-gate-list a[href="/api/artifacts/%s/content"]') ? 'evidence-linked' : ''`, evidence.ID), "evidence-linked")
	// An external validation changes the evidence, not the task revision. The
	// global reload must revoke the stale acceptance action and load the new gate.
	requestJSON(t, server.URL+"/api/tasks/"+task.ID+"/validations", http.MethodPost, map[string]any{
		"requirement_id": "AC-1", "check_id": "external-regression", "subject_revision": taskengine.RequirementSubjectRevision(task),
		"status": "fail", "evidence_refs": []string{"artifact:" + evidence.ID}, "actual": "new external failure",
	}, http.StatusCreated)
	if _, err := client.evaluate(ctx, `document.querySelector('#refreshButton').click(); 'refresh-new-validation'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => {const e=state.taskExecutions[state.selectedDurableTask];return e?.completion?.gates?.some(g=>g.id==='criterion:AC-1'&&g.status==='fail')&&!document.querySelector('#taskAcceptResult')?'same-revision-failure-loaded':''})()`, "same-revision-failure-loaded")
	requestJSON(t, server.URL+"/api/tasks/"+task.ID+"/validations", http.MethodPost, map[string]any{
		"requirement_id": "AC-1", "check_id": "external-regression", "subject_revision": taskengine.RequirementSubjectRevision(task),
		"status": "pass", "evidence_refs": []string{"artifact:" + evidence.ID}, "actual": "external regression fixed",
	}, http.StatusCreated)
	if _, err := client.evaluate(ctx, `document.querySelector('#refreshButton').click(); 'refresh-fixed-validation'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskAcceptResult') && !document.querySelector('#taskComplete') ? 'new-validation-restored-acceptance' : ''`, "new-validation-restored-acceptance")
	if _, err := client.evaluate(ctx, `document.documentElement.setAttribute('data-e2e-reload', 'old'); 'marked'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Page.reload", map[string]any{"ignoreCache": true}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!document.documentElement.hasAttribute('data-e2e-reload') && document.querySelector('#taskAcceptResult') && !document.querySelector('#taskComplete') ? 'restored-current-evidence' : ''`, "restored-current-evidence")
	if _, err := client.evaluate(ctx, `document.querySelector('#taskBackToOverview').click(); document.documentElement.setAttribute('data-e2e-reload', 'old'); 'back-to-overview'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Page.reload", map[string]any{"ignoreCache": true}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!document.documentElement.hasAttribute('data-e2e-reload') && document.querySelector('#plansOverviewTitle') && document.querySelector('[data-plans-task]') ? 'restored-overview' : ''`, "restored-overview")
	if _, err := client.evaluate(ctx, `window.__motionErrors = []; window.addEventListener('error', event => window.__motionErrors.push(event.message)); document.querySelector('#plansNewTask').click(); 'new-task'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#durableTaskForm textarea[name="objective"]') ? 'new-task-form' : ''`, "new-task-form")
	motionErrors, err := client.evaluate(ctx, `window.__motionErrors.join('\n')`)
	if err != nil || motionErrors != "" {
		t.Fatalf("new Task form failed: %s (%v)", motionErrors, err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#plansViewButton').click(); 'overview'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#plansOverviewTitle') ? 'overview-again' : ''`, "overview-again")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-plans-task]').click(); 'reopen-task'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskAcceptResult') ? 'reopened-current-evidence' : ''`, "reopened-current-evidence")
	if _, err := client.evaluate(ctx, `document.querySelector('#taskAcceptResult').click(); 'request-acceptance'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#actionDialog')?.open ? 'acceptance-confirmation-open' : ''`, "acceptance-confirmation-open")
	if _, err := client.evaluate(ctx, `document.querySelector('#actionCancel').click(); 'cancel-acceptance'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskAcceptResult') && !document.querySelector('#actionDialog').open ? 'acceptance-still-pending' : ''`, "acceptance-still-pending")
	if _, err := client.evaluate(ctx, `document.querySelector('#taskAcceptResult').click(); document.querySelector('#actionConfirm').click(); 'confirm-acceptance'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskComplete') && document.querySelector('.task-gate-card')?.textContent.includes('พร้อมปิดงาน') ? 'ready-to-complete' : ''`, "ready-to-complete")
	waitBrowserValue(t, ctx, client, `document.querySelector('.task-activity')?.textContent.includes('รับรองผล') && document.querySelector('.task-activity')?.textContent.includes('ฉบับปัจจุบัน') ? 'activity-current' : ''`, "activity-current")
	if _, err := client.evaluate(ctx, `document.querySelector('#taskComplete').click(); 'request-completion'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#actionDialog')?.open ? 'confirmation-open' : ''`, "confirmation-open")
	if _, err := client.evaluate(ctx, `document.querySelector('#actionCancel').click(); 'cancel-completion'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskComplete') && !document.querySelector('#actionDialog').open ? 'still-open' : ''`, "still-open")
	if _, err := client.evaluate(ctx, `document.querySelector('#taskComplete').click(); document.querySelector('#actionConfirm').click(); 'confirm-completion'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!document.querySelector('#taskComplete') && document.querySelector('.task-gate-card')?.textContent.includes('เสร็จแล้ว') ? 'completed' : ''`, "completed")
	response, err := http.Get(server.URL + "/api/tasks/" + task.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(&task); err != nil {
		t.Fatal(err)
	}
	if task.State != taskengine.StateCompleted {
		t.Fatalf("task state after browser confirmation = %s", task.State)
	}
}

func TestTaskClarificationCreatesNewGoalInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	server := httptest.NewServer(testHandler(t))
	defer server.Close()
	projectBody := requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Clarification project", "root_path": t.TempDir()}, http.StatusCreated)
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(projectBody, &project); err != nil {
		t.Fatal(err)
	}
	taskBody := requestJSON(t, server.URL+"/api/tasks", http.MethodPost, map[string]any{
		"project_id": project.ID, "title": "Choose the Page", "objective": "Prepare a reviewed post",
		"original_request": "Prepare a reviewed post", "actor": "owner",
		"criteria": []map[string]string{{"id": "AC-1", "description": "The draft is reviewed"}},
		"unknowns": []string{"Which Page should receive it?"},
	}, http.StatusCreated)
	var task taskengine.Task
	if err := json.Unmarshal(taskBody, &task); err != nil {
		t.Fatal(err)
	}
	if len(task.Requirement.Unknowns) != 1 {
		t.Fatalf("created Task lost its unknown: %+v", task.Requirement)
	}
	listed := requestJSON(t, server.URL+"/api/tasks?limit=100", http.MethodGet, nil, http.StatusOK)
	if !strings.Contains(string(listed), task.ID) {
		t.Fatalf("created Task missing from list: %s", listed)
	}
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
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, fmt.Sprintf(`document.querySelector('[data-open-project="%s"]').click(); 'opened'`, project.ID)); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#startTaskForm #startPrompt') && document.querySelector('#plansViewButton') && state.currentProject?.id ? 'project-open' : ''`, "project-open")
	if _, err := client.evaluate(ctx, `document.querySelector('#plansViewButton').click(); 'plans'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-plans-task]') ? 'overview-task' : ''`, "overview-task")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-plans-task]').click(); 'task-open'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.task-cockpit') ? 'cockpit-ready' : ''`, "cockpit-ready")
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskClarificationForm select')?.value === 'Which Page should receive it?' ? 'question-visible' : ''`, "question-visible")
	if _, err := client.evaluate(ctx, `(() => { const form=document.querySelector('#taskClarificationForm'); form.querySelector('[name="answer"]').value='Company Page'; form.dispatchEvent(new Event('input',{bubbles:true})); form.requestSubmit(); return 'submitted'; })()`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#actionDialog')?.open ? 'confirm-open' : ''`, "confirm-open")
	if _, err := client.evaluate(ctx, `document.querySelector('#actionCancel').click(); 'cancelled'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskClarificationForm [name="answer"]')?.value === 'Company Page' && !document.querySelector('#actionDialog').open ? 'draft-preserved' : ''`, "draft-preserved")
	if _, err := client.evaluate(ctx, `renderPanes(); 'redrawn'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskClarificationForm [name="answer"]')?.value === 'Company Page' ? 'draft-survived-redraw' : ''`, "draft-survived-redraw")
	if _, err := client.evaluate(ctx, `window.__e2eOldDocument=true; 'marked'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Page.reload", map[string]any{"ignoreCache": true}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!window.__e2eOldDocument && document.readyState === 'complete' ? 'new-document' : ''`, "new-document")
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`document.querySelector('[data-open-project="%s"]') || state.currentProject?.id === %q ? 'project-ready' : ''`, project.ID, project.ID), "project-ready")
	if _, err := client.evaluate(ctx, fmt.Sprintf(`if(state.currentProject?.id !== %q) document.querySelector('[data-open-project="%s"]').click(); 'opened'`, project.ID, project.ID)); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#plansViewButton') && state.currentProject?.id ? 'project-open' : ''`, "project-open")
	if _, err := client.evaluate(ctx, `document.querySelector('#plansViewButton').click(); 'plans'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-plans-task]') ? 'overview-task' : ''`, "overview-task")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-plans-task]').click(); 'task-open'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskClarificationForm [name="answer"]')?.value === 'Company Page' ? 'draft-survived-reload' : ''`, "draft-survived-reload")
	if _, err := client.evaluate(ctx, `document.querySelector('#taskClarificationForm').requestSubmit(); document.querySelector('#actionConfirm').click(); 'confirmed'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!document.querySelector('#taskClarificationForm') && document.querySelector('.task-clarification-history')?.textContent.includes('Company Page') ? 'answer-saved' : ''`, "answer-saved")
	response, err := http.Get(server.URL + "/api/tasks/" + task.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(&task); err != nil {
		t.Fatal(err)
	}
	if task.ActiveRequirementRevision != 2 || len(task.Requirement.Unknowns) != 0 ||
		len(task.Requirement.Contract.Clarifications) != 1 || task.Requirement.Contract.Clarifications[0].Answer != "Company Page" {
		t.Fatalf("clarification browser result = %+v", task.Requirement)
	}
}

func TestPlanReviewInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome is required for the UI E2E job")
		}
		t.Skip("Chrome is not installed")
	}
	server := httptest.NewServer(testHandler(t))
	defer server.Close()
	projectBody := requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Plan review project", "root_path": t.TempDir()}, http.StatusCreated)
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(projectBody, &project); err != nil {
		t.Fatal(err)
	}
	taskBody := requestJSON(t, server.URL+"/api/tasks", http.MethodPost, map[string]any{
		"project_id": project.ID, "title": "Review the plan", "objective": "Check the workflow",
		"original_request": "Check the workflow", "actor": "owner",
		"criteria": []map[string]string{{"id": "AC-1", "description": "The workflow passes"}},
		"contract": map[string]any{"require_plan_review": true},
	}, http.StatusCreated)
	var task taskengine.Task
	if err := json.Unmarshal(taskBody, &task); err != nil {
		t.Fatal(err)
	}
	planBody := requestJSON(t, server.URL+"/api/tasks/"+task.ID+"/plans", http.MethodPost, map[string]any{
		"expected_task_revision": task.Revision, "requirement_revision": task.ActiveRequirementRevision,
		"reason": "Verify the workflow", "actor": "planner",
		"steps": []map[string]any{{"key": "verify", "title": "Verify", "instructions": "Run a check",
			"requirement_ids": []string{"AC-1"}, "checks": []string{"smoke"}}},
	}, http.StatusCreated)
	if err := json.Unmarshal(planBody, &task); err != nil {
		t.Fatal(err)
	}
	if task.Plan.ReviewStatus != "pending" || task.State != taskengine.StateWaitingForInput {
		t.Fatalf("plan is not awaiting review: %+v", task)
	}
	planBody = requestJSON(t, server.URL+"/api/tasks/"+task.ID+"/plans", http.MethodPost, map[string]any{
		"expected_task_revision": task.Revision, "requirement_revision": task.ActiveRequirementRevision,
		"reason": "Add a second check", "actor": "planner",
		"steps": []map[string]any{{"key": "verify", "title": "Verify", "instructions": "Run two checks",
			"requirement_ids": []string{"AC-1"}, "checks": []string{"smoke", "browser"}}},
	}, http.StatusCreated)
	if err := json.Unmarshal(planBody, &task); err != nil {
		t.Fatal(err)
	}
	if task.Plan.Revision != 2 || task.Plan.ReviewStatus != "pending" {
		t.Fatalf("revised plan is not awaiting review: %+v", task.Plan)
	}
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
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, fmt.Sprintf(`document.querySelector('[data-open-project="%s"]').click(); 'project'`, project.ID)); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`document.querySelector('#startTaskForm #startPrompt') && document.querySelector('#plansViewButton') && state.currentProject?.id===%q ? 'project-open' : ''`, project.ID), "project-open")
	if _, err := client.evaluate(ctx, `document.querySelector('#plansViewButton').click(); 'plans'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-plans-task]') ? 'overview-task' : ''`, "overview-task")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-plans-task]').click(); 'task-open'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskPlanReview') && document.querySelector('#taskApprovePlan') ? 'review-visible' : ''`, "review-visible")
	waitBrowserValue(t, ctx, client, `document.querySelector('.task-plan-diff')?.textContent.includes('1 จุดเปลี่ยน') ? 'diff-visible' : ''`, "diff-visible")
	if _, err := client.evaluate(ctx, `document.querySelector('#taskApprovePlan').click(); 'approve-request'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#actionDialog')?.open ? 'confirm-open' : ''`, "confirm-open")
	if _, err := client.evaluate(ctx, `document.querySelector('#actionCancel').click(); 'cancel-review'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskPlanReview') && !document.querySelector('#actionDialog').open ? 'review-still-pending' : ''`, "review-still-pending")
	if _, err := client.evaluate(ctx, `document.querySelector('#taskApprovePlan').click(); document.querySelector('#actionConfirm').click(); 'approve-plan'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.durableTasks.find(item=>item.id===state.selectedDurableTask)?.plan.review_status==='approved' && !document.querySelector('#taskPlanReview') ? 'plan-approved' : ''`, "plan-approved")
}

func TestPrimaryRequestOpensEditablePlanBeforeExecution(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome is required for the UI E2E job")
		}
		t.Skip("Chrome is not installed")
	}
	server := httptest.NewServer(testHandler(t))
	defer server.Close()
	projectBody := requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Review before work", "root_path": t.TempDir()}, http.StatusCreated)
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(projectBody, &project); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
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
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, fmt.Sprintf(`document.querySelector('[data-open-project="%s"]').click(); 'project'`, project.ID)); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`state.currentProject?.id === %q && document.querySelector('#startPlanButton') ? 'request-ready' : ''`, project.ID), "request-ready")
	if _, err := client.evaluate(ctx, `(() => { const input=document.querySelector('#startPrompt'); input.value='Improve the start screen'; input.dispatchEvent(new Event('input',{bubbles:true})); document.querySelector('#startPlanButton').click(); return 'draft-requested'; })()`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#actionDialog')?.open ? 'handoff-review' : ''`, "handoff-review")
	if _, err := client.evaluate(ctx, `document.querySelector('#actionConfirm').click(); 'handoff-confirmed'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#durableTaskForm textarea[name="objective"]')?.value === 'Improve the start screen' && document.querySelector('#durableTaskForm textarea[name="criteria"]')?.value === 'Improve the start screen' ? 'editable-brief' : ''`, "editable-brief")
	var tasks []taskengine.Task
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/tasks", http.MethodGet, nil, http.StatusOK), &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("opening the brief executed or created work: %+v", tasks)
	}
	var sessions []map[string]any
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/sessions", http.MethodGet, nil, http.StatusOK), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("primary request started an agent turn before review: %+v", sessions)
	}
	if _, err := client.evaluate(ctx, `(() => {const form=document.querySelector('#durableTaskForm'); const preview=form.querySelector('[name="experience_preview"]'); preview.value='A review screen explains the goal and offers edit and confirm actions'; preview.dispatchEvent(new Event('input',{bubbles:true})); const criterion=form.querySelector('[name="criteria"]'); criterion.value='A user can review and edit the plan before work starts'; criterion.dispatchEvent(new Event('input',{bubbles:true})); form.querySelector('button.primary').click(); return 'create-draft';})()`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.durableTasks.length===1 && state.durableTasks[0].requirement.contract.experience_preview.includes('review screen') && !state.durableTasks[0].active_plan_revision ? 'draft-saved' : ''`, "draft-saved")
	if value, err := client.evaluate(ctx, `document.querySelector('#taskProposalForm') ? 'premature-execution' : 'no-execution'`); err != nil || value != "no-execution" {
		t.Fatalf("work was offered before review: %q %v", value, err)
	}
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/tasks", http.MethodGet, nil, http.StatusOK), &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Requirement.Contract.ExperiencePreview != "A review screen explains the goal and offers edit and confirm actions" ||
		!tasks[0].Requirement.Contract.RequirePlanReview {
		t.Fatalf("the editable brief was not saved with a required plan review: %+v", tasks)
	}
	planBody := requestJSON(t, server.URL+"/api/tasks/"+tasks[0].ID+"/plans", http.MethodPost, map[string]any{
		"expected_task_revision": tasks[0].Revision, "requirement_revision": tasks[0].ActiveRequirementRevision,
		"reason": "The start screen will show a review summary and a route back to edit the request", "actor": "planner",
		"steps": []map[string]any{{"key": "start-screen", "title": "Build the review screen", "instructions": "Show the goal and edit action",
			"requirement_ids": []string{"AC-1"}, "checks": []string{"browser"}}},
	}, http.StatusCreated)
	var planned taskengine.Task
	if err := json.Unmarshal(planBody, &planned); err != nil {
		t.Fatal(err)
	}
	if planned.Plan.ReviewStatus != "pending" {
		t.Fatalf("plan did not wait for review: %+v", planned.Plan)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#plansViewButton').click(); 'reload-plans'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-plans-task]') ? 'overview-task' : ''`, "overview-task")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-plans-task]').click(); 'task-open'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.task-plan-brief')?.textContent.includes('A review screen') && document.querySelector('.task-plan-reference')?.textContent.includes('route back to edit') && document.querySelector('#taskApprovePlan') && !document.querySelector('#taskProposalForm') ? 'plan-review-ready' : ''`, "plan-review-ready")
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1440, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.waitForPaint(ctx); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!document.querySelector('#toast')?.classList.contains('show') && Number(getComputedStyle(document.querySelector('#toast')).opacity) < 0.01 ? 'toast-dismissed' : ''`, "toast-dismissed")
	if _, err := client.evaluate(ctx, `document.querySelector('.task-plan-brief').scrollIntoView({block:'start'}); 'brief-visible'`); err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("HERMETRIX_PLAN_BRIEF_DESKTOP_SCREENSHOT"); path != "" {
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
		if err := os.WriteFile(path, pixels, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": true}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.waitForPaint(ctx); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.documentElement.scrollWidth <= innerWidth + 1 && document.querySelector('.task-cockpit')?.getBoundingClientRect().width <= innerWidth ? 'brief-mobile' : ''`, "brief-mobile")
	if _, err := client.evaluate(ctx, `document.querySelector('.task-plan-brief').scrollIntoView({block:'start'}); 'brief-mobile-visible'`); err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("HERMETRIX_PLAN_BRIEF_MOBILE_SCREENSHOT"); path != "" {
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
		if err := os.WriteFile(path, pixels, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#taskApprovePlan').scrollIntoView({block:'center'}); 'review-mobile-visible'`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `renderPanes(); 'pane-remounted'`); err != nil {
		t.Fatal(err)
	}
	if err := client.waitForPaint(ctx); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => {const button=document.querySelector('#taskApprovePlan'); const rect=button?.getBoundingClientRect(); const hit=rect&&document.elementFromPoint(rect.left+rect.width/2,rect.top+rect.height/2); return hit && (hit===button || button.contains(hit)) && rect.left>=0 && rect.right<=innerWidth ? 'review-button-reachable' : JSON.stringify({button:rect,hit:hit?.outerHTML?.slice(0,240),viewport:[innerWidth,innerHeight],documentScroll:[scrollX,scrollY],position:taskViewportPositions.get(state.selectedDurableTask),body:(()=>{const b=button?.closest('.pane-body');return{rect:b?.getBoundingClientRect(),scrollTop:b?.scrollTop,scrollHeight:b?.scrollHeight,clientHeight:b?.clientHeight}})()});})()`, "review-button-reachable")
	if path := os.Getenv("HERMETRIX_PLAN_REVIEW_MOBILE_SCREENSHOT"); path != "" {
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
		if err = os.WriteFile(path, pixels, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// A DOM hit test alone cannot prove the confirmation is actionable. Use
	// native input and cancel the review dialog without approving any work.
	pointJSON, err := client.evaluate(ctx, `(()=>{const r=document.querySelector('#taskApprovePlan').getBoundingClientRect();return JSON.stringify({x:r.left+r.width/2,y:r.top+r.height/2})})()`)
	if err != nil {
		t.Fatal(err)
	}
	var point struct{ X, Y float64 }
	if err = json.Unmarshal([]byte(pointJSON), &point); err != nil {
		t.Fatal(err)
	}
	for _, eventType := range []string{"mousePressed", "mouseReleased"} {
		if err = client.call(ctx, "Input.dispatchMouseEvent", map[string]any{"type": eventType, "x": point.X, "y": point.Y, "button": "left", "clickCount": 1}, nil); err != nil {
			t.Fatal(err)
		}
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#actionDialog')?.open && document.querySelector('#actionDialog').textContent.includes('อนุมัติแผนนี้') ? 'native-review-open' : ''`, "native-review-open")
	if _, err = client.evaluate(ctx, `document.querySelector('#actionCancel').click(); 'review-cancelled'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!document.querySelector('#actionDialog')?.open ? 'review-closed' : ''`, "review-closed")
	if err = json.Unmarshal(requestJSON(t, server.URL+"/api/tasks", http.MethodGet, nil, http.StatusOK), &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Plan.ReviewStatus != "pending" {
		t.Fatalf("opening/canceling review approved work: %+v", tasks)
	}
}

// TestIDEWorkspaceRunsInARealBrowser covers the two work surfaces whose
// behaviour cannot be proved by source inspection: CodeMirror must mount and
// save through the bounded file API, and xterm must send real key input to the
// PTY while preserving terminal escape sequences on output.
func TestIDEWorkspaceRunsInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome is required for the UI E2E job")
		}
		t.Skip("Chrome is not installed")
	}
	root := t.TempDir()
	filePath := filepath.Join(root, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitAvailable := false
	if _, err := exec.LookPath("git"); err == nil {
		gitAvailable = exec.Command("git", "-C", root, "init").Run() == nil
	}
	server := testHTTPServer(t)
	projectBody := requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "IDE project", "root_path": root}, http.StatusCreated)
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(projectBody, &project); err != nil {
		t.Fatal(err)
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
	if err := client.call(ctx, "Page.enable", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1440, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}

	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, fmt.Sprintf(`openProject(%q).then(() => { switchView('code'); window.HermetrixWorkspace.open(); }).catch(error => { window.__e2eOpenProjectError=String(error); }); 'opened'`, project.ID)); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-workbench-file="main.go"]') ? 'files' : ''`, "files")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-workbench-file="main.go"]').click(); 'clicked'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.cm-editor') ? 'editor' : ''`, "editor")
	waitForStyledEditor(t, ctx, client)
	if _, err := client.evaluate(ctx, `document.querySelector('[data-ide-side="chat"]').click(); 'assistant'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.ide-ai-welcome') ? 'assistant-ready' : ''`, "assistant-ready")
	if directory := os.Getenv("HERMETRIX_E2E_SCREENSHOT_DIR"); directory != "" {
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
		path := filepath.Join(directory, "ide-assistant.png")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Log("IDE assistant screenshot:", path)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('[data-assistant-suggestion]').click(); 'suggestion'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.ide-assistant .pane-chat-input')?.value.includes('ตรวจคุณภาพโค้ด') ? 'draft' : ''`, "draft")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-ide-side="environment"]').click(); 'tooling'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.ide-tool-list') ? 'tools' : ''`, "tools")
	waitBrowserValue(t, ctx, client, `!document.querySelector('[data-editor-wrap]')?.checked && !document.querySelector('.cm-content')?.classList.contains('cm-lineWrapping') ? 'unwrapped' : ''`, "unwrapped")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-editor-wrap]').click(); 'wrap-off'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-editor-wrap]')?.checked && document.querySelector('.cm-content')?.classList.contains('cm-lineWrapping') ? 'wrapped' : ''`, "wrapped")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-editor-wrap]').click(); 'wrap-on'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `localStorage.getItem('hermetrix.ide.wordWrap.v2') === 'false' && !document.querySelector('.cm-content')?.classList.contains('cm-lineWrapping') ? 'scroll-persisted' : ''`, "scroll-persisted")
	if directory := os.Getenv("HERMETRIX_E2E_SCREENSHOT_DIR"); directory != "" {
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
		path := filepath.Join(directory, "ide-settings.png")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Log("IDE settings screenshot:", path)
	}
	if _, err := client.evaluate(ctx, `switchTab('tools'); 'direct-tools'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('.direct-tool-card').length === state.direct_tools.length && state.direct_tools.length > 0 ? 'registry' : ''`, "registry")
	waitBrowserValue(t, ctx, client, `getComputedStyle(document.querySelector('#configOverlay')).opacity === '1' ? 'settled' : ''`, "settled")
	if directory := os.Getenv("HERMETRIX_E2E_SCREENSHOT_DIR"); directory != "" {
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
		path := filepath.Join(directory, "direct-tools-full.png")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Log("Direct tools directory screenshot:", path)
	}
	if _, err := client.evaluate(ctx, `const input=document.querySelector('#directToolQuery'); input.value='workspace.read_file'; input.dispatchEvent(new Event('input',{bubbles:true})); 'filter'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `[...document.querySelectorAll('.direct-tool-card')].filter(node=>!node.hidden).length === 1 ? 'filtered' : ''`, "filtered")
	if _, err := client.evaluate(ctx, `document.querySelector('.direct-tool-card:not([hidden]) summary').click(); 'details'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.direct-tool-card[open] pre')?.textContent.includes('path') ? 'schema' : ''`, "schema")
	if directory := os.Getenv("HERMETRIX_E2E_SCREENSHOT_DIR"); directory != "" {
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
		path := filepath.Join(directory, "direct-tools.png")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Log("Direct tools screenshot:", path)
	}
	if _, err := client.evaluate(ctx, `switchTab('library'); 'skills'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `readySurfaces.has('library') && document.querySelector('#view-library.active') ? 'library' : ''`, "library")
	if _, err := client.evaluate(ctx, `state.skills=['aetox-architect','aetox-brainstorm','aetox-code-review','aetox-debug','aetox-forge','aetox-grill','aetox-idea-to-architecture','aetox-orient'].map((name,index)=>({id:String(index),canonical_name:name,summary:'ช่วยวางแผนและตรวจงานในโปรเจกต์',state:'active',enabled:true,pinned:index<2,origin:index<3?'agent':'user',injected_count:index+1,success_count:0})); renderLibrary(); 'fixture'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('.skill-tile').length === 8 ? 'tiles' : ''`, "tiles")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-skill-collection="pinned"]').click(); 'pinned'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('.skill-tile').length === 2 ? 'pinned-filter' : ''`, "pinned-filter")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-skill-collection="all"]').click(); 'all'`); err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("HERMETRIX_E2E_SCREENSHOT_DIR"); directory != "" {
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
		path := filepath.Join(directory, "skills-fixture.png")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Log("Skills fixture screenshot:", path)
	}
	if _, err := client.evaluate(ctx, `switchTab('chat'); 'editor-return'`); err != nil {
		t.Fatal(err)
	}
	if gitAvailable {
		if _, err := client.evaluate(ctx, `document.querySelector('[data-ide-side="git"]').click(); 'git'`); err != nil {
			t.Fatal(err)
		}
		waitBrowserValue(t, ctx, client, `ideGitSnapshots.get(state.currentProject?.id)?.status?.includes('main.go') ? 'git-status' : ''`, "git-status")
		if directory := os.Getenv("HERMETRIX_E2E_SCREENSHOT_DIR"); directory != "" {
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
			path := filepath.Join(directory, "ide-git.png")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Log("IDE Git screenshot:", path)
		}
	}
	if _, err := client.evaluate(ctx, `document.querySelector('[data-ide-side="files"]').click(); 'files'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.cm-editor') && document.querySelector('[data-workbench-file="main.go"]') ? 'editor-and-files' : ''`, "editor-and-files")
	// Opening a file does not start an unrelated shell. Request the terminal
	// explicitly so this also covers the on-demand workspace path.
	if _, err := client.evaluate(ctx, `openContentPane('terminal'); 'opened-terminal'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.xterm-helper-textarea') ? 'terminal' : ''`, "terminal")
	waitBrowserValue(t, ctx, client, `(() => { const line=[...document.querySelectorAll('.cm-line')].find(node => node.textContent.includes('package main')); if (!line) return ''; const token=line.querySelector('span') || line; const color=getComputedStyle(token).color; return color !== 'rgb(20, 20, 20)' && color !== 'rgba(0, 0, 0, 0)' ? 'visible' : ''; })()`, "visible")
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('[data-editor-action]').length === 4 && document.querySelector('[data-code-symbol]') ? 'ide-tools' : ''`, "ide-tools")

	if _, err := client.evaluate(ctx, `document.querySelector('.cm-content').focus(); document.execCommand('insertText', false, '// browser-e2e\n'); 'edited'`); err != nil {
		t.Fatal(err)
	}
	saveModifier := 2 // Control in the Chrome DevTools Protocol bitmask.
	if runtime.GOOS == "darwin" {
		saveModifier = 4 // Meta/Command.
	}
	if err := client.call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "s", "code": "KeyS", "modifiers": saveModifier}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "s", "code": "KeyS", "modifiers": saveModifier}, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, readErr := os.ReadFile(filePath)
		if readErr == nil && strings.Contains(string(data), "browser-e2e") {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}
	data, err := os.ReadFile(filePath)
	if err != nil || !strings.Contains(string(data), "browser-e2e") {
		t.Fatalf("CodeMirror Mod-S did not save through the project API: %q err=%v", data, err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-editor-action="format"]') ? 'format-ready' : ''`, "format-ready")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-editor-action="format"]').click(); 'format'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `/formatted/i.test(document.querySelector('#codeFeedback')?.textContent || '') ? 'formatted-buffer' : ''`, "formatted-buffer")
	if _, err := client.evaluate(ctx, `document.querySelector('.xterm-helper-textarea').focus(); 'focused'`); err != nil {
		t.Fatal(err)
	}
	terminalCommand := "printf '\\033[31m%s%s\\033[0m\\n' 'PTY-' 'E2E'"
	if runtime.GOOS == "windows" {
		terminalCommand = "Write-Output ('PTY-' + 'E2E')"
	}
	if err := client.call(ctx, "Input.insertText", map[string]any{"text": terminalCommand}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "Enter", "code": "Enter"}, nil); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		value, _ := client.evaluate(ctx, `document.querySelector('#terminalAccessibleOutput')?.textContent.includes('PTY-E2E') ? 'pty' : ''`)
		if value == "pty" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	diagnostic, _ := client.evaluate(ctx, `JSON.stringify({terminal:document.querySelector('#terminalAccessibleOutput')?.textContent,terminals:state.terminals,selected:state.selectedTerminal,cursor:terminalCursor,visible:terminalPaneOpen(),polling:terminalPollInFlight,pollScheduled:workbenchPollTimers.has(pollTerminal),emulator:Boolean(activeTerminalEmulator),active:document.activeElement?.className,toasts:[...document.querySelectorAll('.toast')].map(el=>el.textContent)})`)
	terminalID, _ := client.evaluate(ctx, `state.selectedTerminal || ''`)
	if response, err := http.Get(server.URL + "/api/terminals/" + terminalID + "/output?cursor=0"); err == nil {
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		t.Logf("raw fixture terminal output: %s", body)
	}
	t.Fatalf("typed command did not return through the terminal: %s", diagnostic)
}

// TestEditorKeyboardWorkflowInARealBrowser uses Chrome input events, not DOM
// editing commands, to verify the editing path a person actually uses.
func TestMobileCodeEditorKeepsLongLinesScrollable(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome is required for the UI E2E job")
		}
		t.Skip("Chrome is not installed")
	}
	root := t.TempDir()
	longLine := "// " + strings.Repeat("0123456789", 50)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"+longLine+"\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := testHTTPServer(t)
	projectBody := requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Long line project", "root_path": root}, http.StatusCreated)
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(projectBody, &project); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
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
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1440, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, fmt.Sprintf(`openProject(%q).then(() => { switchView('code'); window.HermetrixWorkspace.open(); }); 'opened'`, project.ID)); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-workbench-file="main.go"]') ? 'file' : ''`, "file")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-workbench-file="main.go"]').click(); 'clicked'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.code-editor-host .cm-scroller') ? 'editor' : ''`, "editor")
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 375, "height": 812, "deviceScaleFactor": 1, "mobile": true}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const content=document.querySelector('.cm-content'), scroller=document.querySelector('.code-editor-host .cm-scroller'); if (!content || !scroller || content.classList.contains('cm-lineWrapping') || scroller.scrollWidth <= scroller.clientWidth) return ''; scroller.scrollLeft=120; return scroller.scrollLeft > 0 ? 'horizontal-scroll' : ''; })()`, "horizontal-scroll")
	if got, err := client.evaluate(ctx, `document.querySelector('.code-editor-host .cm-line')?.textContent || ''`); err != nil || got != "package main" {
		t.Fatalf("the editor did not preserve source lines: first=%q err=%v", got, err)
	}
}

func TestEditorKeyboardWorkflowInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome is required for the UI E2E job")
		}
		t.Skip("Chrome is not installed")
	}
	root := t.TempDir()
	filePath := filepath.Join(root, "greeting.js")
	const original = "export function sumPrices(items) {\n  return items.reduce((total, item) => total + item.price, 0);\n}\n"
	if err := os.WriteFile(filePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	server := testHTTPServer(t)
	requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Keyboard coding project", "root_path": root}, http.StatusCreated)
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
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1440, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	click := func(selector string) {
		t.Helper()
		geometry, err := client.evaluate(ctx, fmt.Sprintf(`(() => {const el=document.querySelector(%q);if(!el)return '{}';const r=el.getBoundingClientRect();const x=r.x+r.width/2,y=r.y+r.height/2;const at=document.elementFromPoint(x,y);return JSON.stringify({x,y,hit:at?.outerHTML.slice(0,180),reachable:!!at&&(at===el||el.contains(at))});})()`, selector))
		if err != nil {
			t.Fatal(err)
		}
		var point struct {
			X, Y      float64
			Reachable bool
		}
		if err := json.Unmarshal([]byte(geometry), &point); err != nil {
			t.Fatal(err)
		}
		if !point.Reachable {
			t.Fatalf("keyboard workflow cannot click %s: %s", selector, geometry)
		}
		for _, eventType := range []string{"mousePressed", "mouseReleased"} {
			if err := client.call(ctx, "Input.dispatchMouseEvent", map[string]any{"type": eventType, "x": point.X, "y": point.Y, "button": "left", "clickCount": 1}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	key := func(name, code string, modifiers int) {
		t.Helper()
		for _, eventType := range []string{"keyDown", "keyUp"} {
			params := map[string]any{"type": eventType, "key": name, "code": code, "modifiers": modifiers}
			if value := map[string]int{"Enter": 13, "Tab": 9, "ArrowDown": 40, "Home": 36, "End": 35, "Space": 32}[code]; value != 0 {
				params["windowsVirtualKeyCode"] = value
			}
			if err := client.call(ctx, "Input.dispatchKeyEvent", params, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	insert := func(value string) {
		t.Helper()
		if err := client.call(ctx, "Input.insertText", map[string]any{"text": value}, nil); err != nil {
			t.Fatal(err)
		}
	}
	openFile := func() {
		t.Helper()
		waitBrowserValue(t, ctx, client, `document.querySelector('#appShell:not([hidden]) #projectChip') ? 'chat-ready' : ''`, "chat-ready")
		click("#projectChip")
		waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'project' : ''`, "project")
		click("[data-open-project]")
		waitBrowserValue(t, ctx, client, `state.currentProject?.name === 'Keyboard coding project' ? 'project-open' : ''`, "project-open")
		click(`#viewSwitch [data-view="code"]`)
		waitBrowserValue(t, ctx, client, `document.querySelector('[data-workbench-file="greeting.js"]') ? 'file-listed' : ''`, "file-listed")
		// Restored open tabs can leave the editor visible after reload. Use the
		// same Files tab a person uses to bring an offscreen pane into view.
		click(`[data-compact-pane="files"]`)
		waitBrowserValue(t, ctx, client, `(() => {const el=document.querySelector('[data-workbench-file="greeting.js"]');if(!el)return '';const r=el.getBoundingClientRect(),at=document.elementFromPoint(r.x+r.width/2,r.y+r.height/2);return at && (at===el || el.contains(at)) ? 'files-visible':'';})()`, "files-visible")
		click(`[data-workbench-file="greeting.js"]`)
		waitBrowserValue(t, ctx, client, `document.querySelector('.cm-editor') ? 'editor' : ''`, "editor")
		waitForStyledEditor(t, ctx, client)
	}
	openFile()
	click(".cm-line")
	waitBrowserValue(t, ctx, client, `document.activeElement?.classList.contains('cm-content') ? 'editor-focused' : ''`, "editor-focused")
	modifier := 2
	if runtime.GOOS == "darwin" {
		modifier = 4
	}
	const replacement = "export function greetUser(name) {\n  const message = `Hello, ${name}`;\n  return message;\n}\n\nconst result = greetUser(\"Hermetrix\");\nconsole.log(result);"
	key("a", "KeyA", modifier)
	insert(replacement)
	waitBrowserValue(t, ctx, client, `document.querySelector('.cm-content')?.textContent.includes('greetUser') && document.querySelector('#codeSaveState')?.textContent === 'Unsaved' ? 'multiline-edited' : ''`, "multiline-edited")
	key("z", "KeyZ", modifier)
	waitBrowserValue(t, ctx, client, `document.querySelector('.cm-content')?.textContent.includes('sumPrices') ? 'undo' : ''`, "undo")
	if runtime.GOOS == "darwin" {
		key("z", "KeyZ", modifier|8)
	} else {
		key("y", "KeyY", modifier)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.cm-content')?.textContent.includes('greetUser') ? 'redo' : ''`, "redo")
	key("Home", "Home", modifier)
	key("ArrowDown", "ArrowDown", 0)
	key("Tab", "Tab", 0)
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('.cm-line')[1]?.textContent.startsWith('    const message') ? 'indented' : ''`, "indented")
	key("Tab", "Tab", 8)
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('.cm-line')[1]?.textContent.startsWith('  const message') ? 'outdented' : ''`, "outdented")
	key("End", "End", modifier)
	key("Enter", "Enter", 0)
	insert("greetU")
	key(" ", "Space", 2)
	waitBrowserValue(t, ctx, client, `document.querySelector('.cm-tooltip-autocomplete')?.textContent.includes('greetUser') ? 'completion' : ''`, "completion")
	// CodeMirror deliberately ignores acceptance keys for the first 75 ms
	// after opening a popup so normal typing cannot select a surprise item.
	time.Sleep(100 * time.Millisecond)
	key("Enter", "Enter", 0)
	waitBrowserValue(t, ctx, client, `[...document.querySelectorAll('.cm-line')].at(-1)?.textContent || ''`, "greetUser")
	insert(`("friend");`)
	key("s", "KeyS", modifier)
	waitBrowserValue(t, ctx, client, `document.querySelector('#codeSaveState')?.textContent === 'Saved' ? 'saved' : ''`, "saved")
	const expected = replacement + "\ngreetUser(\"friend\");"
	content, err := os.ReadFile(filePath)
	if err != nil || string(content) != expected {
		t.Fatalf("keyboard edit/save result mismatch: got %q want %q err=%v", content, expected, err)
	}
	if _, err := client.evaluate(ctx, `window.__e2eOldDocument=true; 'marked'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Page.reload", map[string]any{"ignoreCache": true}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!window.__e2eOldDocument && document.readyState === 'complete' ? 'new-document' : ''`, "new-document")
	openFile()
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`[...document.querySelectorAll('.cm-line')].map(line=>line.textContent).join('\n') === %q ? 'reopened-saved-file' : ''`, expected), "reopened-saved-file")
	click(".cm-line")
	key("a", "KeyA", modifier)
	const unformatted = "const values={first:1,second:2};\nconsole.log(values)"
	insert(unformatted)
	click(`[data-editor-action="format"]`)
	const formatted = "const values = { first: 1, second: 2 };\nconsole.log(values);\n"
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`activeCodeEditor.getValue() === %q ? 'formatted' : ''`, formatted), "formatted")
	if data, err := os.ReadFile(filePath); err != nil || string(data) != expected {
		t.Fatalf("Format must change only the unsaved buffer: %q %v", data, err)
	}
	click(".cm-line")
	key("z", "KeyZ", modifier)
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`activeCodeEditor.getValue() === %q ? 'format-undone' : ''`, unformatted), "format-undone")
	click(".cm-breakpoint-gutter .cm-gutterElement:nth-child(3)")
	waitBrowserValue(t, ctx, client, `activeCodeEditor.getBreakpoints().join(',')`, "2")
	if _, err := client.evaluate(ctx, `activeCodeEditor.setExecutionLine(2); 'paused-line'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.cm-execution-line')?.textContent || ''`, "console.log(values)")
}

func TestResponsiveWorkspaceFlowsInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome is required for the UI E2E job")
		}
		t.Skip("Chrome is not installed")
	}
	for _, viewport := range []struct{ width, height int }{{1280, 720}, {800, 900}, {390, 844}} {
		t.Run(fmt.Sprintf("%dx%d", viewport.width, viewport.height), func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			server := testHTTPServer(t)
			requestJSON(t, server.URL+"/api/providers", http.MethodPost, map[string]any{
				"name": "Ready model", "adapter_kind": "openai-compatible", "base_url": "http://127.0.0.1:8080/v1",
				"model": "browser-fixture", "context_window": 32768, "context_evidence": "declared", "max_output_tokens": 4096}, http.StatusCreated)
			otherProviderBody := requestJSON(t, server.URL+"/api/providers", http.MethodPost, map[string]any{
				"name": "Second ready model", "adapter_kind": "openai-compatible", "base_url": "http://127.0.0.1:8080/v1",
				"model": "other-browser-fixture", "context_window": 32768, "context_evidence": "declared", "max_output_tokens": 4096}, http.StatusCreated)
			var otherProvider struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(otherProviderBody, &otherProvider); err != nil {
				t.Fatal(err)
			}
			requestJSON(t, server.URL+"/api/projects", http.MethodPost,
				map[string]any{"name": "Responsive workspace project", "root_path": root}, http.StatusCreated)
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
			if err := client.call(ctx, "Page.enable", map[string]any{}, nil); err != nil {
				t.Fatal(err)
			}
			if err := client.call(ctx, "Page.navigate", map[string]any{"url": server.URL}, nil); err != nil {
				t.Fatal(err)
			}
			if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": viewport.width, "height": viewport.height, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
				t.Fatal(err)
			}
			evaluate := func(expression string) string {
				t.Helper()
				result, err := client.evaluate(ctx, expression)
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			captureLayout := func(stage string) {
				t.Helper()
				directory := os.Getenv("HERMETRIX_E2E_SCREENSHOT_DIR")
				if directory == "" {
					return
				}
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
				path := filepath.Join(directory, fmt.Sprintf("responsive-%dx%d-%s.png", viewport.width, viewport.height, stage))
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
				t.Log("layout screenshot:", path)
			}
			checkHeader := func(stage string) {
				t.Helper()
				problems := evaluate(`(() => {const header=document.querySelector('#appHeader').getBoundingClientRect();const bad=[...document.querySelectorAll('#appHeader button')].filter(el=>el.getClientRects().length).map(el=>{const r=el.getBoundingClientRect();return {id:el.id||el.textContent.trim(),x:r.x,y:r.y,w:r.width,h:r.height}}).filter(r=>r.x < -1 || r.y < header.y-1 || r.x+r.w > innerWidth+1 || r.y+r.h > header.bottom+1);return bad.length ? JSON.stringify(bad) : '';})()`)
				if problems != "" {
					t.Errorf("%s: header controls extend outside their visible header: %s", stage, problems)
				}
			}
			click := func(selector string) bool {
				t.Helper()
				geometry := evaluate(fmt.Sprintf(`(() => {const el=document.querySelector(%q);if(!el)return '{}';const r=el.getBoundingClientRect();const x=r.x+r.width/2,y=r.y+r.height/2;const at=document.elementFromPoint(x,y);return JSON.stringify({x,y,hit:at?.outerHTML.slice(0,180),reachable:!!at&&(at===el||el.contains(at))});})()`, selector))
				var point struct {
					X, Y      float64
					Reachable bool
				}
				if err := json.Unmarshal([]byte(geometry), &point); err != nil {
					t.Fatal(err)
				}
				if !point.Reachable {
					t.Errorf("control %s cannot be reached by pointer at %dx%d: %s", selector, viewport.width, viewport.height, geometry)
					return false
				}
				for _, eventType := range []string{"mousePressed", "mouseReleased"} {
					if err := client.call(ctx, "Input.dispatchMouseEvent", map[string]any{"type": eventType, "x": point.X, "y": point.Y, "button": "left", "clickCount": 1}, nil); err != nil {
						t.Fatal(err)
					}
				}
				return true
			}
			chooseView := func(view string) {
				t.Helper()
				if viewport.width <= 1080 {
					if !click("#compactNavigation > summary") {
						t.Fatal("compact navigation is unreachable")
					}
					if !click(fmt.Sprintf(`[data-compact-view=%q]`, view)) {
						t.Fatal("compact view choice is unreachable")
					}
				} else if view == "plans" {
					click("#plansViewButton")
				} else {
					click(fmt.Sprintf(`#viewSwitch [data-view=%q]`, view))
				}
			}
			waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
			evaluate(`document.querySelector('[data-open-project]').click(); 'open'`)
			waitBrowserValue(t, ctx, client, `state.currentProject?.name === 'Responsive workspace project' ? 'composer' : ''`, "composer")
			evaluate(`window.__e2eModelEvents=[];['pointerdown','pointerup','click','change','close'].forEach(kind=>document.addEventListener(kind,event=>{const target=event.target,button=target.closest?.('button');if(!['startModelOptions','sessionSetupDone','sessionSetupClose','chatProviderSelect','sessionSetupDialog'].includes(button?.id||target.id))return;window.__e2eModelEvents.push({kind,target:target.id,button:button?.id,at:performance.now(),open:document.querySelector('#sessionSetupDialog')?.open,value:document.querySelector('#chatProviderSelect')?.value,draft:state.draftProviderID,focus:document.activeElement?.id});if(window.__e2eModelEvents.length>40)window.__e2eModelEvents.shift()},true));'model-event-probe'`)
			checkHeader("start")
			captureLayout("start")
			if viewport.width <= 1080 {
				if click("#toggleRail") {
					waitBrowserValue(t, ctx, client, `document.querySelector('#zones').classList.contains('mobile-rail-open') && document.querySelector('#toggleRail').getAttribute('aria-expanded') === 'true' ? 'rail-open' : ''`, "rail-open")
					if click("#toggleRail") {
						waitBrowserValue(t, ctx, client, `!document.querySelector('#zones').classList.contains('mobile-rail-open') && document.querySelector('#toggleRail').getAttribute('aria-expanded') === 'false' ? 'rail-closed' : ''`, "rail-closed")
					}
				}
			}
			evaluate(`document.querySelector('#startPrompt').focus(); 'focus-goal'`)
			if err := client.call(ctx, "Input.insertText", map[string]any{"text": "Preserve this goal while navigating."}, nil); err != nil {
				t.Fatal(err)
			}
			if viewport.width > 1080 {
				evaluate(`document.querySelector('#toggleRail').click(); 'collapse'`)
			}
			click("#startModelOptions")
			waitBrowserValue(t, ctx, client, `document.querySelector('#sessionSetupDialog')?.open ? 'model-dialog' : ''`, "model-dialog")
			waitBrowserValue(t, ctx, client, `(() => {const el=document.querySelector('#chatProviderSelect');const r=el.getBoundingClientRect();const at=document.elementFromPoint(r.x+r.width/2,r.y+r.height/2);return at&&(at===el||el.contains(at))?'model-choice-reachable':'';})()`, "model-choice-reachable")
			evaluate(fmt.Sprintf(`document.querySelector('#chatProviderSelect').value=%q;document.querySelector('#chatProviderSelect').dispatchEvent(new Event('change',{bubbles:true}));'change-model'`, otherProvider.ID))
			click("#sessionSetupDone")
			waitBrowserValue(t, ctx, client, `!document.querySelector('#sessionSetupDialog')?.open && document.querySelector('#startPrompt')?.value === 'Preserve this goal while navigating.' ? 'model-saved-goal-preserved' : ''`, "model-saved-goal-preserved")
			click("#startModelOptions")
			waitBrowserValue(t, ctx, client, fmt.Sprintf(`document.querySelector('#sessionSetupDialog')?.open && document.querySelector('#chatProviderSelect')?.value === %q ? 'choice-preserved' : ''`, otherProvider.ID), "choice-preserved")
			click("#sessionSetupClose")
			chooseView("plans")
			waitBrowserValue(t, ctx, client, `document.querySelector('#plansOverviewTitle') ? 'plans' : ''`, "plans")
			captureLayout("plans")
			chooseView("code")
			if viewport.width <= 1080 {
				click("#paneAdd")
				click(`[data-open-side-pane="team"]`)
				waitBrowserValue(t, ctx, client, `document.querySelector('[data-compact-pane="team"]')?.getAttribute('aria-pressed') === 'true' && document.querySelector('#teamCreateForm') && document.querySelector('#addTeamMember')?.textContent.includes('เพิ่มสมาชิก') ? 'team-pane' : ''`, "team-pane")
				captureLayout("team")
				if viewport.width <= 700 {
					click("#workspaceToolbarToggle")
				}
			}
			click("#workspaceIDE")
			waitBrowserValue(t, ctx, client, `[...document.querySelectorAll('#workspacePaneHost .pane-body')].map(node=>node.dataset.paneKind).join(',')`, "review,files")
			if viewport.width <= 1080 {
				evaluate(`document.querySelector('[data-compact-pane="files"]').click(); 'files-tab'`)
			}
			waitBrowserValue(t, ctx, client, `document.querySelector('[data-workbench-file="main.go"]') ? 'files' : ''`, "files")
			evaluate(`document.querySelector('[data-workbench-file="main.go"]').click(); 'file'`)
			waitBrowserValue(t, ctx, client, `[...document.querySelectorAll('#workspacePaneHost .pane-body')].map(node=>node.dataset.paneKind).join(',')`, "editor,files")
			chooseView("plans")
			waitBrowserValue(t, ctx, client, `document.querySelector('#plansOverviewTitle') ? 'plans' : ''`, "plans")
			chooseView("chat")
			waitBrowserValue(t, ctx, client, `document.querySelector('#startPrompt')?.value === 'Preserve this goal while navigating.' ? 'goal-preserved' : ''`, "goal-preserved")
			chooseView("code")
			waitBrowserValue(t, ctx, client, `[...document.querySelectorAll('#workspacePaneHost .pane-body')].map(node=>node.dataset.paneKind).join(',')`, "editor,files")
			if viewport.width <= 1080 {
				evaluate(`document.querySelector('[data-compact-pane="files"]').click(); 'files-tab'`)
			}
			waitBrowserValue(t, ctx, client, `document.querySelector('[data-workbench-file="main.go"]') ? 'files' : ''`, "files")
			evaluate(`document.querySelector('[data-workbench-file="main.go"]').click(); 'file'`)
			waitBrowserValue(t, ctx, client, `document.querySelector('.cm-editor') ? 'editor' : ''`, "editor")
			waitForStyledEditor(t, ctx, client)
			waitBrowserValue(t, ctx, client, `(() => {const actions=[...document.querySelectorAll('[data-editor-action]')];return actions.length===4 && actions.every(button=>!button.disabled && button.title) ? 'supported-actions-explained' : '';})()`, "supported-actions-explained")
			evaluate(`document.querySelector('.cm-content').focus(); document.execCommand('insertText',false,'// draft-layout\n'); 'edit'`)
			chooseView("chat")
			chooseView("code")
			waitBrowserValue(t, ctx, client, `document.querySelector('.cm-content')?.textContent.includes('draft-layout') ? 'draft-preserved' : ''`, "draft-preserved")
			captureLayout("editor-unsaved")
			if click(".code-editor-toolbar button.primary") {
				waitBrowserValue(t, ctx, client, `document.querySelector('#codeSaveState')?.textContent === 'Saved' ? 'saved' : ''`, "saved")
				content, err := os.ReadFile(filepath.Join(root, "main.go"))
				if err != nil || !strings.Contains(string(content), "draft-layout") {
					t.Fatalf("pointer Save did not persist the returned editor draft: %q, err=%v", content, err)
				}
				captureLayout("editor-saved")
			}
		})
	}
}

func waitForStyledEditor(t *testing.T, ctx context.Context, client *e2eCDPClient) {
	t.Helper()
	waitBrowserValue(t, ctx, client, `(() => {
		const scroller=document.querySelector('.cm-scroller'), host=document.querySelector('.code-editor-host');
		const line=document.querySelector('.cm-line'), gutter=[...document.querySelectorAll('.cm-gutterElement')].find(el=>el.textContent.trim()==='1');
		if(!scroller||!host||!line||!gutter||getComputedStyle(scroller).display!=='flex')return '';
		const code=line.getBoundingClientRect(), number=gutter.getBoundingClientRect(), bounds=host.getBoundingClientRect();
		return code.bottom>number.top && number.bottom>code.top && code.left>=number.right-1 && code.left<bounds.right && code.top>=bounds.top-1 && code.bottom<=bounds.bottom+1 ? 'styled-readable-editor' : '';
	})()`, "styled-readable-editor")
}

func waitBrowserValue(t *testing.T, ctx context.Context, client *e2eCDPClient, expression, want string) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	var value string
	var err error
	for time.Now().Before(deadline) {
		// The Project picker sets currentProject before bootstrap and surface
		// hydration finish. Wait for that transition before reading any page
		// assertion so tests never drive half-loaded controls.
		value, err = client.evaluate(ctx, "typeof state !== 'undefined' && state.projectOpening ? '__project-opening__' : ("+expression+")")
		if err == nil && value == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	diagnostic, diagnosticErr := client.evaluate(ctx, `JSON.stringify({chat:{sending:state.sending,session:state.sessionDetail?.session?.state,control:state.sessionDetail?.turn_control,queue:state.sessionDetail?.queued_messages,failures:state.sessionDetail?.events?.filter(e=>e.event_kind==='turn_failed').map(e=>e.content),error:state.chatTurnError},project:state?.currentProject?.id||'',projectOpening:state?.projectOpening||false,view:state?.view||'',tab:state?.activeTab||'',statePanes:state?.panes||[],compactPane:state?.compactPane||'',layout:state?.paneLayout||'',viewport:innerWidth,toast:document.querySelector('#toast')?.textContent||'',dialog:document.querySelector('#actionDialog')?.open||false,openProjectError:window.__e2eOpenProjectError||'',panes:document.querySelector('#workspacePaneHost')?.textContent?.slice(0,240)||'',office:{filter:typeof officeFilter==='undefined'?'':officeFilter,limit:typeof officeHistoryLimit==='undefined'?0:officeHistoryLimit,jobs:state?.jobs?.length,rows:document.querySelectorAll('.office-entry').length,moreHidden:document.querySelector('#officeMore')?.hidden,count:document.querySelector('#officeCount')?.textContent},task:(()=>{const b=document.querySelector('.pane-body[data-pane-kind="tasks"]'),t=document.querySelector('#taskAutoPlan'),id=state.selectedDurableTask;return{selected:id,action:id&&taskActions.get(id),error:id&&taskErrors.get(id),brief:id&&state.taskExecutions[id]?.content?.brief,contentSteps:document.querySelectorAll('.task-content-step').length,briefForm:!!document.querySelector('#taskContentBriefForm'),draftForm:!!document.querySelector('[data-content-draft="facebook"]'),jump:typeof pendingTaskJump==='undefined'?null:pendingTaskJump,bodyScroll:b?.scrollTop,bodyHeight:b?.clientHeight,bodyContentHeight:b?.scrollHeight,bodyOverflow:b&&getComputedStyle(b).overflowY,targetTop:t?.getBoundingClientRect().top,detailsOpen:t?.closest('details')?.open}})()})`)
	modelDiagnostic, modelDiagnosticErr := client.evaluate(ctx, `JSON.stringify({open:document.querySelector('#sessionSetupDialog')?.open,value:document.querySelector('#chatProviderSelect')?.value,draft:state.draftProviderID,default:state.defaultProviderID,focus:document.activeElement?.id,events:window.__e2eModelEvents||[]})`)
	// Capture only after the failed observation. Taking a screenshot before
	// the assertion can itself commit a compositor frame and conceal a race.
	if directory := os.Getenv("HERMETRIX_E2E_FAILURE_DIR"); directory != "" {
		var shot struct {
			Data string `json:"data"`
		}
		if captureErr := client.call(ctx, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": false}, &shot); captureErr == nil {
			if pixels, decodeErr := base64.StdEncoding.DecodeString(shot.Data); decodeErr == nil {
				label := strings.Map(func(r rune) rune {
					if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
						return r
					}
					return '_'
				}, t.Name())
				if len(label) > 96 {
					label = label[:96]
				}
				name := fmt.Sprintf("%s-%d", label, time.Now().UnixNano())
				path := filepath.Join(directory, name+".png")
				if saveErr := os.WriteFile(path, pixels, 0600); saveErr == nil {
					t.Logf("failed browser view: %s", path)
				} else {
					t.Logf("failed browser capture save: %v", saveErr)
				}
			}
		} else {
			t.Logf("failed browser capture: %v", captureErr)
		}
	}
	t.Fatalf("browser value did not become %q: value=%q err=%v diagnostic=%q diagnostic_err=%v model=%q model_err=%v", want, value, err, diagnostic, diagnosticErr, modelDiagnostic, modelDiagnosticErr)
}

type e2eChromeTarget struct {
	ID                   string `json:"id"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func startE2EChrome(ctx context.Context, executable, profile string) (string, func(), error) {
	processCtx, cancel := context.WithCancel(ctx)
	args := []string{
		"--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
		"--disable-background-networking", "--no-first-run", "--no-default-browser-check",
		"--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", "--user-data-dir=" + profile}
	if wav := os.Getenv("HERMETRIX_E2E_FAKE_MIC_WAV"); wav != "" {
		args = append(args, "--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream", "--use-file-for-fake-audio-capture="+wav)
	}
	args = append(args, "about:blank")
	command := exec.CommandContext(processCtx, executable, args...)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		cancel()
		return "", nil, err
	}
	stop := func() {
		cancel()
		_ = command.Wait()
	}
	portFile := filepath.Join(profile, "DevToolsActivePort")
	for {
		data, err := os.ReadFile(portFile)
		if err == nil {
			fields := strings.Fields(string(data))
			if len(fields) > 0 {
				return "http://127.0.0.1:" + fields[0], stop, nil
			}
		}
		select {
		case <-ctx.Done():
			stop()
			return "", nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func createE2ETarget(ctx context.Context, debugURL, pageURL string) (e2eChromeTarget, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, debugURL+"/json/new?"+url.QueryEscape(pageURL), nil)
	if err != nil {
		return e2eChromeTarget{}, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return e2eChromeTarget{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return e2eChromeTarget{}, fmt.Errorf("create Chrome target returned %s", response.Status)
	}
	var target e2eChromeTarget
	if err := json.NewDecoder(response.Body).Decode(&target); err != nil {
		return e2eChromeTarget{}, err
	}
	return target, nil
}

type e2eCDPClient struct {
	connection    *websocket.Conn
	nextID        int64
	browserErrors []json.RawMessage
}

func (c *e2eCDPClient) call(ctx context.Context, method string, params, result any) error {
	c.nextID++
	id := c.nextID
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.connection.SetWriteDeadline(deadline)
		_ = c.connection.SetReadDeadline(deadline)
	}
	if err := c.connection.WriteJSON(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		_, data, err := c.connection.ReadMessage()
		if err != nil {
			return err
		}
		var response struct {
			Method string          `json:"method"`
			ID     int64           `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &response) != nil {
			continue
		}
		if response.Method == "Log.entryAdded" || response.Method == "Runtime.exceptionThrown" {
			if len(c.browserErrors) < 100 {
				c.browserErrors = append(c.browserErrors, append(json.RawMessage(nil), data...))
			}
		}
		if response.ID != id {
			continue
		}
		if response.Error != nil {
			return errors.New(response.Error.Message)
		}
		if result != nil {
			return json.Unmarshal(response.Result, result)
		}
		return nil
	}
}

// Native CDP input can hit the previous compositor layout immediately after
// navigation or viewport emulation, even when DOM geometry already changed.
func (c *e2eCDPClient) waitForPaint(ctx context.Context) error {
	return c.call(ctx, "Runtime.evaluate", map[string]any{"expression": `new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve('painted'))))`, "awaitPromise": true, "returnByValue": true}, nil)
}

func (c *e2eCDPClient) evaluate(ctx context.Context, expression string) (string, error) {
	var response struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := c.call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true}, &response); err != nil {
		return "", err
	}
	if len(response.ExceptionDetails) > 0 && string(response.ExceptionDetails) != "null" {
		return "", fmt.Errorf("browser evaluation failed: %s", response.ExceptionDetails)
	}
	return response.Result.Value, nil
}

func browserExecutableForE2E() string {
	if configured := strings.TrimSpace(os.Getenv("HERMETRIX_E2E_CHROME")); configured != "" {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured
		}
	}
	if runtime.GOOS == "darwin" {
		for _, candidate := range []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		} {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	if runtime.GOOS == "windows" {
		for _, candidate := range []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
		} {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func abbreviateDOM(value string) string {
	const limit = 2000
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n… clipped …"
}

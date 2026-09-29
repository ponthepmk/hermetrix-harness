package web

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestProjectBrainCandidatePanelInARealBrowser checks script ordering, the
// explicit preview gate, text fit and mobile controls in the shipped UI. The
// fixture is a completed task; it never calls Pi or queues a candidate.
func TestProjectBrainCandidatePanelInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome is required for the UI E2E job")
		}
		t.Skip("Chrome is not installed")
	}
	server := testHTTPServer(t)
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
	evaluate := func(expression string) string {
		t.Helper()
		value, err := client.evaluate(ctx, expression)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if evaluate(`typeof window.HermetrixProjectBrain?.render`) == "function" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if evaluate(`typeof window.HermetrixProjectBrain?.render`) != "function" {
		t.Fatal("Project Brain UI script did not load")
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1440, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	setup := `(() => {
      document.querySelector('#projectPicker').hidden = true;
      document.querySelector('#appShell').hidden = true;
      const host = document.createElement('main'); host.className = 'pane-body'; host.dataset.paneKind = 'tasks';
      host.innerHTML = '<div class="panel task-cockpit"><div id="brain-fixture"></div></div>';
      document.body.append(host);
      window.__brainPosts = 0;
      const eligibility = {task_id:'task-fixture',title:'Repair a planning issue',state:'completed',task_revision:4,eligible:true,validations:[{
        id:'validation-fixture',requirement_id:'AC-1',check_id:'go test ./...',subject_revision:'requirement:2',observed_at:'2026-09-24T00:00:00Z',artifacts:[
          {id:'artifact-fixture',name:'Saved test output',content_digest:'sha256:'+'a'.repeat(64),visibility:'project_shared',export_policy:'explicit_selection',sharing_revision:2,eligible:true}
        ]}]};
      const deps = {api:async (path,options) => {
          if (!options && path.endsWith('/project-brain-eligibility')) return eligibility;
          if (path.endsWith('/preview')) return {candidate:{candidate_id:'candidate-fixture',title:'Repair a planning issue',problem:'Planning project IDs were confused',solution:'Bind the board project first.',verification:{kind:'test',scope:'requirement:2',evidence_refs:[{evidence_id:'artifact-fixture'}]}},approval:{actor:'owner'},selected_artifact_ids:['artifact-fixture']};
          window.__brainPosts++; throw Error('fixture must not submit');
        }, askAction:async()=>false, load:async()=>{}, currentActor:()=> 'owner',
        escapeHTML:value=>String(value??'').replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('"','&quot;'),
        formatDate:value=>value.slice(0,10)};
      HermetrixProjectBrain.render(host.querySelector('#brain-fixture'),{id:'task-fixture',title:'Repair a planning issue',state:'completed',sharing_revision:2},
        {id:'project-fixture',sharing_revision:2},deps);
      host.querySelector('[data-brain-toggle]').click();
      return 'mounted';
    })()`
	if got := evaluate(setup); got != "mounted" {
		t.Fatalf("fixture did not mount: %q", got)
	}
	for time.Now().Before(deadline) {
		if evaluate(`Boolean(document.querySelector('[data-brain-form]')) ? 'ready' : 'loading'`) == "ready" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if evaluate(`Boolean(document.querySelector('[data-brain-form]')) ? 'ready' : 'loading'`) != "ready" {
		t.Fatal("eligible completed task did not show candidate form")
	}
	if got := evaluate(`(() => {const root=document.querySelector('#brain-fixture');const box=root.querySelector('input[name="artifact_ids"]');box.checked=true;box.dispatchEvent(new Event('input',{bubbles:true}));const text=root.querySelector('textarea[name="solution"]');text.value='Bind the board project first.';text.dispatchEvent(new Event('input',{bubbles:true}));root.querySelector('[data-brain-preview]').click();return String(window.__brainPosts)})()`); got != "0" {
		t.Fatalf("preview dispatched candidate: %q", got)
	}
	for time.Now().Before(deadline) {
		if evaluate(`Boolean(document.querySelector('.brain-preview')) ? 'ready' : 'loading'`) == "ready" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if evaluate(`Boolean(document.querySelector('.brain-preview')) ? 'ready' : 'loading'`) != "ready" {
		t.Fatal("preview did not render")
	}
	for _, viewport := range []struct{ width, height int }{{1440, 900}, {390, 844}} {
		if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": viewport.width, "height": viewport.height, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
			t.Fatal(err)
		}
		metrics := evaluate(`(() => {const root=document.querySelector('#brain-fixture');const buttons=[...root.querySelectorAll('button')].filter(item=>item.getClientRects().length);return JSON.stringify({overflow:document.documentElement.scrollWidth>innerWidth+1,small:buttons.filter(item=>item.getBoundingClientRect().height<43).map(item=>item.textContent.trim())})})()`)
		if !strings.Contains(metrics, `"overflow":false`) || !strings.Contains(metrics, `"small":[]`) {
			t.Errorf("Project Brain panel layout at %d×%d: %s", viewport.width, viewport.height, metrics)
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
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, fmt.Sprintf("project-brain-share-%dx%d.png", viewport.width, viewport.height))
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			t.Log("Project Brain screenshot:", path)
		}
	}
	if got := evaluate(`String(window.__brainPosts)`); got != "0" {
		t.Fatalf("candidate submitted without final click: %q", got)
	}
}

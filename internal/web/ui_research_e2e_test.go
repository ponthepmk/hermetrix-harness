package web

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hermetrix-harness/internal/taskengine"
)

func TestResearchTaskEntryIsVisibleInRealChrome(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	server := testHTTPServer(t)
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Research fixture", "root_path": t.TempDir()}, http.StatusCreated), &project); err != nil {
		t.Fatal(err)
	}
	var task taskengine.Task
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/tasks", http.MethodPost, taskengine.CreateTaskInput{
		ProjectID: project.ID, Title: "Article task", Objective: "Find recent work", OriginalRequest: "Find recent work", Actor: "owner",
		Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "Review sources"}}, EgressPolicy: "remote_allowed",
		RemoteEgressApproval: &taskengine.TaskEgressApproval{Actor: "owner", Reason: "Crossref research"},
	}, http.StatusCreated), &task); err != nil {
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
	if err := client.call(ctx, "Page.enable", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-open-project]').click(); 'opened'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#startPrompt') ? 'ready' : ''`, "ready")
	// The shell can paint before openProject finishes loading sessions and layout.
	// Real pointer input is blocked by inert until then; the CDP fixture must
	// wait for the same boundary before calling an internal pane action.
	waitBrowserValue(t, ctx, client, `!document.querySelector('#appShell')?.inert && !state.projectOpening && state.currentProject?.id ? 'project-ready' : ''`, "project-ready")
	if _, err := client.evaluate(ctx, `openContentPane('tasks'); 'tasks'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskResearchForm [name="query"]') && document.querySelector('#taskResearchHeading') ? 'research' : ''`, "research")
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskComfyPreflightForm [name="workflow_path"]') && !document.querySelector('#taskComfyStart') ? 'comfy-preflight' : ''`, "comfy-preflight")
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskSocialSettings') && document.querySelectorAll('.task-content-step').length >= 6 ? 'social-step' : ''`, "social-step")
	if _, err := client.evaluate(ctx, `document.querySelector('#taskSocialSettings').click(); 'accounts-opened'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#socialConnectForm [name="token"]')?.type === 'password' ? 'secure-token-form' : ''`, "secure-token-form")
	waitBrowserValue(t, ctx, client, `getComputedStyle(document.querySelector('#socialModeLabel')).display === 'none' ? 'facebook-mode-hidden' : ''`, "facebook-mode-hidden")
	if _, err := client.evaluate(ctx, `(() => { const select=document.querySelector('#socialConnectForm [name="platform"]'); select.value='tiktok'; select.dispatchEvent(new Event('change',{bubbles:true})); return 'tiktok-selected'; })()`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `getComputedStyle(document.querySelector('#socialModeLabel')).display !== 'none' ? 'tiktok-mode-visible' : ''`, "tiktok-mode-visible")
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const r=document.querySelector('#socialConnectForm [name="token"]')?.getBoundingClientRect(); return r && r.width > 0 && r.left >= -1 && r.right <= innerWidth + 1 && document.documentElement.scrollWidth <= innerWidth + 1 ? 'social-mobile-fits' : ''; })()`, "social-mobile-fits")
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1440, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `closeConfig(); 'accounts-closed'`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#taskResearchForm [name="query"]').value='AI small business'; document.querySelector('#taskResearchForm [name="query"]').dispatchEvent(new Event('input',{bubbles:true})); 'draft'`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `renderCurrentTaskCockpit(state.selectedDurableTask); 'rerendered'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#taskResearchForm [name="query"]')?.value === 'AI small business' ? 'preserved' : ''`, "preserved")
	if _, err := client.evaluate(ctx, `(() => { const form=document.querySelector('#taskContentBriefForm'); for (const [key,value] of Object.entries({topic:'AI small business',audience:'Store owners',language:'Thai',facebook_page:'Fixture Page'})) form.elements[key].value=value; form.requestSubmit(); return 'brief-submit'; })()`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#actionDialog')?.open ? 'brief-review' : ''`, "brief-review")
	if _, err := client.evaluate(ctx, `document.querySelector('#actionConfirm').click(); 'brief-confirmed'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-content-draft="facebook"]') ? 'facebook-editor' : ''`, "facebook-editor")
	if _, err := client.evaluate(ctx, `(() => { const form=document.querySelector('[data-content-draft="facebook"]'); form.elements.body.value='A careful summary with a source.'; form.requestSubmit(); return 'draft-submit'; })()`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#actionDialog')?.open ? 'draft-review' : ''`, "draft-review")
	if _, err := client.evaluate(ctx, `document.querySelector('#actionConfirm').click(); 'draft-confirmed'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.task-content-preview')?.textContent.includes('A careful summary with a source.') && document.querySelector('[data-content-approve]')?.disabled ? 'draft-blocked-until-claims' : ''`, "draft-blocked-until-claims")
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": true}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.documentElement.scrollWidth <= innerWidth && document.querySelector('#taskResearchForm [name="query"]')?.getBoundingClientRect().width < innerWidth && document.querySelector('#taskComfyPreflightForm')?.getBoundingClientRect().right <= innerWidth+1 && document.querySelector('.task-content-draft')?.getBoundingClientRect().right <= innerWidth+1 ? 'mobile-fits' : ''`, "mobile-fits")
}

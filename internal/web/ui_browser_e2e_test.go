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
	if _, err := client.evaluate(ctx, `document.querySelector('#startChatButton').click(); 'sent'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `Array.from(document.querySelectorAll('.chat-message.assistant:not(.streaming) .message-body')).some(node => node.textContent.includes('Ready from the browser fixture.')) ? 'replied' : ''`, "replied")
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
	// Ask next to an edited file, keeping its unsaved buffer intact. The local
	// pane must render the complete persisted reply, not just a toast or tail.
	if _, err := client.evaluate(ctx, `document.querySelector('#viewSwitch [data-view="code"]').click(); 'workspace'`); err != nil {
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
	waitBrowserValue(t, ctx, client, `!state.sending && document.querySelectorAll('.ide-assistant-log .chat-message.assistant').length === 2 ? 'coding-replied' : ''`, "coding-replied")
	waitBrowserValue(t, ctx, client, `[...document.querySelectorAll('.ide-assistant-log .chat-message.user')].some(node=>node.textContent.includes('unsaved editor buffer') && node.textContent.includes('Unsaved local context')) && activeCodeEditor?.getValue().includes('Unsaved local context') ? 'context-and-draft-preserved' : ''`, "context-and-draft-preserved")
	if content, err := os.ReadFile(filepath.Join(projectRoot, "README.md")); err != nil || string(content) != "# Browser fixture\n" {
		t.Fatalf("asking the local assistant changed the unsaved file on disk: %q %v", content, err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#viewSwitch [data-view="chat"]').click(); 'return-chat'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#chatInput').focus(); 'focus-draft'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.insertText", map[string]any{"text": "Keep this chat draft after browsing files."}, nil); err != nil {
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
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`document.querySelector('#durableTaskForm textarea[name="objective"]')?.value === %q ? 'goal-preserved' : ''`, plannedGoal), "goal-preserved")
	const criterion = "A new user can start work from the project screen."
	if _, err := client.evaluate(ctx, `document.querySelector('#durableTaskForm textarea[name="criteria"]').focus(); 'focused'`); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.insertText", map[string]any{"text": criterion}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `document.querySelector('#durableTaskForm button.primary').click(); 'create-task'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`document.querySelector('#taskPlannerProvider')?.value === %q && !document.querySelector('#taskAutoPlan')?.disabled ? 'plan-ready' : ''`, provider.ID), "plan-ready")
	taskResponse, err := http.Get(server.URL + "/api/tasks?limit=100")
	if err != nil {
		t.Fatal(err)
	}
	defer taskResponse.Body.Close()
	var tasks []struct {
		ProjectID   string `json:"project_id"`
		Objective   string `json:"objective"`
		Requirement struct {
			Criteria []struct {
				Description string `json:"description"`
			} `json:"criteria"`
		} `json:"requirement"`
	}
	if err := json.NewDecoder(taskResponse.Body).Decode(&tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ProjectID != project.ID || tasks[0].Objective != plannedGoal ||
		len(tasks[0].Requirement.Criteria) != 1 || tasks[0].Requirement.Criteria[0].Description != criterion {
		t.Fatalf("the plan form did not persist the goal and observable success criterion: %+v", tasks)
	}
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
	waitBrowserValue(t, ctx, client, `document.querySelector('#plansViewButton').classList.contains('on') && document.querySelector('.planning-toolbar') && !document.querySelector('#paneAdd').getClientRects().length ? 'focused-plans' : ''`, "focused-plans")
	if _, err := client.evaluate(ctx, `document.querySelector('#viewSwitch [data-view="code"]').click(); 'open-workspace'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!document.querySelector('#plansViewButton').classList.contains('on') && document.querySelector('#paneAdd').getClientRects().length && Array.from(document.querySelectorAll('#workspacePaneHost .pane-head')).some(node => node.getClientRects().length) ? 'workspace-restored' : ''`, "workspace-restored")
	if _, err := client.evaluate(ctx, `document.querySelector('#railNewSession').click(); 'new-from-workspace'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#startPrompt') && document.querySelector('#zones').classList.contains('side-hidden') ? 'focused-new-task' : ''`, "focused-new-task")
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
	if _, err := client.evaluate(ctx, fmt.Sprintf(`openProject(%q).then(() => { switchView('code'); }); 'opened'`, project.ID)); err != nil {
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
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-editor-wrap]')?.checked && document.querySelector('.cm-content')?.classList.contains('cm-lineWrapping') ? 'wrapped' : ''`, "wrapped")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-editor-wrap]').click(); 'wrap-off'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!document.querySelector('[data-editor-wrap]')?.checked && !document.querySelector('.cm-content')?.classList.contains('cm-lineWrapping') ? 'unwrapped' : ''`, "unwrapped")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-editor-wrap]').click(); 'wrap-on'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `localStorage.getItem('hermetrix.ide.wordWrap') === 'true' && document.querySelector('.cm-content')?.classList.contains('cm-lineWrapping') ? 'wrap-persisted' : ''`, "wrap-persisted")
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
	if runtime.GOOS == "windows" {
		waitBrowserValue(t, ctx, client, `document.querySelector('.xterm-rows')?.textContent.includes('PS ') ? 'powershell-ready' : ''`, "powershell-ready")
	}

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
		value, _ := client.evaluate(ctx, `document.querySelector('.xterm-rows')?.textContent.includes('PTY-E2E') ? 'pty' : ''`)
		if value == "pty" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	diagnostic, _ := client.evaluate(ctx, `JSON.stringify({terminal:document.querySelector('.xterm-rows')?.textContent,terminals:state.terminals,selected:state.selectedTerminal,active:document.activeElement?.className,toasts:[...document.querySelectorAll('.toast')].map(el=>el.textContent)})`)
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
		geometry, err := client.evaluate(ctx, fmt.Sprintf(`(() => {const el=document.querySelector(%q);if(!el)return '{}';const r=el.getBoundingClientRect();const x=r.x+r.width/2,y=r.y+r.height/2;const at=document.elementFromPoint(x,y);return JSON.stringify({x,y,reachable:!!at&&(at===el||el.contains(at))});})()`, selector))
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
		waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'project' : ''`, "project")
		click("[data-open-project]")
		waitBrowserValue(t, ctx, client, `document.querySelector('#startPrompt') ? 'project-open' : ''`, "project-open")
		click(`#viewSwitch [data-view="code"]`)
		waitBrowserValue(t, ctx, client, `document.querySelector('[data-workbench-file="greeting.js"]') ? 'file-listed' : ''`, "file-listed")
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
				geometry := evaluate(fmt.Sprintf(`(() => {const el=document.querySelector(%q);if(!el)return '{}';const r=el.getBoundingClientRect();const x=r.x+r.width/2,y=r.y+r.height/2;const at=document.elementFromPoint(x,y);return JSON.stringify({x,y,reachable:!!at&&(at===el||el.contains(at))});})()`, selector))
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
			waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
			evaluate(`document.querySelector('[data-open-project]').click(); 'open'`)
			waitBrowserValue(t, ctx, client, `document.querySelector('#startPrompt') ? 'composer' : ''`, "composer")
			checkHeader("start")
			captureLayout("start")
			evaluate(`document.querySelector('#startPrompt').focus(); 'focus-goal'`)
			if err := client.call(ctx, "Input.insertText", map[string]any{"text": "Preserve this goal while navigating."}, nil); err != nil {
				t.Fatal(err)
			}
			if viewport.width > 700 {
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
			click("#plansViewButton")
			waitBrowserValue(t, ctx, client, `document.querySelector('#durableTaskForm') ? 'plans' : ''`, "plans")
			captureLayout("plans")
			click(`#viewSwitch [data-view="code"]`)
			waitBrowserValue(t, ctx, client, `[...document.querySelectorAll('#workspacePaneHost .pane-body')].map(node=>node.dataset.paneKind).join(',')`, "editor,files")
			click("#plansViewButton")
			waitBrowserValue(t, ctx, client, `document.querySelector('#durableTaskForm') ? 'plans' : ''`, "plans")
			evaluate(`document.querySelector('#viewSwitch [data-view="chat"]').click(); 'chat'`)
			waitBrowserValue(t, ctx, client, `document.querySelector('#startPrompt')?.value === 'Preserve this goal while navigating.' ? 'goal-preserved' : ''`, "goal-preserved")
			evaluate(`document.querySelector('#viewSwitch [data-view="code"]').click(); 'workspace'`)
			waitBrowserValue(t, ctx, client, `[...document.querySelectorAll('#workspacePaneHost .pane-body')].map(node=>node.dataset.paneKind).join(',')`, "editor,files")
			if viewport.width <= 900 {
				evaluate(`document.querySelector('[data-compact-pane="files"]').click(); 'files-tab'`)
			}
			waitBrowserValue(t, ctx, client, `document.querySelector('[data-workbench-file="main.go"]') ? 'files' : ''`, "files")
			evaluate(`document.querySelector('[data-workbench-file="main.go"]').click(); 'file'`)
			waitBrowserValue(t, ctx, client, `document.querySelector('.cm-editor') ? 'editor' : ''`, "editor")
			waitForStyledEditor(t, ctx, client)
			waitBrowserValue(t, ctx, client, `(() => {const actions=[...document.querySelectorAll('[data-editor-action]')];return actions.length===4 && actions.every(button=>!button.disabled && button.title) ? 'supported-actions-explained' : '';})()`, "supported-actions-explained")
			evaluate(`document.querySelector('.cm-content').focus(); document.execCommand('insertText',false,'// draft-layout\n'); 'edit'`)
			evaluate(`document.querySelector('#viewSwitch [data-view="chat"]').click(); document.querySelector('#viewSwitch [data-view="code"]').click(); 'round-trip'`)
			waitBrowserValue(t, ctx, client, `document.querySelector('.cm-content')?.textContent.includes('draft-layout') ? 'draft-preserved' : ''`, "draft-preserved")
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
	deadline := time.Now().Add(8 * time.Second)
	var value string
	var err error
	for time.Now().Before(deadline) {
		value, err = client.evaluate(ctx, expression)
		if err == nil && value == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("browser value did not become %q: value=%q err=%v", want, value, err)
}

type e2eChromeTarget struct {
	ID                   string `json:"id"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func startE2EChrome(ctx context.Context, executable, profile string) (string, func(), error) {
	processCtx, cancel := context.WithCancel(ctx)
	command := exec.CommandContext(processCtx, executable,
		"--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
		"--disable-background-networking", "--no-first-run", "--no-default-browser-check",
		"--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", "--user-data-dir="+profile,
		"about:blank")
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
	connection *websocket.Conn
	nextID     int64
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
			ID     int64           `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &response) != nil || response.ID != id {
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

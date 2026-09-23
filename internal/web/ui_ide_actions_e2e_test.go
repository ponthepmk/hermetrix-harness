package web

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise user-visible actions against real file, command and debugger APIs.
// This is an isolated project; the user's checkout is never the test target.
func TestIDEFormatRunTestAndDebugInBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal("Chrome required")
		}
		t.Skip("Chrome unavailable")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node unavailable")
	}
	root := t.TempDir()
	original := "const total=2+3;\nconst doubled=total*2;\nconsole.log(doubled);\n"
	for name, content := range map[string]string{"main.js": original, "main.test.js": "const test = require('node:test'); const assert = require('node:assert/strict'); test('sum', () => assert.equal(2+3,5));\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	server := testHTTPServer(t)
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/projects", http.MethodPost, map[string]any{"name": "IDE action proof", "root_path": root}, http.StatusCreated), &project); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
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
	act := func(source string) {
		t.Helper()
		if _, err := client.evaluate(ctx, source); err != nil {
			t.Fatal(err)
		}
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready':''`, "ready")
	act(fmt.Sprintf(`openProject(%q).then(()=>{switchView('code');HermetrixWorkspace.open();return openWorkbenchFile('main.js')});'opening'`, project.ID))
	waitBrowserValue(t, ctx, client, `document.querySelector('.cm-editor') && document.querySelector('[data-pane-kind="files"]') ? 'ide':''`, "ide")
	act(`HermetrixWorkspace.panel('chat');'assistant'`)
	waitBrowserValue(t, ctx, client, `document.querySelector('.cm-editor') && document.querySelector('.ide-assistant') ? 'ide':''`, "ide")
	waitForStyledEditor(t, ctx, client)
	act(`document.querySelector('[data-editor-action="format"]').click();'format'`)
	waitBrowserValue(t, ctx, client, `document.querySelector('#codeFeedback')?.textContent.startsWith('Formatted') ? 'formatted':''`, "formatted")
	before, err := os.ReadFile(filepath.Join(root, "main.js"))
	if err != nil || string(before) != original {
		t.Fatalf("format wrote disk: %q %v", before, err)
	}
	act(`activeCodeEditor.focus();'focused'`)
	if err := client.call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "z", "code": "KeyZ", "modifiers": 2}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "z", "code": "KeyZ", "modifiers": 2}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `activeCodeEditor.getValue().startsWith('const total=2+3;')?'undo':''`, "undo")
	act(`document.querySelector('[data-editor-action="format"]').click();'format-again'`)
	waitBrowserValue(t, ctx, client, `activeCodeEditor.getValue().startsWith('const total = 2 + 3;')?'formatted':''`, "formatted")
	act(`document.querySelector('[data-editor-action="run"]').click();'run'`)
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-job-state]')?.textContent==='completed' && document.querySelector('.ide-console-output')?.textContent.includes('10') ? 'ran':''`, "ran")
	saved, err := os.ReadFile(filepath.Join(root, "main.js"))
	if err != nil || !strings.HasPrefix(string(saved), "const total = 2 + 3;") {
		t.Fatalf("run did not save exact formatted buffer: %q %v", saved, err)
	}
	act(`document.querySelector('[data-editor-action="test"]').click();'test'`)
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-job-state]')?.textContent==='completed' && document.querySelector('.ide-console-output')?.textContent.includes('pass 1')?'tested':''`, "tested")
	act(`document.querySelector('[data-editor-action="debug"]').click();'debug'`)
	waitBrowserValue(t, ctx, client, `document.querySelector('.ide-debug')?.textContent.includes('Node.js') && document.querySelector('.ide-debug')?.textContent.includes('Ready') ? 'debug-ready':''`, "debug-ready")
	act(`HermetrixWorkspace.breakpoint(state.projectFile,2,true);document.querySelector('[data-debug-start]').click();'started'`)
	waitBrowserValue(t, ctx, client, `document.querySelector('.ide-console-toolbar strong')?.textContent==='paused'?'paused':''`, "paused")
	act(`document.querySelector('[data-debug-action="continue"]').click();'continue'`)
	waitBrowserValue(t, ctx, client, `document.querySelector('.ide-stack-frame')?.textContent.includes('main.js:2')?'breakpoint':''`, "breakpoint")
	waitBrowserValue(t, ctx, client, `document.querySelector('.ide-variables')?.textContent.includes('total5')?'locals':''`, "locals")
	act(`document.querySelector('[data-debug-action="step_over"]').click();'step'`)
	waitBrowserValue(t, ctx, client, `document.querySelector('.ide-stack-frame')?.textContent.includes('main.js:3') && document.querySelector('.ide-variables')?.textContent.includes('doubled10')?'stepped':''`, "stepped")
	act(`document.querySelector('[data-debug-action="stop"]').click();'stop'`)
	waitBrowserValue(t, ctx, client, `document.querySelector('.ide-console-toolbar strong')?.textContent==='stopped'?'stopped':''`, "stopped")
	act(`HermetrixWorkspace.panel('environment');'editor-settings'`)
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-editor-format-save]') ? 'format-setting' : ''`, "format-setting")
	act(`document.querySelector('[data-editor-format-save]').click(); activeCodeEditor.setValue('const answer={value:42};\nconsole.log(answer.value)'); document.querySelector('#workbenchFileForm').requestSubmit(); 'format-save'`)
	waitBrowserValue(t, ctx, client, `document.querySelector('#codeSaveState')?.textContent === 'Saved' && activeCodeEditor.getValue().includes('const answer = { value: 42 };') ? 'auto-formatted' : ''`, "auto-formatted")
	formattedOnDisk, err := os.ReadFile(filepath.Join(root, "main.js"))
	if err != nil || string(formattedOnDisk) != "const answer = { value: 42 };\nconsole.log(answer.value);\n" {
		t.Fatalf("format-on-save did not persist the formatted buffer: %q %v", formattedOnDisk, err)
	}
	for _, width := range []int{1440, 900, 390} {
		if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": 900, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
			t.Fatal(err)
		}
		waitBrowserValue(t, ctx, client, `document.documentElement.scrollWidth<=window.innerWidth?'fits':''`, "fits")
	}
}

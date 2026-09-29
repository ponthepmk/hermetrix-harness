package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestMobileWorkspacePickerAndCodeTabClose(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Skip("Chrome unavailable")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := testHTTPServer(t)
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Mobile workspace", "root_path": root}, http.StatusCreated), &project); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
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
	captureFolder := func(name string) {
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
		if err := os.WriteFile(filepath.Join(directory, name), pixels, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.call(ctx, "Runtime.enable", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Page.enable", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.call(ctx, "Page.navigate", map[string]any{"url": server.URL}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-open-project]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, `document.querySelector('#viewSwitch [data-view="code"]').click(); 'picker'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!document.querySelector('#projectPicker').hidden ? 'picker' : ''`, "picker")
	if _, err := client.evaluate(ctx, fmt.Sprintf(`document.querySelector('[data-open-project=%q]').click(); 'opening'`, project.ID)); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`state.currentProject?.id === %q && state.view === 'code' && document.querySelector('#projectPicker').hidden ? 'workspace' : ''`, project.ID), "workspace")
	waitBrowserValue(t, ctx, client, `state.panes.join(',') === 'chat,review,files' && !document.querySelector('.pane[data-kind="editor"]') ? 'usage-first' : ''`, "usage-first")
	if _, err := client.evaluate(ctx, `localStorage.setItem(layoutKey(),JSON.stringify({panes:['chat','editor','files'],paneLayout:'adaptive'})); applyLayout(); renderPanes(); localStorage.removeItem(layoutKey()); 'legacy-layout'`); err != nil { t.Fatal(err) }
	waitBrowserValue(t, ctx, client, `state.panes.join(',') === 'chat,review,files' && !document.querySelector('.pane[data-kind="editor"]') ? 'migrated-layout' : ''`, "migrated-layout")
	waitBrowserValue(t, ctx, client, `document.querySelector('#workspaceToolbarToggle')?.getAttribute('aria-expanded') === 'false' && getComputedStyle(document.querySelector('#workspacePaneTabs')).display === 'none' ? 'menu-folded' : ''`, "menu-folded")
	if _, err := client.evaluate(ctx, `document.querySelector('#workspaceToolbarToggle').click(); 'open-menu'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#workspaceToolbarToggle')?.getAttribute('aria-expanded') === 'true' && document.querySelector('#workspacePaneTabs').getBoundingClientRect().height > 0 ? 'menu-open' : ''`, "menu-open")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-compact-pane="review"]').click(); 'select-review'`); err != nil { t.Fatal(err) }
	waitBrowserValue(t, ctx, client, `document.querySelector('.review-usage-panel h3')?.textContent === 'Token และ Context' ? 'usage-panel' : ''`, "usage-panel")
	if _, err := client.evaluate(ctx, `state.sessionDetail={session:{id:'usage-sample',title:'Usage sample',context_profile:'compact-16k',model:'sample-model',contract:{task_budget:{max_cumulative_tokens:3072}}},events:[{turn_id:'sample-turn',event_kind:'message',role:'assistant',metadata:{usage:{prompt_tokens:200,completion_tokens:50,total_tokens:250}}}]}; renderWorkbenchReview(); 'sample-usage'`); err != nil { t.Fatal(err) }
	waitBrowserValue(t, ctx, client, `document.querySelector('.review-usage-panel .token-meter progress')?.value === 250 && document.querySelector('.review-usage-panel .token-meter')?.textContent.includes('250 / 3,072 token') ? 'measured-usage' : ''`, "measured-usage")
	if _, err := client.evaluate(ctx, `state.sessionDetail=null; renderWorkbenchReview(); 'reset-usage'`); err != nil { t.Fatal(err) }
	if _, err := client.evaluate(ctx, `document.querySelector('#workspaceToolbarToggle').click(); 'open-menu'`); err != nil { t.Fatal(err) }
	if _, err := client.evaluate(ctx, `document.querySelector('[data-compact-pane="files"]').click(); 'select-files'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#workspaceToolbarToggle')?.getAttribute('aria-expanded') === 'false' && document.querySelector('.workspace-active-label')?.textContent.includes('ไฟล์') ? 'menu-refolded' : ''`, "menu-refolded")
	if _, err := client.evaluate(ctx, `openWorkbenchFile('note.txt'); 'opening-file'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-code-tab-close="note.txt"]') && state.panes.includes('editor') && activeCodeEditor?.getValue() === 'original\n' ? 'editor' : ''`, "editor")
	if _, err := client.evaluate(ctx, `activeCodeEditor.setValue('unsaved edit\n'); 'edited'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.projectFile?.content === 'unsaved edit\n' ? 'draft' : ''`, "draft")
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-code-tab-close="note.txt"]').getBoundingClientRect().width >= 44 ? 'touch-target' : ''`, "touch-target")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-code-tab-close="note.txt"]').click(); 'closed'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `!state.projectFile && !state.panes.includes('editor') && !document.querySelector('.pane[data-kind="editor"]') && state.panes.includes('review') ? 'closed' : ''`, "closed")
	if _, err := client.evaluate(ctx, `openWorkbenchFile('note.txt'); 'reopening'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `activeCodeEditor?.getValue() === 'unsaved edit\n' ? 'draft-restored' : ''`, "draft-restored")
	content, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil || string(content) != "original\n" {
		t.Fatalf("closing unsaved tab wrote disk: %q, %v", content, err)
	}
	if _, err := client.evaluate(ctx, `switchTab('projects'); 'projects'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.project-add-details summary') ? 'project-form' : ''`, "project-form")
	if _, err := client.evaluate(ctx, `document.querySelector('.project-add-details summary').click(); document.querySelector('#browseRoot').click(); 'picker'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#folderDialog').open && document.querySelector('#folderRoots button') ? 'folders' : ''`, "folders")
	waitBrowserValue(t, ctx, client, `(() => {const dialog=document.querySelector('#folderDialog').getBoundingClientRect(),list=document.querySelector('#folderBody').getBoundingClientRect(),action=document.querySelector('#folderChoose').getBoundingClientRect();return dialog.top>=-1 && dialog.bottom<=innerHeight+1 && list.height>=80 && action.bottom<=dialog.bottom+1 && document.querySelector('#folderTitle').textContent==='เลือกโฟลเดอร์'?'usable-folder-dialog':''})()`, "usable-folder-dialog")
	captureFolder("folder-picker-mobile.png")
	if _, err := client.evaluate(ctx, `(() => {const filter=document.querySelector('#folderFilter'); filter.value='no-such-folder-zzzz'; filter.dispatchEvent(new Event('input',{bubbles:true})); return 'filter'})()`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#folderFilterEmpty').hidden===false && [...document.querySelectorAll('[data-folder-entry]')].every(row=>row.hidden) ? 'filter-empty' : ''`, "filter-empty")
	if _, err := client.evaluate(ctx, `(() => {const filter=document.querySelector('#folderFilter'); filter.value=''; filter.dispatchEvent(new Event('input',{bubbles:true})); return 'clear-filter'})()`); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(`D:\`); err == nil {
			waitBrowserValue(t, ctx, client, `Array.from(document.querySelectorAll('[data-folder-root]')).some(button => button.dataset.folderRoot === 'D:\\') ? 'drive' : ''`, "drive")
			if _, err := client.evaluate(ctx, `Array.from(document.querySelectorAll('[data-folder-root]')).find(button => button.dataset.folderRoot === 'D:\\').click(); 'drive'`); err != nil {
				t.Fatal(err)
			}
			waitBrowserValue(t, ctx, client, `document.querySelector('#folderPath').textContent === 'D:\\' ? 'drive-open' : ''`, "drive-open")
		}
	}
	if _, err := client.evaluate(ctx, fmt.Sprintf(`document.querySelector('#folderAddress').value = %q; document.querySelector('#folderAddressForm').requestSubmit(); 'address'`, root)); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`document.querySelector('#folderPath').textContent === %q ? 'address-open' : ''`, root), "address-open")
	if _, err := client.evaluate(ctx, `document.querySelector('#folderChoose').click(); 'chosen'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`!document.querySelector('#folderDialog').open && document.querySelector('#projectRoot').value === %q ? 'chosen' : ''`, root), "chosen")
	secondRoot := filepath.Join(root, "second-project")
	if err := os.Mkdir(secondRoot, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cmd", ".hermetrix"} {
		if err := os.Mkdir(filepath.Join(secondRoot, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(secondRoot, "README.md"), []byte("project notes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondRoot, ".hermetrix", "oversized.db"), bytes.Repeat([]byte("x"), 2*1024*1024+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.evaluate(ctx, `switchTab('chat'); document.querySelector('#projectChip').click(); document.querySelector('#pickerBrowseExisting').click(); 'browse-from-picker'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#folderDialog').open && !document.querySelector('#folderProjectNameLabel').hidden ? 'picker-folder' : ''`, "picker-folder")
	waitBrowserValue(t, ctx, client, `document.querySelector('#folderTitle').textContent==='เพิ่ม Workspace จากโฟลเดอร์' && document.querySelector('#folderDialog').dataset.mode==='picker' ? 'picker-hierarchy' : ''`, "picker-hierarchy")
	captureFolder("folder-picker-add-mobile.png")
	if _, err := client.evaluate(ctx, fmt.Sprintf(`document.querySelector('#folderAddress').value = %q; document.querySelector('#folderAddressForm').requestSubmit(); 'second-folder'`, secondRoot)); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`document.querySelector('#folderPath').textContent === %q ? 'second-ready' : ''`, secondRoot), "second-ready")
	if _, err := client.evaluate(ctx, `document.querySelector('#folderProjectName').value = 'Mobile second project'; document.querySelector('#folderChoose').click(); 'create-second'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, fmt.Sprintf(`state.currentProject?.root_path === %q && state.currentProject?.name === 'Mobile second project' && state.view === 'code' && document.querySelector('#projectPicker').hidden ? 'second-open' : ''`, secondRoot), "second-open")
	if _, err := client.evaluate(ctx, `openContentPane('files'); 'files'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('.workspace-files .file-group') && Array.from(document.querySelectorAll('.workspace-files .file-group h4, .workspace-files .file-group summary')).map(node => node.textContent.trim()).join('|') === 'โฟลเดอร์|ไฟล์|รายการซ่อน 1' && document.querySelector('.workspace-files .file-group .file-row strong')?.textContent === 'cmd' && !document.querySelector('.workspace-files .file-group-hidden').open ? 'ordered' : ''`, "ordered")
	if _, err := client.evaluate(ctx, `document.querySelector('.file-group-hidden summary').click(); document.querySelector('[data-workbench-file=".hermetrix"]').click(); 'hidden-folder'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.projectPath === '.hermetrix' && document.querySelector('[data-workbench-file=".hermetrix/oversized.db"]') ? 'nested' : ''`, "nested")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-workbench-file=".hermetrix/oversized.db"]').click(); 'large-file'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#toast')?.textContent.includes('ไฟล์นี้ใหญ่เกิน 2 MiB') && !projectCodeTabs().includes('.hermetrix/oversized.db') ? 'large-file-rejected' : ''`, "large-file-rejected")
	if _, err := client.evaluate(ctx, `document.querySelector('#workbenchFileRoot').click(); 'root'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `state.projectPath === '' && document.querySelector('[data-workbench-file="cmd"]') ? 'back-at-root' : ''`, "back-at-root")
	if _, err := client.evaluate(ctx, `openContentPane('review'); 'review'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#reviewOpenSkills') && document.querySelector('#workspacePaneHost').getBoundingClientRect().bottom <= document.querySelector('#zoneMain').getBoundingClientRect().bottom + 1 ? 'review-visible' : ''`, "review-visible")
	if _, err := client.evaluate(ctx, `document.querySelector('#plansViewButton').click(); 'plans'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#plansNewTask') && document.querySelector('#workspacePaneHost').getBoundingClientRect().bottom <= document.querySelector('#zoneMain').getBoundingClientRect().bottom + 1 ? 'plans-visible' : ''`, "plans-visible")
}

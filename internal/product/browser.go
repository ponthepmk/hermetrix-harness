package product

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"hermetrix-harness/internal/identity"
)

const maxBrowserSnapshot = 256 << 10

type browserRuntime struct {
	command  *exec.Cmd
	debugURL string
	profile  string
}

type browserTabRuntime struct {
	mu       sync.Mutex
	tab      BrowserTab
	targetID string
	client   *cdpClient
}

type cdpClient struct {
	mu     sync.Mutex
	conn   *websocket.Conn
	nextID int64
}

type cdpResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (s *Service) OpenBrowserTab(ctx context.Context, input OpenBrowserTabInput) (BrowserTab, error) {
	input.Actor = strings.TrimSpace(input.Actor)
	if input.Actor == "" {
		return BrowserTab{}, fmt.Errorf("browser actor is required")
	}
	validatedURL, err := s.validateBrowserURL(ctx, input.ProjectID, input.URL, input.AllowPrivate)
	if err != nil {
		return BrowserTab{}, err
	}
	browser, err := s.ensureBrowser(ctx)
	if err != nil {
		return BrowserTab{}, err
	}
	target, err := createChromeTarget(ctx, browser.debugURL, validatedURL)
	if err != nil {
		return BrowserTab{}, err
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		return BrowserTab{}, fmt.Errorf("connect Chrome DevTools target: %w", err)
	}
	client := &cdpClient{conn: conn}
	if err := client.call(ctx, "Page.enable", map[string]any{}, nil); err != nil {
		_ = conn.Close()
		return BrowserTab{}, err
	}
	if err := client.call(ctx, "Runtime.enable", map[string]any{}, nil); err != nil {
		_ = conn.Close()
		return BrowserTab{}, err
	}
	if err := client.call(ctx, "Page.navigate", map[string]any{"url": validatedURL}, nil); err != nil {
		_ = conn.Close()
		return BrowserTab{}, err
	}
	_ = waitForPageReady(ctx, client)
	now := time.Now().UTC()
	tab := BrowserTab{ID: identity.New("btab"), ProjectID: input.ProjectID, URL: validatedURL,
		State: "ready", AllowPrivate: input.AllowPrivate, Links: []BrowserLink{}, Elements: []BrowserElement{},
		CreatedAt: now, UpdatedAt: now}
	runtimeTab := &browserTabRuntime{tab: tab, targetID: target.ID, client: client}
	if err := s.refreshBrowserTab(ctx, runtimeTab, true); err != nil {
		_ = conn.Close()
		return BrowserTab{}, err
	}
	tab = runtimeTab.tab
	linksJSON, _ := json.Marshal(tab.Links)
	elementsJSON, _ := json.Marshal(tab.Elements)
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO browser_tabs(id,project_id,url,title,state,allow_private,text_snapshot,
		links_json,elements_json,screenshot_artifact_id,error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		tab.ID, nullIfEmpty(tab.ProjectID), tab.URL, tab.Title, tab.State, tab.AllowPrivate, tab.TextSnapshot,
		string(linksJSON), string(elementsJSON), tab.ScreenshotArtifactID, tab.Error, formatTime(tab.CreatedAt), formatTime(tab.UpdatedAt))
	if err != nil {
		_ = conn.Close()
		return BrowserTab{}, err
	}
	s.mu.Lock()
	s.browserTabs[tab.ID] = runtimeTab
	s.mu.Unlock()
	return tab, nil
}

func (s *Service) BrowserAction(ctx context.Context, id string, input BrowserActionInput) (BrowserTab, error) {
	input.Actor, input.Action = strings.TrimSpace(input.Actor), strings.TrimSpace(input.Action)
	if input.Actor == "" {
		return BrowserTab{}, fmt.Errorf("browser actor is required")
	}
	runtimeTab, err := s.liveBrowserTab(id)
	if err != nil {
		return BrowserTab{}, err
	}
	runtimeTab.mu.Lock()
	defer runtimeTab.mu.Unlock()
	switch input.Action {
	case "navigate":
		validated, err := s.validateBrowserURL(ctx, runtimeTab.tab.ProjectID, input.URL, runtimeTab.tab.AllowPrivate)
		if err != nil {
			return BrowserTab{}, err
		}
		if err := runtimeTab.client.call(ctx, "Page.navigate", map[string]any{"url": validated}, nil); err != nil {
			return BrowserTab{}, err
		}
		_ = waitForPageReady(ctx, runtimeTab.client)
	case "back":
		if _, err := runtimeTab.client.evaluate(ctx, `history.back(); true`); err != nil {
			return BrowserTab{}, err
		}
		_ = waitForPageReady(ctx, runtimeTab.client)
	case "read":
	case "click":
		selector, err := browserSelector(runtimeTab.tab.Elements, input.Ref)
		if err != nil {
			return BrowserTab{}, err
		}
		expression := fmt.Sprintf(`(() => { const el=document.querySelector(%s); if(!el) throw new Error("element ref is stale"); el.click(); return true })()`, jsString(selector))
		if _, err := runtimeTab.client.evaluate(ctx, expression); err != nil {
			return BrowserTab{}, err
		}
		time.Sleep(200 * time.Millisecond)
	case "type":
		if len(input.Text) > 64<<10 {
			return BrowserTab{}, fmt.Errorf("browser text exceeds 64 KiB")
		}
		selector, err := browserSelector(runtimeTab.tab.Elements, input.Ref)
		if err != nil {
			return BrowserTab{}, err
		}
		expression := fmt.Sprintf(`(() => { const el=document.querySelector(%s); if(!el) throw new Error("element ref is stale"); el.focus(); el.value=%s; el.dispatchEvent(new Event("input",{bubbles:true})); el.dispatchEvent(new Event("change",{bubbles:true})); return true })()`, jsString(selector), jsString(input.Text))
		if _, err := runtimeTab.client.evaluate(ctx, expression); err != nil {
			return BrowserTab{}, err
		}
	case "capture":
	case "close":
		return s.closeBrowserTabLocked(ctx, runtimeTab)
	default:
		return BrowserTab{}, fmt.Errorf("unsupported browser action %q", input.Action)
	}
	if err := s.refreshBrowserTab(ctx, runtimeTab, true); err != nil {
		return BrowserTab{}, err
	}
	if err := s.persistBrowserTab(ctx, runtimeTab.tab); err != nil {
		return BrowserTab{}, err
	}
	return runtimeTab.tab, nil
}

func (s *Service) ListBrowserTabs(ctx context.Context, limit int) ([]BrowserTab, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.store.DB.QueryContext(ctx, browserTabSelect+` ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BrowserTab
	for rows.Next() {
		item, err := scanBrowserTab(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) GetBrowserTab(ctx context.Context, id string) (BrowserTab, error) {
	return scanBrowserTab(s.store.DB.QueryRowContext(ctx, browserTabSelect+` WHERE id=?`, id))
}

func (s *Service) refreshBrowserTab(ctx context.Context, runtimeTab *browserTabRuntime, capture bool) error {
	expression := `(() => {
const clean=s=>(s||"").replace(/\s+/g," ").trim();
const nodes=[...document.querySelectorAll('a,button,input,textarea,select,[role="button"],[contenteditable="true"]')].slice(0,200);
const elements=nodes.map((el,i)=>{const ref=i+1;el.setAttribute('data-hermetrix-ref',String(ref));return {ref,tag:el.tagName.toLowerCase(),role:el.getAttribute('role')||'',text:clean(el.innerText||el.value||el.getAttribute('aria-label')),placeholder:el.getAttribute('placeholder')||'',selector:'[data-hermetrix-ref="'+ref+'"]'}});
const links=[...document.querySelectorAll('a[href]')].slice(0,200).map(a=>({text:clean(a.innerText||a.getAttribute('aria-label')),url:a.href}));
return JSON.stringify({title:document.title||'',url:location.href,text:clean(document.body?.innerText||'').slice(0,262144),links,elements});
})()`
	value, err := runtimeTab.client.evaluate(ctx, expression)
	if err != nil {
		return err
	}
	var snapshot struct {
		Title    string           `json:"title"`
		URL      string           `json:"url"`
		Text     string           `json:"text"`
		Links    []BrowserLink    `json:"links"`
		Elements []BrowserElement `json:"elements"`
	}
	if err := json.Unmarshal([]byte(value), &snapshot); err != nil {
		return fmt.Errorf("decode browser snapshot: %w", err)
	}
	if len(snapshot.Text) > maxBrowserSnapshot {
		snapshot.Text = snapshot.Text[:maxBrowserSnapshot]
	}
	runtimeTab.tab.Title, runtimeTab.tab.URL, runtimeTab.tab.TextSnapshot = snapshot.Title, snapshot.URL, snapshot.Text
	runtimeTab.tab.Links, runtimeTab.tab.Elements = snapshot.Links, snapshot.Elements
	runtimeTab.tab.State, runtimeTab.tab.Error, runtimeTab.tab.UpdatedAt = "ready", "", time.Now().UTC()
	if capture {
		var result struct {
			Data string `json:"data"`
		}
		if err := runtimeTab.client.call(ctx, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": false}, &result); err != nil {
			return err
		}
		image, err := base64.StdEncoding.DecodeString(result.Data)
		if err != nil {
			return err
		}
		artifact, err := s.CreateArtifact(ctx, ArtifactInput{ProjectID: runtimeTab.tab.ProjectID,
			Name: "browser-" + runtimeTab.tab.ID + ".png", Kind: "browser_screenshot", MIMEType: "image/png",
			Content: string(image), Metadata: map[string]any{"browser_tab_id": runtimeTab.tab.ID, "url": runtimeTab.tab.URL,
				"untrusted_page_content": true}})
		if err != nil {
			return err
		}
		runtimeTab.tab.ScreenshotArtifactID = artifact.ID
	}
	return nil
}

func (s *Service) persistBrowserTab(ctx context.Context, tab BrowserTab) error {
	links, _ := json.Marshal(tab.Links)
	elements, _ := json.Marshal(tab.Elements)
	_, err := s.store.DB.ExecContext(ctx, `UPDATE browser_tabs SET url=?,title=?,state=?,text_snapshot=?,links_json=?,
		elements_json=?,screenshot_artifact_id=?,error=?,updated_at=? WHERE id=?`, tab.URL, tab.Title, tab.State,
		tab.TextSnapshot, string(links), string(elements), tab.ScreenshotArtifactID, tab.Error, formatTime(tab.UpdatedAt), tab.ID)
	return err
}

func (s *Service) closeBrowserTabLocked(ctx context.Context, runtimeTab *browserTabRuntime) (BrowserTab, error) {
	_ = runtimeTab.client.call(ctx, "Target.closeTarget", map[string]any{"targetId": runtimeTab.targetID}, nil)
	_ = runtimeTab.client.conn.Close()
	runtimeTab.tab.State, runtimeTab.tab.UpdatedAt = "closed", time.Now().UTC()
	if err := s.persistBrowserTab(ctx, runtimeTab.tab); err != nil {
		return BrowserTab{}, err
	}
	s.mu.Lock()
	delete(s.browserTabs, runtimeTab.tab.ID)
	s.mu.Unlock()
	return runtimeTab.tab, nil
}

func (s *Service) liveBrowserTab(id string) (*browserTabRuntime, error) {
	s.mu.Lock()
	tab := s.browserTabs[id]
	s.mu.Unlock()
	if tab == nil {
		return nil, fmt.Errorf("managed browser tab is not live")
	}
	return tab, nil
}

func (s *Service) ensureBrowser(ctx context.Context) (*browserRuntime, error) {
	s.mu.Lock()
	if s.browser != nil {
		browser := s.browser
		s.mu.Unlock()
		return browser, nil
	}
	s.mu.Unlock()
	executable, err := chromeExecutable()
	if err != nil {
		return nil, err
	}
	profile, err := os.MkdirTemp(s.store.Root, "chrome-profile-")
	if err != nil {
		return nil, err
	}
	command := exec.Command(executable, "--headless=new", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0",
		"--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check", "--disable-background-networking",
		"--disable-default-apps", "--disable-sync", "--metrics-recording-only", "about:blank")
	stderr, err := command.StderrPipe()
	if err != nil {
		_ = os.RemoveAll(profile)
		return nil, err
	}
	if err := command.Start(); err != nil {
		_ = os.RemoveAll(profile)
		return nil, err
	}
	devToolsURL := make(chan string, 1)
	stderrDone := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		lines := make([]string, 0, 12)
		for scanner.Scan() {
			line := scanner.Text()
			if len(lines) == cap(lines) {
				copy(lines, lines[1:])
				lines = lines[:len(lines)-1]
			}
			lines = append(lines, line)
			if index := strings.Index(line, "DevTools listening on "); index >= 0 {
				select {
				case devToolsURL <- strings.TrimSpace(line[index+len("DevTools listening on "):]):
				default:
				}
			}
		}
		stderrDone <- strings.Join(lines, "\n")
	}()
	processDone := make(chan error, 1)
	go func() { processDone <- command.Wait() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	debugURL := ""
	for debugURL == "" {
		select {
		case websocketURL := <-devToolsURL:
			parsed, parseErr := url.Parse(websocketURL)
			if parseErr == nil && parsed.Host != "" {
				debugURL = "http://" + parsed.Host
			}
		case <-ticker.C:
			debugURL = devToolsURLFromProfile(profile)
		case processErr := <-processDone:
			stderrText := <-stderrDone
			_ = os.RemoveAll(profile)
			if stderrText != "" {
				return nil, fmt.Errorf("Chrome exited before DevTools became ready: %v: %s", processErr, stderrText)
			}
			return nil, fmt.Errorf("Chrome exited before DevTools became ready: %v", processErr)
		case <-ctx.Done():
			_ = command.Process.Kill()
			_ = os.RemoveAll(profile)
			return nil, ctx.Err()
		case <-timeout.C:
			_ = command.Process.Kill()
			_ = os.RemoveAll(profile)
			return nil, fmt.Errorf("Chrome DevTools did not become ready within 10 seconds")
		}
	}
	browser := &browserRuntime{command: command, debugURL: debugURL, profile: profile}
	s.mu.Lock()
	if s.browser == nil {
		s.browser = browser
		s.mu.Unlock()
		return browser, nil
	}
	existing := s.browser
	s.mu.Unlock()
	_ = command.Process.Kill()
	_ = os.RemoveAll(profile)
	return existing, nil
}

func devToolsURLFromProfile(profile string) string {
	data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return ""
	}
	port, err := net.LookupPort("tcp", strings.TrimSpace(lines[0]))
	if err != nil || port <= 0 {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func (s *Service) closeBrowser() {
	s.mu.Lock()
	browser := s.browser
	tabs := make([]*browserTabRuntime, 0, len(s.browserTabs))
	for _, tab := range s.browserTabs {
		tabs = append(tabs, tab)
	}
	s.browser, s.browserTabs = nil, map[string]*browserTabRuntime{}
	s.mu.Unlock()
	for _, tab := range tabs {
		_ = tab.client.conn.Close()
	}
	if browser != nil {
		if browser.command.Process != nil {
			_ = browser.command.Process.Kill()
		}
		_ = os.RemoveAll(browser.profile)
	}
}

func (s *Service) validateBrowserURL(ctx context.Context, projectID, raw string, allowPrivate bool) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" {
		return "", fmt.Errorf("browser URL must be absolute")
	}
	switch parsed.Scheme {
	case "file":
		if projectID == "" {
			return "", fmt.Errorf("file URLs require a bound project")
		}
		root, err := s.RequireRoot(ctx, projectID)
		if err != nil {
			return "", err
		}
		path, err := filepath.EvalSymlinks(filepath.FromSlash(parsed.Path))
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("file URL escapes the bound project")
		}
		return parsed.String(), nil
	case "http", "https":
		if parsed.User != nil {
			return "", fmt.Errorf("browser URLs containing credentials are refused")
		}
		private, err := privateHost(ctx, parsed.Hostname())
		if err != nil {
			return "", err
		}
		if private && !allowPrivate {
			return "", fmt.Errorf("private or local browser URLs require explicit allow_private")
		}
		return parsed.String(), nil
	default:
		return "", fmt.Errorf("browser supports only http, https and project-bound file URLs")
	}
}

func privateHost(ctx context.Context, host string) (bool, error) {
	if strings.EqualFold(host, "localhost") {
		return true, nil
	}
	if address := net.ParseIP(host); address != nil {
		return address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsUnspecified(), nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return false, fmt.Errorf("resolve browser host: %w", err)
	}
	for _, address := range addresses {
		if address.IP.IsPrivate() || address.IP.IsLoopback() || address.IP.IsLinkLocalUnicast() || address.IP.IsUnspecified() {
			return true, nil
		}
	}
	return false, nil
}

func chromeExecutable() (string, error) {
	candidates := []string{"google-chrome", "chrome", "chromium", "chromium-browser"}
	if runtime.GOOS == "darwin" {
		candidates = append([]string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium"}, candidates...)
	}
	if runtime.GOOS == "windows" {
		candidates = append([]string{filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe")}, candidates...)
	}
	for _, candidate := range candidates {
		if filepath.IsAbs(candidate) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("Chrome or Chromium is required for the managed browser")
}

type chromeTarget struct {
	ID                   string `json:"id"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func createChromeTarget(ctx context.Context, baseURL, rawURL string) (chromeTarget, error) {
	request, _ := http.NewRequestWithContext(ctx, http.MethodPut, baseURL+"/json/new?"+url.QueryEscape(rawURL), nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return chromeTarget{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return chromeTarget{}, fmt.Errorf("create Chrome target returned %s", response.Status)
	}
	var target chromeTarget
	if err := json.NewDecoder(response.Body).Decode(&target); err != nil {
		return chromeTarget{}, err
	}
	if target.ID == "" || target.WebSocketDebuggerURL == "" {
		return chromeTarget{}, fmt.Errorf("Chrome target response was incomplete")
	}
	return target, nil
}

func (c *cdpClient) call(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	id := c.nextID
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetWriteDeadline(deadline)
		_ = c.conn.SetReadDeadline(deadline)
	} else {
		_ = c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
		_ = c.conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	}
	if err := c.conn.WriteJSON(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return err
		}
		var response cdpResponse
		if json.Unmarshal(data, &response) != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return fmt.Errorf("Chrome DevTools %s: %s", method, response.Error.Message)
		}
		if result != nil && len(response.Result) > 0 {
			return json.Unmarshal(response.Result, result)
		}
		return nil
	}
}

func (c *cdpClient) evaluate(ctx context.Context, expression string) (string, error) {
	var response struct {
		Result struct {
			Type        string `json:"type"`
			Value       any    `json:"value"`
			Description string `json:"description"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := c.call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true,
		"awaitPromise": true}, &response); err != nil {
		return "", err
	}
	if len(response.ExceptionDetails) > 0 && string(response.ExceptionDetails) != "null" {
		return "", fmt.Errorf("browser script failed: %s", response.Result.Description)
	}
	switch value := response.Result.Value.(type) {
	case string:
		return value, nil
	case nil:
		return "", nil
	default:
		encoded, _ := json.Marshal(value)
		return string(encoded), nil
	}
}

func waitForPageReady(ctx context.Context, client *cdpClient) error {
	for attempt := 0; attempt < 50; attempt++ {
		value, err := client.evaluate(ctx, `document.readyState`)
		if err == nil && (value == "complete" || value == "interactive") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("page did not become ready")
}

func browserSelector(elements []BrowserElement, ref int) (string, error) {
	if ref <= 0 {
		return "", fmt.Errorf("browser element ref is required")
	}
	for _, element := range elements {
		if element.Ref == ref {
			return element.Selector, nil
		}
	}
	return "", fmt.Errorf("browser element ref is stale or unknown")
}

func jsString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

const browserTabSelect = `SELECT id,COALESCE(project_id,''),url,title,state,allow_private,text_snapshot,links_json,
	elements_json,screenshot_artifact_id,error,created_at,updated_at FROM browser_tabs`

type browserTabScanner interface{ Scan(...any) error }

func scanBrowserTab(row browserTabScanner) (BrowserTab, error) {
	var item BrowserTab
	var linksJSON, elementsJSON, created, updated string
	if err := row.Scan(&item.ID, &item.ProjectID, &item.URL, &item.Title, &item.State, &item.AllowPrivate,
		&item.TextSnapshot, &linksJSON, &elementsJSON, &item.ScreenshotArtifactID, &item.Error, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BrowserTab{}, sql.ErrNoRows
		}
		return BrowserTab{}, err
	}
	_ = json.Unmarshal([]byte(linksJSON), &item.Links)
	_ = json.Unmarshal([]byte(elementsJSON), &item.Elements)
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

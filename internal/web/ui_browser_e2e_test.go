package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
		return "", fmt.Errorf("browser evaluation failed")
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

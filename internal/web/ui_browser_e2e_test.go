package web

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
	profile := filepath.Join(t.TempDir(), "chrome-profile")
	command := exec.CommandContext(ctx, chrome,
		"--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
		"--disable-background-networking", "--no-first-run", "--no-default-browser-check",
		"--user-data-dir="+profile, "--timeout=5000", "--dump-dom", server.URL)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("Chrome UI E2E failed: %v\n%s", err, stderr.String())
	}
	dom := stdout.String()
	for _, marker := range []string{`class="picker-card"`, `data-open-project=`, `E2E &lt;project&gt;`} {
		if !strings.Contains(dom, marker) {
			t.Fatalf("hydrated DOM is missing %q; scripts did not complete\n%s", marker, abbreviateDOM(dom))
		}
	}
	if strings.Contains(dom, `<strong>E2E <project>`) {
		t.Fatal("project name entered the hydrated DOM as markup")
	}
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

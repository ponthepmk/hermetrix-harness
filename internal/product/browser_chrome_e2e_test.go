package product

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// TestChromeBlocksAPrivateSubresourceBeforeItReachesTheServer proves the CDP
// guard with a real browser process. A project-bound file is a safe initial
// page that can still try to reach loopback; AllowPrivate=false must stop the
// request at Fetch.requestPaused, before the target server observes a hit.
func TestChromeBlocksAPrivateSubresourceBeforeItReachesTheServer(t *testing.T) {
	if os.Getenv("HERMETRIX_RUN_BROWSER_NETWORK_E2E") != "1" {
		t.Skip("set HERMETRIX_RUN_BROWSER_NETWORK_E2E=1 in the browser E2E job")
	}
	if _, err := chromeExecutable(); err != nil {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = fmt.Fprint(w, "private response")
	}))
	defer target.Close()

	service, _, _ := testProductService(t)
	root := t.TempDir()
	project, err := service.SaveProject(context.Background(), ProjectInput{Name: "browser network e2e", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(root, "private-subresource.html")
	if err := os.WriteFile(page, []byte(`<html><body>safe page<img src="`+target.URL+`/secret"></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	pageURL := (&url.URL{Scheme: "file", Path: page}).String()
	_, err = service.OpenBrowserTab(context.Background(), OpenBrowserTabInput{
		ProjectID: project.ID, URL: pageURL, Actor: "e2e", AllowPrivate: false,
	})
	if err == nil || !strings.Contains(err.Error(), "blocked browser request") {
		t.Fatalf("private subresource did not produce a policy error: %v", err)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("private server received %d requests before the browser guard blocked it", got)
	}
}

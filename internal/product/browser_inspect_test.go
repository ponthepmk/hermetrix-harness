package product

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInspectionURLStripsCredentialsAndQuery(t *testing.T) {
	got := inspectionURL("https://user:pass@example.test/api/items?token=secret#section")
	if got != "https://example.test/api/items" {
		t.Fatalf("inspectionURL = %q", got)
	}
	for _, raw := range []string{"javascript:alert(1)", "data:text/plain,secret", "/relative", "file:///etc/passwd"} {
		if got := inspectionURL(raw); got != "" {
			t.Errorf("inspectionURL(%q) = %q, want empty", raw, got)
		}
	}
}

func TestChromePassiveInspectionReportsLoadedAPIAndClientSource(t *testing.T) {
	if os.Getenv("HERMETRIX_RUN_BROWSER_NETWORK_E2E") != "1" {
		t.Skip("set HERMETRIX_RUN_BROWSER_NETWORK_E2E=1 for a real Chrome inspection")
	}
	if _, err := chromeExecutable(); err != nil {
		t.Skip(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Security-Policy-Report-Only", "default-src 'self'")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			fmt.Fprint(w, `<html><head><script src="/client.js?token=script-secret"></script></head><body><a href="/settings?token=route-secret">Settings</a><form action="/login?token=form-secret" method="post"><input value="input-secret"></form><script>console.error('fixture issue');fetch('/api/items')</script></body></html>`)
		case "/client.js":
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, `window.clientLoaded=true;window.clientAPI="/api/items";`)
		case "/api/items":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"items":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, _, _ := testProductService(t)
	tab, err := service.OpenBrowserTab(context.Background(), OpenBrowserTabInput{URL: server.URL, AllowPrivate: true, Actor: "inspection-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer service.BrowserAction(context.Background(), tab.ID, BrowserActionInput{Action: "close", Actor: "inspection-test"})
	time.Sleep(200 * time.Millisecond)
	result, err := service.BrowserAction(context.Background(), tab.ID, BrowserActionInput{Action: "inspect", Actor: "inspection-test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Inspection == nil {
		t.Fatal("inspection missing")
	}
	if len(result.Inspection.PageErrors) == 0 || !strings.Contains(strings.Join(result.Inspection.PageErrors, " "), "fixture issue") {
		t.Fatalf("console error missing: %+v", result.Inspection.PageErrors)
	}
	encoded := fmt.Sprintf("%+v", result.Inspection)
	for _, want := range []string{"/api/items", "/client.js", "/login", "/settings"} {
		if !strings.Contains(encoded, want) {
			t.Errorf("inspection missing %q: %s", want, encoded)
		}
	}
	for _, secret := range []string{"script-secret", "form-secret", "route-secret", "input-secret"} {
		if strings.Contains(encoded, secret) {
			t.Errorf("inspection exposed %q", secret)
		}
	}
	audited, err := service.BrowserAction(context.Background(), tab.ID, BrowserActionInput{Action: "audit", Actor: "inspection-test"})
	if err != nil {
		t.Fatal(err)
	}
	if audited.Inspection == nil || audited.Inspection.Security == nil {
		t.Fatal("security audit missing")
	}
	foundDocument, foundAPI, foundReportOnly := false, false, false
	for _, item := range audited.Inspection.Traffic {
		if item.Type == "Document" && item.Status == 200 && item.SecurityHeaders["content-security-policy-report-only"] != "" {
			foundDocument = true
		}
		if strings.HasSuffix(item.URL, "/api/items") && item.Method == "GET" && item.Status == 200 {
			foundAPI = true
		}
	}
	for _, finding := range audited.Inspection.Security.Findings {
		if finding.ID == "csp_report_only" {
			foundReportOnly = true
		}
	}
	if !foundDocument || !foundAPI || !foundReportOnly {
		t.Fatalf("audit evidence missing: traffic=%+v findings=%+v", audited.Inspection.Traffic, audited.Inspection.Security.Findings)
	}
	var scriptURL string
	for _, script := range audited.Inspection.Scripts {
		if strings.HasSuffix(script.URL, "/client.js") {
			scriptURL = script.URL
		}
	}
	if scriptURL == "" {
		t.Fatal("external script URL missing")
	}
	source, err := service.BrowserAction(context.Background(), tab.ID, BrowserActionInput{Action: "source", URL: scriptURL, Actor: "inspection-test"})
	if err != nil {
		t.Fatal(err)
	}
	if source.Inspection == nil || source.Inspection.ClientSource == nil ||
		!strings.Contains(source.Inspection.ClientSource.Excerpt, "clientLoaded") ||
		len(source.Inspection.ClientSource.PathHints) != 1 || source.Inspection.ClientSource.PathHints[0] != "/api/items" {
		t.Fatalf("observed source missing code/path hint: %+v", source.Inspection)
	}
}

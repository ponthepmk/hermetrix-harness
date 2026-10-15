package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestDiagramViewerIsTheOnlyFramableSandbox(t *testing.T) {
	server := testHTTPServer(t)
	for _, path := range []string{"/", "/diagram.html", "/runtime.js"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		policy := response.Header.Get("Content-Security-Policy")
		if path == "/diagram.html" {
			if response.Header.Get("X-Frame-Options") != "SAMEORIGIN" {
				t.Fatal("diagram cannot be framed by own UI")
			}
			for _, guard := range []string{"sandbox allow-scripts", "connect-src 'none'", "form-action 'none'", "frame-ancestors 'self'"} {
				if !strings.Contains(policy, guard) {
					t.Fatalf("diagram missing %s", guard)
				}
			}
			if strings.Contains(policy, "allow-same-origin") {
				t.Fatal("diagram can access parent origin")
			}
		} else if response.Header.Get("X-Frame-Options") != "DENY" || strings.Contains(policy, "unsafe-inline") {
			t.Fatalf("general policy widened for %s", path)
		}
	}
}

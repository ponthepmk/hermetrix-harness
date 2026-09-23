package web

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestAppEditorStyleNonceIsFreshAndBoundToResponse(t *testing.T) {
	server := testHTTPServer(t)
	pattern := regexp.MustCompile(`<meta name="hermetrix-style-nonce" content="([A-Za-z0-9+/]+)">`)
	previousNonce := ""
	for _, path := range []string{"/", "/", "/workspace/deep-link"} {
		request, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("If-Modified-Since", "Wed, 01 Jan 2031 00:00:00 GMT")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		match := pattern.FindSubmatch(body)
		if response.StatusCode != http.StatusOK || len(match) != 2 {
			t.Fatalf("%s did not serve an independently authorized app page: status=%d", path, response.StatusCode)
		}
		nonce := string(match[1])
		policy := response.Header.Get("Content-Security-Policy")
		if len(nonce) < 32 || nonce == previousNonce || !strings.Contains(policy, "style-src 'self' 'nonce-"+nonce+"'") {
			t.Fatalf("HTML style nonce was reused or not bound to its response policy: %q", policy)
		}
		if !strings.Contains(policy, "; script-src 'self';") || strings.Contains(policy, "unsafe-inline") {
			t.Fatalf("authorizing editor styles weakened script policy: %q", policy)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("nonce-bearing app page can be cached independently of its policy")
		}
		previousNonce = nonce
	}
}

func TestEditorStyleNonceDoesNotAuthorizeStoredArtifactContent(t *testing.T) {
	server := testHTTPServer(t)
	created := requestJSON(t, server.URL+"/api/artifacts", http.MethodPost, map[string]any{
		"name": "nonce-check.html", "kind": "report", "mime_type": "text/html",
		"content": `<style>body { display:none }</style><script>alert(1)</script>`,
	}, http.StatusCreated)
	var artifact struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created, &artifact); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/artifacts/" + artifact.ID + "/content", "/app.js"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		policy := response.Header.Get("Content-Security-Policy")
		if response.StatusCode != http.StatusOK || policy != contentSecurityPolicy("") || strings.Contains(policy, "nonce-") {
			t.Fatalf("%s inherited app-only style authority: status=%d policy=%q", path, response.StatusCode, policy)
		}
	}
}

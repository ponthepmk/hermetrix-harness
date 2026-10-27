package web

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthenticatedLoopbackProxyBoundariesAndLogin(t *testing.T) {
	const host = "device.example.ts.net:18810"
	const token = "test-token-at-least-thirty-two-characters"
	called := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if r.Host == host {
			t.Error("external host leaked to backend")
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Forwarded") != "" {
			t.Error("gateway credentials or forwarded headers leaked to backend")
		}
		if origin := r.Header.Get("Origin"); origin != "" && !strings.HasPrefix(origin, "http://127.0.0.1:") {
			t.Errorf("untranslated origin: %s", origin)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()
	handler, err := NewAuthenticatedLoopbackProxy(upstream.URL, host, token, "local-user")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, origin, bearer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://"+host+path, strings.NewReader(`{"text":"hello"}`))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if got := request("GET", "/api/projects", "", ""); got.Code != 401 || called != 0 {
		t.Fatalf("unauthenticated request: %d calls=%d", got.Code, called)
	}
	if got := request("POST", "/api/sessions", "https://attacker.example", token); got.Code != 403 || called != 0 {
		t.Fatalf("cross-origin request: %d calls=%d", got.Code, called)
	}
	r := httptest.NewRequest("GET", "https://attacker.example/api/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 421 || called != 0 {
		t.Fatalf("untrusted host: %d calls=%d", w.Code, called)
	}
	if got := request("POST", "/api/sessions", "https://"+host, token); got.Code != 200 || called != 1 {
		t.Fatalf("authenticated mutation: %d calls=%d", got.Code, called)
	}
	r = httptest.NewRequest("POST", "https://"+host+"/api/auth/session", strings.NewReader(`{"token":"`+token+`"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://"+host)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 || !w.Result().Cookies()[0].Secure {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "https://"+host+"/api/projects", nil)
	r.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || called != 2 {
		t.Fatalf("cookie access: %d calls=%d", w.Code, called)
	}
}

func TestAuthenticatedLoopbackProxyStreamsAndPropagatesDisconnect(t *testing.T) {
	disconnected := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: started\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(disconnected)
	}))
	defer upstream.Close()
	token := strings.Repeat("x", 32)
	handler, err := NewAuthenticatedLoopbackProxy(upstream.URL, "device.example.ts.net:18810", token, "local-user")
	if err != nil {
		t.Fatal(err)
	}
	gateway := httptest.NewTLSServer(handler)
	defer gateway.Close()
	request, _ := http.NewRequest("GET", gateway.URL+"/api/stream", nil)
	request.Host = "device.example.ts.net:18810"
	request.Header.Set("Authorization", "Bearer "+token)
	client := gateway.Client()
	client.Timeout = 3 * time.Second
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || line != "data: started\n" {
		t.Fatalf("live event not flushed: %q %v", line, err)
	}
	response.Body.Close()
	select {
	case <-disconnected:
	case <-time.After(2 * time.Second):
		t.Fatal("client disconnect did not reach backend")
	}
}

func TestAuthenticatedLoopbackProxyRejectsRemoteUpstream(t *testing.T) {
	for _, upstream := range []string{"http://example.com:18810", "http://localhost:18810", "https://127.0.0.1:18810", "http://127.0.0.1:18810/path"} {
		if _, err := NewAuthenticatedLoopbackProxy(upstream, "device.example.ts.net:18810", strings.Repeat("x", 32), "local-user"); err == nil {
			t.Errorf("accepted %s", upstream)
		}
	}
}

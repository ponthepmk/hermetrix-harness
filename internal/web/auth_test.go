package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthenticationProtectsAPIAndBindsAuditActor(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	auth := newAuthenticator(token, "local-user", false)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/session", auth.session)
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		if PrincipalFromContext(r.Context()) != "local-user" {
			t.Errorf("principal = %q", PrincipalFromContext(r.Context()))
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	server := httptest.NewServer(auth.middleware(mux))
	defer server.Close()

	assertStatus(t, http.MethodGet, server.URL+"/", "", "", http.StatusOK)
	assertStatus(t, http.MethodGet, server.URL+"/api/health", "", "", http.StatusOK)
	assertStatus(t, http.MethodGet, server.URL+"/api/projects", "", "", http.StatusUnauthorized)
	assertStatus(t, http.MethodGet, server.URL+"/api/projects", "", "Bearer "+token, http.StatusOK)

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/session",
		strings.NewReader(`{"token":"`+token+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(response.Cookies()) != 1 {
		t.Fatalf("login status=%d cookies=%v", response.StatusCode, response.Cookies())
	}
	cookie := response.Cookies()[0]
	if strings.Contains(cookie.Value, token) || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unsafe session cookie: %+v", cookie)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/projects", strings.NewReader(`{"actor":"someone-else"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("mismatched actor status = %d", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/projects", strings.NewReader(`{"actor":"local-user"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("bound actor status = %d", response.StatusCode)
	}
}

func TestInvalidAuthenticationTokenNeverSetsCookie(t *testing.T) {
	auth := newAuthenticator("0123456789abcdef0123456789abcdef", "local-user", true)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/session", strings.NewReader(`{"token":"wrong"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	auth.session(recorder, request)
	if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("Set-Cookie") != "" {
		t.Fatalf("invalid login status=%d cookie=%q", recorder.Code, recorder.Header().Get("Set-Cookie"))
	}
}

func TestAuthenticationCookieIsSignedAndExpires(t *testing.T) {
	auth := newAuthenticator("0123456789abcdef0123456789abcdef", "custom-principal", true)
	now := time.Unix(1_800_000_000, 0)
	valid := auth.cookieValue(now.Add(time.Hour))
	if !auth.validCookie(valid, now) {
		t.Fatal("fresh signed cookie was rejected")
	}
	if auth.validCookie(valid+"0", now) {
		t.Fatal("tampered signed cookie was accepted")
	}
	if auth.validCookie(auth.cookieValue(now.Add(-time.Second)), now) {
		t.Fatal("expired signed cookie was accepted")
	}
}

func assertStatus(t *testing.T, method, target, body, authorization string, want int) {
	t.Helper()
	request, _ := http.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		var payload map[string]any
		_ = json.NewDecoder(response.Body).Decode(&payload)
		t.Fatalf("%s %s status=%d want=%d body=%v", method, target, response.StatusCode, want, payload)
	}
}

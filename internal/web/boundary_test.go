package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestBoundaryRejectsUntrustedHostBeforeHandler(t *testing.T) {
	boundary, err := newRequestBoundary("127.0.0.1:7331", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	handler := boundary.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}), nil)

	request := httptest.NewRequest(http.MethodGet, "http://attacker.example/api/health", nil)
	request.Host = "attacker.example:7331"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMisdirectedRequest || called {
		t.Fatalf("status=%d called=%v body=%s", recorder.Code, called, recorder.Body.String())
	}
}

func TestRequestBoundaryRequiresSameOriginForBrowserMutations(t *testing.T) {
	boundary, err := newRequestBoundary("127.0.0.1:7331", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := boundary.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), nil)

	tests := []struct {
		name      string
		origin    string
		fetchSite string
		want      int
	}{
		{name: "same origin", origin: "http://localhost:7331", fetchSite: "same-origin", want: http.StatusNoContent},
		{name: "cross origin", origin: "https://attacker.example", fetchSite: "cross-site", want: http.StatusForbidden},
		{name: "cross site metadata", fetchSite: "cross-site", want: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://localhost:7331/api/tasks", strings.NewReader(`{}`))
			request.Host = "localhost:7331"
			request.Header.Set("Content-Type", "application/json")
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.fetchSite != "" {
				request.Header.Set("Sec-Fetch-Site", test.fetchSite)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestRequestBoundaryDistinguishesBearerAndCookieClientsWithoutOrigin(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	auth := newAuthenticator(token, "local-user", false)
	boundary, err := newRequestBoundary("127.0.0.1:7331", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := boundary.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), auth)

	bearer := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7331/api/tasks", strings.NewReader(`{}`))
	bearer.Header.Set("Authorization", "Bearer "+token)
	bearer.Header.Set("Content-Type", "application/json")
	bearerRecorder := httptest.NewRecorder()
	handler.ServeHTTP(bearerRecorder, bearer)
	if bearerRecorder.Code != http.StatusNoContent {
		t.Fatalf("bearer status=%d body=%s", bearerRecorder.Code, bearerRecorder.Body.String())
	}

	cookie := &http.Cookie{Name: authCookieName, Value: auth.cookieValue(time.Now().Add(time.Hour))}
	missingMetadata := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7331/api/tasks", strings.NewReader(`{}`))
	missingMetadata.Header.Set("Content-Type", "application/json")
	missingMetadata.AddCookie(cookie)
	missingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(missingRecorder, missingMetadata)
	if missingRecorder.Code != http.StatusForbidden {
		t.Fatalf("cookie without fetch metadata status=%d body=%s", missingRecorder.Code, missingRecorder.Body.String())
	}

	sameOrigin := missingMetadata.Clone(missingMetadata.Context())
	sameOrigin.Body = http.NoBody
	sameOrigin.Header.Set("Sec-Fetch-Site", "same-origin")
	sameOriginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(sameOriginRecorder, sameOrigin)
	if sameOriginRecorder.Code != http.StatusNoContent {
		t.Fatalf("same-origin cookie status=%d body=%s", sameOriginRecorder.Code, sameOriginRecorder.Body.String())
	}
}

func TestDecodeJSONRequiresJSONMediaTypeAndExactlyOneDocument(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		want        int
		wantOK      bool
	}{
		{name: "application json", contentType: "application/json; charset=utf-8", body: `{"value":"ok"}`, want: http.StatusOK, wantOK: true},
		{name: "structured suffix", contentType: "application/problem+json", body: `{"value":"ok"}`, want: http.StatusOK, wantOK: true},
		{name: "missing media type", body: `{"value":"ok"}`, want: http.StatusUnsupportedMediaType},
		{name: "plain text", contentType: "text/plain", body: `{"value":"ok"}`, want: http.StatusUnsupportedMediaType},
		{name: "second document", contentType: "application/json", body: `{"value":"one"} {"value":"two"}`, want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/example", strings.NewReader(test.body))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			recorder := httptest.NewRecorder()
			var value struct {
				Value string `json:"value"`
			}
			ok := decodeJSON(recorder, request, &value)
			if ok != test.wantOK || recorder.Code != test.want {
				t.Fatalf("ok=%v status=%d want_ok=%v want_status=%d body=%s", ok, recorder.Code, test.wantOK, test.want, recorder.Body.String())
			}
		})
	}
}

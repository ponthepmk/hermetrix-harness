package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hermetrix-harness/internal/secrets"
	"hermetrix-harness/internal/store"
)

func TestSocialConnectionVerifiesIdentityAndKeepsTokenOutOfDatabase(t *testing.T) {
	const token = "secret-facebook-page-token"
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me" || r.URL.Query().Get("fields") != "id,name,category" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("unexpected Page verification request: %s %s", r.Method, r.URL)
			w.WriteHeader(403)
			return
		}
		_, _ = io.WriteString(w, `{"id":"page-123","name":"Sample Page","category":"Community"}`)
	}))
	defer platform.Close()
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	vault, err := secrets.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil, nil, nil, nil, nil, nil, dataStore, slog.New(slog.NewTextHandler(io.Discard, nil))).WithSocialVault(vault)
	s.socialFacebookBase = platform.URL
	app := httptest.NewServer(s.Handler())
	defer app.Close()
	call := func(method, path string, body any) (int, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		}
		req, err := http.NewRequest(method, app.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		payload, _ := io.ReadAll(resp.Body)
		if bytes.Contains(payload, []byte(token)) {
			t.Fatal("access token leaked in API response")
		}
		return resp.StatusCode, payload
	}
	status, payload := call("POST", "/api/social/connections", map[string]string{"platform": "facebook", "token": token})
	if status != 200 {
		t.Fatalf("connect status=%d body=%s", status, payload)
	}
	var connected socialConnection
	if err := json.Unmarshal(payload, &connected); err != nil {
		t.Fatal(err)
	}
	if connected.AccountID != "page-123" || connected.Capability != "page_identity" || !connected.CredentialStored {
		t.Fatalf("unexpected connection: %+v", connected)
	}
	if status, payload = call("GET", "/api/social/connections", nil); status != 200 || !bytes.Contains(payload, []byte("Sample Page")) {
		t.Fatalf("list status=%d body=%s", status, payload)
	}
	if status, payload = call("POST", "/api/social/connections/"+connected.ID+"/check", nil); status != 200 {
		t.Fatalf("check status=%d body=%s", status, payload)
	}
	var stored string
	if err := dataStore.DB.QueryRow(`SELECT group_concat(sql, ' ') FROM sqlite_master WHERE name='social_connections'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, token) {
		t.Fatal("token found in schema")
	}
	var account string
	if err := dataStore.DB.QueryRow(`SELECT account_id FROM social_connections WHERE id=?`, connected.ID).Scan(&account); err != nil || account != "page-123" {
		t.Fatalf("metadata account=%q err=%v", account, err)
	}
	if status, payload = call("DELETE", "/api/social/connections/"+connected.ID, nil); status != 200 {
		t.Fatalf("disconnect status=%d body=%s", status, payload)
	}
	if vault.Has(socialRef(connected.ID)) {
		t.Fatal("disconnect kept token")
	}
	if status, payload = call("GET", "/api/social/connections", nil); status != 200 || strings.TrimSpace(string(payload)) != "[]" {
		t.Fatalf("list after delete=%d %s", status, payload)
	}
}

func TestSocialConnectRejectsMissingTikTokGrant(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/post/publish/creator_info/query/" || r.Header.Get("Authorization") != "Bearer tt-token" {
			t.Errorf("unexpected TikTok request")
		}
		_, _ = io.WriteString(w, `{"data":{},"error":{"code":"scope_not_authorized"}}`)
	}))
	defer platform.Close()
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	vault, err := secrets.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil, nil, nil, nil, nil, nil, dataStore, nil).WithSocialVault(vault)
	s.socialTikTokBase = platform.URL
	app := httptest.NewServer(s.Handler())
	defer app.Close()
	resp, err := http.Post(app.URL+"/api/social/connections", "application/json", strings.NewReader(`{"platform":"tiktok","token":"tt-token"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 422 {
		t.Fatalf("missing scope status=%d", resp.StatusCode)
	}
	var count int
	if err := dataStore.DB.QueryRow(`SELECT count(*) FROM social_connections`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid grant stored: %d %v", count, err)
	}
}

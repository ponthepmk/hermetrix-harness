package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hermetrix-harness/internal/discordbridge"
)

func TestDiscordRoutesRequireAuthentication(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	server := httptest.NewServer(newAuthenticator(token, "local-user", false).middleware(testHandler(t)))
	defer server.Close()
	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/remote/discord", ""},
		{http.MethodPut, "/api/remote/discord", `{"enabled":true}`},
		{http.MethodPut, "/api/remote/discord/token", `{"token":"secret"}`},
		{http.MethodPost, "/api/remote/discord/start", `{}`},
		{http.MethodPost, "/api/remote/discord/stop", `{}`},
	} {
		assertStatus(t, route.method, server.URL+route.path, route.body, "", http.StatusUnauthorized)
	}
	assertStatus(t, http.MethodGet, server.URL+"/api/remote/discord", "", "Bearer "+token, http.StatusOK)
}

func TestDiscordConfigurationIsStagedAndSecretsNeverReturned(t *testing.T) {
	server := testHTTPServer(t)
	read := func(body []byte) discordbridge.Status {
		t.Helper()
		var result discordbridge.Status
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	initial := read(requestJSON(t, server.URL+"/api/remote/discord", http.MethodGet, nil, http.StatusOK))
	if initial.Config.Enabled || initial.TokenStored || initial.State != "disabled" {
		t.Fatalf("unexpected initial status: %+v", initial)
	}
	cfg := discordbridge.Config{Enabled: true, ApplicationID: "111111111111111111", GuildIDs: []string{"222222222222222222"}, ChannelIDs: []string{"333333333333333333"}, UserIDs: []string{"444444444444444444"}, ProjectID: "project-placeholder", ProviderID: "provider-placeholder", ContextProfile: "compact-32k"}
	saved := read(requestJSON(t, server.URL+"/api/remote/discord", http.MethodPut, cfg, http.StatusOK))
	if saved.Config.Enabled || saved.State != "disabled" {
		t.Fatalf("save started an external connection: %+v", saved)
	}
	const secret = "test-discord-credential-not-a-real-token"
	for _, method := range []string{http.MethodPut, http.MethodGet} {
		path := server.URL + "/api/remote/discord"
		var body any
		if method == http.MethodPut {
			path += "/token"
			body = map[string]string{"token": secret}
		}
		data := requestJSON(t, path, method, body, http.StatusOK)
		if strings.Contains(string(data), secret) {
			t.Fatal("token leaked in API response")
		}
		if status := read(data); !status.TokenStored || status.Config.Enabled {
			t.Fatalf("token save enabled connection or forgot secret: %+v", status)
		}
	}
	requestJSON(t, server.URL+"/api/remote/discord/stop", http.MethodPost, map[string]any{}, http.StatusOK)
	cleared := read(requestJSON(t, server.URL+"/api/remote/discord/token", http.MethodPut, map[string]string{"token": ""}, http.StatusOK))
	if cleared.TokenStored {
		t.Fatal("cleared token still reported as stored")
	}
}

func TestDiscordRejectsWildcardScopeAndUnconfiguredStart(t *testing.T) {
	server := testHTTPServer(t)
	requestJSON(t, server.URL+"/api/remote/discord", http.MethodPut, map[string]any{"application_id": "111111111111111111", "user_ids": []string{"*"}}, http.StatusBadRequest)
	requestJSON(t, server.URL+"/api/remote/discord/start", http.MethodPost, map[string]any{}, http.StatusBadRequest)
	requestJSON(t, server.URL+"/api/remote/discord", http.MethodPut, map[string]any{"owner_principal_id": "someone-else"}, http.StatusBadRequest)
}

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"hermetrix-harness/internal/capabilities"
	"hermetrix-harness/internal/secrets"
	"hermetrix-harness/internal/store"
)

func TestAccessCredentialIsBoundToEndpointAndNeverSerialized(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	vault, err := secrets.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(dataStore, capabilities.NewCatalog(), nil).WithVault(vault)
	profile, err := service.Save(ctx, SaveInput{Name: "agent-knowledge", Endpoint: "https://agent-knowledge.example.test/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	profile, err = service.SetAccessCredential(ctx, profile.ID, "access-client-id", "access-client-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !profile.AccessStored {
		t.Fatal("Access credential was not stored")
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "access-client-id") || strings.Contains(string(encoded), "access-client-secret") {
		t.Fatalf("API profile leaked Access fields: %s", encoded)
	}
	attached, err := service.withAccessCredential(profile)
	if err != nil || attached.AccessClientID != "access-client-id" || attached.AccessClientSecret != "access-client-secret" {
		t.Fatalf("credential did not load for original endpoint: %v", err)
	}
	profile, err = service.Save(ctx, SaveInput{ID: profile.ID, Name: profile.Name, Endpoint: "https://other.example.test/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.withAccessCredential(profile); err == nil {
		t.Fatal("stored Access secret followed a changed endpoint")
	}
	if _, err = service.SetAccessCredential(ctx, profile.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	profile, err = service.Get(ctx, profile.ID)
	if err != nil || profile.AccessStored {
		t.Fatalf("Access credential was not cleared: %+v %v", profile, err)
	}
	if _, err = service.SetAccessCredential(ctx, profile.ID, "id-only", ""); err == nil {
		t.Fatal("partial Access credential accepted")
	}
	local, err := service.Save(ctx, SaveInput{Name: "local", Endpoint: "http://127.0.0.1:8902/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SetAccessCredential(ctx, local.ID, "id", "secret"); err == nil {
		t.Fatal("Access secret accepted for plaintext transport")
	}
}

func TestAccessHeadersAccompanyOriginBearerAndCannotFollowRedirect(t *testing.T) {
	var received atomic.Int32
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		http.Error(w, "redirected", http.StatusBadRequest)
	}))
	defer destination.Close()
	var redirect bool
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pi-token" ||
			r.Header.Get("Cf-Access-Client-Id") != "access-id" ||
			r.Header.Get("Cf-Access-Client-Secret") != "access-secret" {
			t.Errorf("missing credentials at intended origin")
		}
		if redirect {
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
			return
		}
		var request rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[]}}`, request.ID)
	}))
	defer origin.Close()
	client := NewClient(origin.Client())
	profile := Server{ID: "access-test", Endpoint: origin.URL, AccessClientID: "access-id", AccessClientSecret: "access-secret"}
	request := rpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"}
	if _, err := client.doRPC(context.Background(), profile, "pi-token", request, nil); err != nil {
		t.Fatal(err)
	}
	redirect = true
	if _, err := client.doRPC(context.Background(), profile, "pi-token", request, nil); err == nil {
		t.Fatal("redirect unexpectedly succeeded")
	}
	if received.Load() != 0 {
		t.Fatalf("secret-bearing request followed redirect %d times", received.Load())
	}
	profile.AccessClientSecret = ""
	if _, err := client.doRPC(context.Background(), profile, "pi-token", request, nil); err == nil {
		t.Fatal("partial Access credential was transmitted")
	}
}

func TestAccessSecretsAreRedactedFromRemoteResults(t *testing.T) {
	const id, secret = "access-id", "access-secret"
	result := redactJSON(json.RawMessage(`{"content":"`+id+` / `+secret+`"}`), id, secret)
	if strings.Contains(string(result), id) || strings.Contains(string(result), secret) {
		t.Fatalf("output leaked Access credential: %s", result)
	}
	err := redactError(fmt.Errorf("remote echoed %s and %s", id, secret), id, secret)
	if strings.Contains(err.Error(), id) || strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked Access credential: %v", err)
	}
}

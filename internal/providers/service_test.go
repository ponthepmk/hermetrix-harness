package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hermetrix-harness/internal/store"
)

func TestProviderProfileStoresCredentialReferenceOnly(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	t.Setenv("HERMETRIX_TEST_KEY", "secret-value-that-must-not-persist")
	service := NewService(dataStore, nil)
	profile, err := service.Save(ctx, SaveInput{Name: "test", BaseURL: "https://models.example/v1", Model: "model-a",
		APIKeyEnv: "HERMETRIX_TEST_KEY", ContextWindow: 131072, MaxOutputTokens: 8192})
	if err != nil {
		t.Fatal(err)
	}
	if !profile.CredentialReady || profile.APIKeyEnv != "HERMETRIX_TEST_KEY" {
		t.Fatalf("unexpected credential state: %+v", profile)
	}
	databaseBytes, err := os.ReadFile(filepath.Join(dataStore.Root, "hermetrix.db"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(databaseBytes), "secret-value-that-must-not-persist") {
		t.Fatal("credential value leaked into SQLite")
	}
}

func TestProviderValidationRejectsInsecureRemoteHTTP(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	_, err = NewService(dataStore, nil).Save(ctx, SaveInput{Name: "bad", BaseURL: "http://models.example/v1",
		Model: "model-a", ContextWindow: 65536, MaxOutputTokens: 4096})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected HTTPS validation error, got %v", err)
	}
}

func TestOpenAICompatibleStreaming(t *testing.T) {
	var gotAuth, gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		if err := decodeTestJSON(r, &body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotModel = body.Model
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"brief\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"HERME\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"TRIX_OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":3,\"total_tokens\":12}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	var streamed strings.Builder
	completion, err := NewOpenAIAdapter(server.Client()).StreamChat(context.Background(), Profile{BaseURL: server.URL, Model: "qwen-test"}, "test-token",
		ChatRequest{Messages: []Message{{Role: "user", Content: "ping"}}, MaxTokens: 16}, func(delta Delta) error {
			streamed.WriteString(delta.Content)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer test-token" || gotModel != "qwen-test" {
		t.Fatalf("binding mismatch auth=%q model=%q", gotAuth, gotModel)
	}
	if completion.Content != "HERMETRIX_OK" || streamed.String() != completion.Content || completion.Reasoning != "brief" {
		t.Fatalf("unexpected completion: %+v streamed=%q", completion, streamed.String())
	}
	if completion.Usage.TotalTokens != 12 || completion.FinishReason != "stop" {
		t.Fatalf("usage/finish mismatch: %+v", completion)
	}
}

func decodeTestJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

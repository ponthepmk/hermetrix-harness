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

// TestTokenScaleSurvivesAReopen is the whole point of moving the calibration
// out of memory. A server that had learned its model over-counts Thai by a
// quarter -- measured at 0.766 after eighteen turns -- threw that away on the
// next boot and went back to over-counting from 1.0.
func TestTokenScaleSurvivesAReopen(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "data")
	dataStore, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(dataStore, nil)
	profile, err := service.Save(ctx, SaveInput{Name: "gateway", BaseURL: "https://models.example/v1",
		Model: "qwen-test", ContextWindow: 131072, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if profile.TokenMultiplier != 1 {
		t.Fatalf("a new profile starts at %v, want 1", profile.TokenMultiplier)
	}
	for i := 0; i < 10; i++ {
		if err := service.ObserveTokenScale(ctx, profile.ID, 1000, 750); err != nil {
			t.Fatal(err)
		}
	}
	learned, err := service.Get(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if learned.TokenMultiplier > 0.76 || learned.TokenMultiplier < 0.74 {
		t.Fatalf("multiplier = %v after ten 0.75 observations, want ~0.75", learned.TokenMultiplier)
	}
	if learned.TokenSample != 10 {
		t.Fatalf("sample = %d, want 10", learned.TokenSample)
	}
	if err := dataStore.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := NewService(reopened, nil).Get(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.TokenMultiplier != learned.TokenMultiplier || after.TokenSample != learned.TokenSample {
		t.Fatalf("calibration was lost on reopen: %v/%d became %v/%d",
			learned.TokenMultiplier, learned.TokenSample, after.TokenMultiplier, after.TokenSample)
	}
}

// TestTokenScaleIsPerProfile covers the second half: one shared number meant
// concurrent sessions on different tokenizers corrupted each other.
func TestTokenScaleIsPerProfile(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := NewService(dataStore, nil)
	thai, err := service.Save(ctx, SaveInput{Name: "thai-heavy", BaseURL: "https://a.example/v1",
		Model: "qwen-test", ContextWindow: 131072, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	english, err := service.Save(ctx, SaveInput{Name: "english-heavy", BaseURL: "https://b.example/v1",
		Model: "other-test", ContextWindow: 131072, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := service.ObserveTokenScale(ctx, thai.ID, 1000, 700); err != nil {
			t.Fatal(err)
		}
		if err := service.ObserveTokenScale(ctx, english.ID, 1000, 1400); err != nil {
			t.Fatal(err)
		}
	}
	left, _ := service.Get(ctx, thai.ID)
	right, _ := service.Get(ctx, english.ID)
	if left.TokenMultiplier >= 1 || right.TokenMultiplier <= 1 {
		t.Fatalf("profiles converged instead of diverging: %v and %v",
			left.TokenMultiplier, right.TokenMultiplier)
	}
}

// TestOneBadUsageReportCannotReplaceTheCalibration bounds a single sample. A
// provider that reports zero-ish prompt usage, or a fixture that reports a
// token count unrelated to the request, must not flatten the ruler.
func TestOneBadUsageReportCannotReplaceTheCalibration(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := NewService(dataStore, nil)
	profile, err := service.Save(ctx, SaveInput{Name: "gateway", BaseURL: "https://models.example/v1",
		Model: "qwen-test", ContextWindow: 131072, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ObserveTokenScale(ctx, profile.ID, 3677, 100); err != nil {
		t.Fatal(err)
	}
	after, _ := service.Get(ctx, profile.ID)
	if after.TokenMultiplier < tokenScaleFloor {
		t.Fatalf("a 0.027 ratio moved the multiplier to %v, below the %v floor",
			after.TokenMultiplier, tokenScaleFloor)
	}
	if err := service.ObserveTokenScale(ctx, profile.ID, 100, 100000); err != nil {
		t.Fatal(err)
	}
	after, _ = service.Get(ctx, profile.ID)
	if after.TokenMultiplier > tokenScaleCeiling {
		t.Fatalf("a 1000x ratio moved the multiplier to %v, above the %v ceiling",
			after.TokenMultiplier, tokenScaleCeiling)
	}
	before := after.TokenMultiplier
	if err := service.ObserveTokenScale(ctx, profile.ID, 0, 500); err != nil {
		t.Fatal(err)
	}
	if err := service.ObserveTokenScale(ctx, profile.ID, 500, 0); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := service.Get(ctx, profile.ID)
	if unchanged.TokenMultiplier != before || unchanged.TokenSample != after.TokenSample {
		t.Fatalf("an empty observation was counted: %v/%d became %v/%d",
			before, after.TokenSample, unchanged.TokenMultiplier, unchanged.TokenSample)
	}
}

package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hermetrix-harness/internal/inference"
	"hermetrix-harness/internal/store"
)

type presetCaptureAdapter struct{ request ChatRequest }

func (a *presetCaptureAdapter) StreamChat(_ context.Context, _ Profile, _ string, request ChatRequest,
	_ func(Delta) error) (Completion, error) {
	a.request = request
	return Completion{FinishReason: "stop", Usage: Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}, nil
}

func TestStreamChatAppliesImmutablePresetAndPersistsBinding(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	adapter := &presetCaptureAdapter{}
	service := NewService(dataStore, adapter)
	profile, err := service.Save(ctx, SaveInput{Name: "local", BaseURL: "http://127.0.0.1:8080/v1", Model: "m",
		ContextWindow: 98304, MaxOutputTokens: 8192})
	if err != nil {
		t.Fatal(err)
	}
	ctx = inference.WithOwner(ctx, inference.Owner{Kind: "task", ID: "task-1", Source: "planner", Priority: inference.PriorityTask,
		PresetID: "planner", PresetRevision: 1, PresetRole: "planner", RuntimeFingerprintID: profile.RuntimeFingerprintID})
	if _, err = service.StreamChat(ctx, profile, ChatRequest{Messages: []Message{{Role: "user", Content: "plan"}}, MaxTokens: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if adapter.request.MaxTokens != 5120 || adapter.request.Temperature == nil || *adapter.request.Temperature != 0.2 {
		t.Fatalf("wire request did not use preset: %+v", adapter.request)
	}
	var presetID, digest string
	var revision int
	if err = dataStore.DB.QueryRow(`SELECT preset_id,preset_revision,effective_parameter_digest FROM inference_usage_ledger
		WHERE owner_id='task-1'`).Scan(&presetID, &revision, &digest); err != nil {
		t.Fatal(err)
	}
	if presetID != "planner" || revision != 1 || len(digest) != 64 {
		t.Fatalf("ledger binding=%s@%d digest=%q", presetID, revision, digest)
	}
}

func TestContextBoundTaskPresetDispatchesSmallRequestButRejectsOversizedInput(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	adapter := &presetCaptureAdapter{}
	service := NewService(dataStore, adapter)
	profile, err := service.Save(ctx, SaveInput{Name: "61k runtime", BaseURL: "http://127.0.0.1:8080/v1", Model: "m", ContextWindow: 61440, MaxOutputTokens: 8192})
	if err != nil {
		t.Fatal(err)
	}
	preset, err := service.ResolveTaskPreset(ctx, profile, "planner", 1)
	if err != nil {
		t.Fatal(err)
	}
	taskContext := inference.WithOwner(ctx, inference.Owner{Kind: "task", ID: "bounded-plan", Source: "planner", Priority: inference.PriorityTask,
		PresetID: preset.ID, PresetRevision: preset.Revision, PresetRole: preset.Role})
	if _, err = service.StreamChat(taskContext, profile, ChatRequest{Messages: []Message{{Role: "user", Content: "plan a small task"}}, MaxTokens: 8192}, nil); err != nil {
		t.Fatal(err)
	}
	if adapter.request.MaxTokens != 5120 {
		t.Fatalf("output reservation changed: %d", adapter.request.MaxTokens)
	}
	var recorded string
	if err = dataStore.DB.QueryRow(`SELECT preset_id FROM inference_usage_ledger WHERE owner_id='bounded-plan'`).Scan(&recorded); err != nil || recorded != preset.ID {
		t.Fatalf("effective preset identity was not persisted: %q err=%v", recorded, err)
	}
	adapter.request = ChatRequest{}
	oversized := ChatRequest{Messages: []Message{{Role: "user", Content: strings.Repeat("long prompt ", 60000)}}}
	if _, err = service.StreamChat(taskContext, profile, oversized, nil); err == nil || !strings.Contains(err.Error(), "preset input ceiling") || adapter.request.Messages != nil {
		t.Fatalf("oversized preset input reached adapter: err=%v dispatched=%v", err, adapter.request.Messages != nil)
	}
	// Ordinary chat has no task preset and retains its caller's output cap.
	if _, err = service.StreamChat(ctx, profile, ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}, MaxTokens: 100}, nil); err != nil || adapter.request.MaxTokens != 100 {
		t.Fatalf("ordinary chat changed: err=%v request=%+v", err, adapter.request)
	}
}

func TestImagePartRequiresQualifiedModalityBeforeDispatch(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	adapter := &presetCaptureAdapter{}
	service := NewService(dataStore, adapter)
	profile, err := service.Save(ctx, SaveInput{Name: "local-vision", BaseURL: "http://127.0.0.1:8080/v1", Model: "m",
		ContextWindow: 98304, MaxOutputTokens: 8192})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.StreamChat(ctx, profile, ChatRequest{Messages: []Message{{Role: "user", Parts: []ContentPart{{Kind: "image",
		MediaType: "image/png", Data: []byte("png")}}}}}, nil)
	var unsupported *UnsupportedCapabilityError
	if !errors.As(err, &unsupported) || adapter.request.Messages != nil {
		t.Fatalf("err=%v request=%+v", err, adapter.request)
	}
}

func TestOpenAIMultipartWireShapeUsesTransientDataURL(t *testing.T) {
	messages := openAIMessages([]Message{{Role: "user", Parts: []ContentPart{{Kind: "text", Text: "inspect"},
		{Kind: "image", MediaType: "image/png", Data: []byte{1, 2, 3}, ArtifactID: "artifact-private"}}}})
	encoded, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(encoded)
	if !strings.Contains(raw, `data:image/png;base64,AQID`) || strings.Contains(raw, "artifact-private") {
		t.Fatalf("unexpected multipart wire payload: %s", raw)
	}
}

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
		if err := service.ObserveTokenScale(ctx, profile.ID, 1, 1000, 750); err != nil {
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
		if err := service.ObserveTokenScale(ctx, thai.ID, 1, 1000, 700); err != nil {
			t.Fatal(err)
		}
		if err := service.ObserveTokenScale(ctx, english.ID, 1, 1000, 1400); err != nil {
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
	if err := service.ObserveTokenScale(ctx, profile.ID, 1, 3677, 100); err != nil {
		t.Fatal(err)
	}
	after, _ := service.Get(ctx, profile.ID)
	if after.TokenMultiplier < tokenScaleFloor {
		t.Fatalf("a 0.027 ratio moved the multiplier to %v, below the %v floor",
			after.TokenMultiplier, tokenScaleFloor)
	}
	if err := service.ObserveTokenScale(ctx, profile.ID, 1, 100, 100000); err != nil {
		t.Fatal(err)
	}
	after, _ = service.Get(ctx, profile.ID)
	if after.TokenMultiplier > tokenScaleCeiling {
		t.Fatalf("a 1000x ratio moved the multiplier to %v, above the %v ceiling",
			after.TokenMultiplier, tokenScaleCeiling)
	}
	before := after.TokenMultiplier
	for _, empty := range []struct {
		name              string
		applied           float64
		predicted, actual int
	}{
		{"no prediction", 1, 0, 500},
		{"no usage", 1, 500, 0},
		// An unknown applied scale cannot be divided back out, so the sample
		// carries no information about the ratio and must not be counted.
		{"no applied scale", 0, 500, 400},
	} {
		if err := service.ObserveTokenScale(ctx, profile.ID, empty.applied, empty.predicted, empty.actual); err != nil {
			t.Fatalf("%s: %v", empty.name, err)
		}
	}
	unchanged, _ := service.Get(ctx, profile.ID)
	if unchanged.TokenMultiplier != before || unchanged.TokenSample != after.TokenSample {
		t.Fatalf("an empty observation was counted: %v/%d became %v/%d",
			before, after.TokenSample, unchanged.TokenMultiplier, unchanged.TokenSample)
	}
}

// TestCalibrationConvergesOnTheRatioNotItsSquareRoot is the property the first
// version got wrong, and only live data exposed.
//
// The multiplier is learned from actual/predicted -- but predicted has already
// been scaled by that same multiplier, so averaging the ratio is a feedback
// loop. Its fixed point is the square root of the truth: for a model whose real
// ratio is 0.80 the multiplier settles at 0.894 and leaves a permanent -10.6%
// error, close enough to the +/-10% gate to read as noise and never close.
func TestCalibrationConvergesOnTheRatioNotItsSquareRoot(t *testing.T) {
	const trueRatio = 0.80
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
	multiplier := 1.0
	for i := 0; i < 200; i++ {
		// The compiler measures the same content with whatever ruler it has, and
		// the provider counts the same content the same way every time.
		predicted := int(1000 * multiplier)
		actual := int(1000 * trueRatio)
		if err := service.ObserveTokenScale(ctx, profile.ID, multiplier, predicted, actual); err != nil {
			t.Fatal(err)
		}
		learned, err := service.Get(ctx, profile.ID)
		if err != nil {
			t.Fatal(err)
		}
		multiplier = learned.TokenMultiplier
	}
	if math.Abs(multiplier-trueRatio) > 0.01 {
		t.Fatalf("multiplier settled at %.4f, want %.2f (the square-root fixed point is %.4f)",
			multiplier, trueRatio, math.Sqrt(trueRatio))
	}
	residual := (1000*trueRatio - 1000*multiplier) / (1000 * multiplier)
	if math.Abs(residual) > 0.02 {
		t.Fatalf("a converged calibration still leaves %.1f%% error", 100*residual)
	}
}

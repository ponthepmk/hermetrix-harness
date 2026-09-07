package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"hermetrix-harness/internal/store"
)

func TestAnthropicNativeAdapterTranslatesToolsAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("X-Api-Key") != "anthropic-secret" || r.Header.Get("Anthropic-Version") == "" {
			t.Errorf("unexpected Anthropic request: path=%s key=%q version=%q", r.URL.Path,
				r.Header.Get("X-Api-Key"), r.Header.Get("Anthropic-Version"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["system"] != "system policy" || len(body["tools"].([]any)) != 1 {
			t.Errorf("Anthropic envelope = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"content":[{"type":"thinking","thinking":"brief"},{"type":"text","text":"done"},{"type":"tool_use","id":"call_a","name":"lookup","input":{"q":"ไทย"}}],"stop_reason":"tool_use","usage":{"input_tokens":31,"output_tokens":9}}`)
	}))
	defer server.Close()

	var emitted Delta
	completion, err := NewAnthropicAdapter(server.Client()).StreamChat(context.Background(), Profile{
		BaseURL: server.URL + "/v1", Model: "claude-test", MaxOutputTokens: 2048,
	}, "anthropic-secret", ChatRequest{Messages: []Message{{Role: "system", Content: "system policy"},
		{Role: "user", Content: "use a tool"}}, Tools: []ToolDefinition{{Type: "function", Function: ToolFunction{
		Name: "lookup", Description: "look up", Parameters: map[string]any{"type": "object"},
	}}}}, func(delta Delta) error { emitted = delta; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if completion.Content != "done" || completion.Reasoning != "brief" || completion.FinishReason != "tool_calls" ||
		completion.Usage.TotalTokens != 40 || len(completion.ToolCalls) != 1 ||
		completion.ToolCalls[0].Arguments != `{"q":"ไทย"}` {
		t.Fatalf("normalized Anthropic completion = %+v", completion)
	}
	if len(emitted.ToolCalls) != 1 || emitted.Content != "done" {
		t.Fatalf("emitted Anthropic delta = %+v", emitted)
	}
}

func TestGeminiNativeAdapterTranslatesFunctionCallsWithoutCredentialInURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-test:generateContent" {
			t.Errorf("Gemini path = %q", r.URL.Path)
		}
		if r.URL.RawQuery != "" || r.Header.Get("X-Goog-Api-Key") != "gemini-secret" {
			t.Errorf("Gemini credential leaked/missing: query=%q header=%q", r.URL.RawQuery, r.Header.Get("X-Goog-Api-Key"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body["systemInstruction"]; !ok {
			t.Errorf("Gemini envelope has no system instruction: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"internal","thought":true},{"text":"answer"},{"functionCall":{"name":"lookup","args":{"q":"safe"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":7,"totalTokenCount":27}}`)
	}))
	defer server.Close()

	completion, err := NewGeminiAdapter(server.Client()).StreamChat(context.Background(), Profile{
		BaseURL: server.URL + "/v1beta", Model: "models/gemini-test", MaxOutputTokens: 2048,
	}, "gemini-secret", ChatRequest{Messages: []Message{{Role: "system", Content: "system policy"},
		{Role: "user", Content: "use a tool"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completion.Content != "answer" || completion.Reasoning != "internal" || completion.FinishReason != "stop" ||
		completion.Usage.TotalTokens != 27 || len(completion.ToolCalls) != 1 ||
		!strings.Contains(completion.ToolCalls[0].Arguments, `"q":"safe"`) {
		t.Fatalf("normalized Gemini completion = %+v", completion)
	}
}

type fixedAdapter struct{ content string }

func (a fixedAdapter) StreamChat(context.Context, Profile, string, ChatRequest, func(Delta) error) (Completion, error) {
	return Completion{Content: a.content, FinishReason: "stop"}, nil
}

func TestServiceDispatchesByAdapterKind(t *testing.T) {
	dataStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(dataStore, nil)
	service.WithAdapter(AdapterAnthropicNative, fixedAdapter{content: "native"})
	profile, err := service.Save(context.Background(), SaveInput{Name: "anthropic", AdapterKind: AdapterAnthropicNative,
		BaseURL: "https://api.anthropic.com/v1", Model: "claude-test", ContextWindow: 65536, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	completion, err := service.StreamChat(context.Background(), profile, ChatRequest{}, nil)
	if err != nil || completion.Content != "native" {
		t.Fatalf("dispatch completion=%+v err=%v", completion, err)
	}
}

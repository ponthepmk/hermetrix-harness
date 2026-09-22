package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalLlamaPresetReasoningBudgetIsSentAndCapabilityCacheIsRuntimeBound(t *testing.T) {
	probes, calls := 0, 0
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/props" {
			probes++
			fmt.Fprint(w, `{"build_info":"b10709-9a9394a89","chat_template":"<think> {{ enable_thinking }}","chat_template_caps":{"supports_reasoning_effort":true}}`)
			return
		}
		calls++
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	adapter := NewOpenAIAdapter(server.Client())
	profile := Profile{ID: "local", BaseURL: server.URL + "/v1", Model: "model", RuntimeFingerprintID: "runtime-a"}
	request := ChatRequest{Messages: []Message{{Role: "user", Content: "plan"}}, MaxTokens: 5120, Reasoning: &ReasoningControl{Mode: "bounded", TokenCap: 3072}}
	for i := 0; i < 2; i++ {
		if _, err := adapter.StreamChat(context.Background(), profile, "", request, nil); err != nil {
			t.Fatal(err)
		}
	}
	if probes != 1 || calls != 2 || payload["reasoning_budget_tokens"] != float64(3072) || payload["max_tokens"] != float64(5120) || payload["reasoning_format"] != "deepseek" {
		t.Fatalf("probes=%d calls=%d payload=%v", probes, calls, payload)
	}
	profile.RuntimeFingerprintID = "runtime-b"
	request.Reasoning = &ReasoningControl{Mode: "disabled", TokenCap: 0}
	if _, err := adapter.StreamChat(context.Background(), profile, "", request, nil); err != nil {
		t.Fatal(err)
	}
	kwargs, _ := payload["chat_template_kwargs"].(map[string]any)
	if probes != 2 || payload["reasoning_budget_tokens"] != float64(0) || kwargs["enable_thinking"] != false {
		t.Fatalf("new runtime reused stale capability or disabled mode lost: probes=%d payload=%v", probes, payload)
	}
}

type reasoningCaptureTransport struct {
	requests []string
	payload  map[string]any
}

func (transport *reasoningCaptureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.requests = append(transport.requests, request.URL.String())
	if request.Body != nil {
		_ = json.NewDecoder(request.Body).Decode(&transport.payload)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"done"},"finish_reason":"stop"}]}`)), Request: request}, nil
}

func TestRemoteCompatibleProviderNeverReceivesLlamaReasoningFlagsOrProbe(t *testing.T) {
	transport := &reasoningCaptureTransport{}
	adapter := NewOpenAIAdapter(&http.Client{Transport: transport})
	profile := Profile{BaseURL: "https://api.example.test/v1", Model: "model"}
	_, err := adapter.StreamChat(context.Background(), profile, "", ChatRequest{MaxTokens: 5120, Reasoning: &ReasoningControl{Mode: "bounded", TokenCap: 3072}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.requests) != 1 || !strings.HasSuffix(transport.requests[0], "/chat/completions") {
		t.Fatalf("remote capability probe: %v", transport.requests)
	}
	for _, field := range []string{"reasoning_budget_tokens", "reasoning_format", "chat_template_kwargs"} {
		if _, ok := transport.payload[field]; ok {
			t.Fatalf("llama flag %s sent to foreign provider", field)
		}
	}
}

func TestUnknownLocalRuntimeAndOrdinaryChatKeepCompatiblePayload(t *testing.T) {
	probes := 0
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/props" {
			probes++
			fmt.Fprint(w, `{"name":"different-server"}`)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	adapter := NewOpenAIAdapter(server.Client())
	profile := Profile{BaseURL: server.URL + "/v1", Model: "model"}
	for _, request := range []ChatRequest{{MaxTokens: 200}, {MaxTokens: 5120, Reasoning: &ReasoningControl{Mode: "bounded", TokenCap: 3072}}} {
		if _, err := adapter.StreamChat(context.Background(), profile, "", request, nil); err != nil {
			t.Fatal(err)
		}
		if _, ok := payload["reasoning_budget_tokens"]; ok {
			t.Fatalf("unexpected llama flag: %v", payload)
		}
	}
	if probes != 1 {
		t.Fatalf("ordinary chat should not probe: %d", probes)
	}
}

func TestOlderOrMalformedLlamaBuildDoesNotAdvertiseBudgetSupport(t *testing.T) {
	for _, build := range []string{"b10708-abc123", "b999-abc123", "b10709unverified", "bnot-a-number", "b999999999999999999999999999999999999999"} {
		t.Run(build, func(t *testing.T) {
			var payload map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/props" {
					_ = json.NewEncoder(w).Encode(map[string]any{"build_info": build, "chat_template": "<think>", "chat_template_caps": map[string]bool{"supports_reasoning_effort": true}})
					return
				}
				_ = json.NewDecoder(r.Body).Decode(&payload)
				fmt.Fprint(w, `{"choices":[{"message":{"content":"done"},"finish_reason":"stop"}]}`)
			}))
			defer server.Close()
			adapter := NewOpenAIAdapter(server.Client())
			_, err := adapter.StreamChat(context.Background(), Profile{BaseURL: server.URL + "/v1"}, "", ChatRequest{MaxTokens: 5120, Reasoning: &ReasoningControl{Mode: "bounded", TokenCap: 3072}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := payload["reasoning_budget_tokens"]; ok {
				t.Fatalf("unverified build %q received reasoning control flags", build)
			}
		})
	}
}

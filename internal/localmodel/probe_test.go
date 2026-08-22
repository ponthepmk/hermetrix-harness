package localmodel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaRequiresExplicitAllocationForVerification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			fmt.Fprint(w, `{"models":[{"model":"local-model","context_length":65536}]}`)
		case "/api/show":
			fmt.Fprint(w, `{"parameters":"num_ctx 32768\ntemperature 0.2\n","model_info":{"llama.context_length":131072}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	result, err := NewProber().Probe(context.Background(), ProbeRequest{Runtime: "ollama", Endpoint: server.URL + "/v1", Model: "local-model"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || result.AllocatedContext != 65536 || result.ConfiguredContext != 32768 || result.TrainingContext != 131072 || result.Mode != "certified-context" {
		t.Fatalf("result = %+v", result)
	}
}

func TestOllamaTrainingMaximumDoesNotCertifyRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ps" {
			fmt.Fprint(w, `{"models":[]}`)
			return
		}
		fmt.Fprint(w, `{"parameters":"num_ctx 65536\ntemperature 0.2\n","model_info":{"qwen.context_length":131072}}`)
	}))
	defer server.Close()
	result, err := NewProber().Probe(context.Background(), ProbeRequest{Runtime: "ollama", Endpoint: server.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Verified || result.AllocatedContext != 0 || result.ConfiguredContext != 65536 || result.TrainingContext != 131072 || result.Mode != "limited" {
		t.Fatalf("result = %+v", result)
	}
}

func TestLMStudioUsesLoadedInstanceContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"models":[{"key":"publisher/model-a","max_context_length":1048576,"loaded_instances":[{"config":{"context_length":32768}}]}]}`)
	}))
	defer server.Close()
	result, err := NewProber().Probe(context.Background(), ProbeRequest{Runtime: "lmstudio", Endpoint: server.URL + "/api/v1", Model: "publisher/model-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || result.AllocatedContext != 32768 || result.TrainingContext != 1048576 || result.Mode != "compact-context" {
		t.Fatalf("result = %+v", result)
	}
}

func TestLlamaCppPrefersRuntimeNCtx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/props":
			fmt.Fprint(w, `{"default_generation_settings":{"n_ctx":65536}}`)
		case "/v1/models/m":
			http.NotFound(w, r)
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"m","meta":{"n_ctx_train":262144}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	result, err := NewProber().Probe(context.Background(), ProbeRequest{Runtime: "llamacpp", Endpoint: server.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if result.AllocatedContext != 65536 || result.TrainingContext != 262144 || !result.Verified {
		t.Fatalf("result = %+v", result)
	}
}

func TestLlamaCppFallsBackToSmallestSlot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/props":
			http.NotFound(w, r)
		case "/slots":
			fmt.Fprint(w, `[{"id":0,"n_ctx":65536},{"id":1,"n_ctx":32768}]`)
		case "/v1/models/m":
			http.NotFound(w, r)
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"m","meta":{"n_ctx_train":131072}}]}`)
		}
	}))
	defer server.Close()
	result, err := NewProber().Probe(context.Background(), ProbeRequest{Runtime: "llamacpp", Endpoint: server.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || result.AllocatedContext != 32768 || result.ContextSource != "llamacpp_slots_min_n_ctx" {
		t.Fatalf("result = %+v", result)
	}
}

func TestVLLMUsesServerInfoRuntimeConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/server_info" || r.URL.Query().Get("config_format") != "json" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"vllm_config":{"model_config":{"max_model_len":65536}}}`)
	}))
	defer server.Close()
	result, err := NewProber().Probe(context.Background(), ProbeRequest{Runtime: "vllm", Endpoint: server.URL + "/v1", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verified || result.AllocatedContext != 65536 || result.ContextSource != "vllm_server_info_max_model_len" {
		t.Fatalf("result = %+v", result)
	}
}

func TestProbeRejectsNonLocalEndpoint(t *testing.T) {
	_, err := NewProber().Probe(context.Background(), ProbeRequest{Runtime: "ollama", Endpoint: "https://example.com", Model: "m"})
	if err == nil {
		t.Fatal("remote endpoint was accepted")
	}
}

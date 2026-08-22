package localmodel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ProbeRequest struct {
	Runtime  string `json:"runtime"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
}

type Result struct {
	Runtime           string    `json:"runtime"`
	Endpoint          string    `json:"endpoint"`
	Model             string    `json:"model"`
	AllocatedContext  int       `json:"allocated_context"`
	ConfiguredContext int       `json:"configured_context"`
	TrainingContext   int       `json:"training_context"`
	ContextSource     string    `json:"context_source"`
	Verified          bool      `json:"verified"`
	Mode              string    `json:"mode"`
	Warnings          []string  `json:"warnings"`
	ProbedAt          time.Time `json:"probed_at"`
}

type Prober struct{ client *http.Client }

func NewProber() *Prober {
	return &Prober{client: &http.Client{Timeout: 4 * time.Second}}
}

func NewProberWithClient(client *http.Client) *Prober { return &Prober{client: client} }

func (p *Prober) Probe(ctx context.Context, input ProbeRequest) (Result, error) {
	input.Runtime = strings.ToLower(strings.TrimSpace(input.Runtime))
	input.Model = strings.TrimSpace(input.Model)
	endpoint, err := validateEndpoint(input.Endpoint)
	if err != nil {
		return Result{}, err
	}
	if input.Model == "" {
		return Result{}, fmt.Errorf("model is required")
	}
	result := Result{Runtime: input.Runtime, Endpoint: endpoint.String(), Model: input.Model, ProbedAt: time.Now().UTC()}
	switch input.Runtime {
	case "ollama":
		err = p.probeOllama(ctx, endpoint, &result)
	case "lmstudio":
		err = p.probeLMStudio(ctx, endpoint, &result)
	case "vllm":
		err = p.probeVLLM(ctx, endpoint, &result)
	case "llamacpp":
		err = p.probeLlamaCPP(ctx, endpoint, &result)
	default:
		return Result{}, fmt.Errorf("unsupported local runtime %q", input.Runtime)
	}
	if err != nil {
		return Result{}, err
	}
	if result.Verified {
		result.Mode = modeFor(result.AllocatedContext)
	} else {
		result.Mode = "limited"
	}
	if !result.Verified {
		result.Warnings = append(result.Warnings, "runtime allocation is unverified; training context cannot certify agent mode")
	}
	if result.AllocatedContext > 0 && result.AllocatedContext < 32768 {
		result.Warnings = append(result.Warnings, "allocated context is below the compact agent threshold")
	}
	return result, nil
}

func (p *Prober) probeOllama(ctx context.Context, endpoint *url.URL, result *Result) error {
	base := stripKnownSuffix(endpoint, "/v1")
	if data, err := p.request(ctx, http.MethodGet, base+"/api/ps", nil); err == nil {
		var running map[string]any
		if json.Unmarshal(data, &running) == nil {
			for _, raw := range arrayFrom(running, "models") {
				model, _ := raw.(map[string]any)
				if modelMatches(model, result.Model) {
					if value := intFrom(model["context_length"]); value > 0 {
						result.AllocatedContext, result.ContextSource, result.Verified = value, "ollama_ps_context_length", true
					}
				}
			}
		}
	}
	payload, _ := json.Marshal(map[string]string{"model": result.Model})
	data, err := p.request(ctx, http.MethodPost, base+"/api/show", payload)
	if err != nil {
		return fmt.Errorf("probe Ollama /api/show: %w", err)
	}
	var response struct {
		Parameters string         `json:"parameters"`
		ModelInfo  map[string]any `json:"model_info"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("decode Ollama probe: %w", err)
	}
	result.TrainingContext = contextFromMap(response.ModelInfo, "context_length")
	result.ConfiguredContext = numCtxFromParameters(response.Parameters)
	if result.ContextSource == "" {
		result.ContextSource = "ollama_training_metadata"
	}
	if result.AllocatedContext == 0 && result.ConfiguredContext == 0 && result.TrainingContext == 0 {
		return fmt.Errorf("Ollama did not report context metadata")
	}
	return nil
}

func (p *Prober) probeLMStudio(ctx context.Context, endpoint *url.URL, result *Result) error {
	base := stripKnownSuffix(endpoint, "/api/v1", "/v1")
	data, err := p.request(ctx, http.MethodGet, base+"/api/v1/models", nil)
	if err != nil {
		return fmt.Errorf("probe LM Studio native models: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	models := arrayFrom(payload, "models")
	if len(models) == 0 {
		models = arrayFrom(payload, "data")
	}
	for _, raw := range models {
		model, ok := raw.(map[string]any)
		if !ok || !modelMatches(model, result.Model) {
			continue
		}
		result.TrainingContext = intFrom(model["max_context_length"])
		instances, _ := model["loaded_instances"].([]any)
		for _, instanceRaw := range instances {
			instance, _ := instanceRaw.(map[string]any)
			config, _ := instance["config"].(map[string]any)
			if value := intFrom(config["context_length"]); value > 0 {
				result.AllocatedContext = value
				result.ContextSource = "lmstudio_loaded_instance"
				result.Verified = true
				return nil
			}
		}
	}
	return fmt.Errorf("LM Studio model %q is not loaded with context metadata", result.Model)
}

func (p *Prober) probeVLLM(ctx context.Context, endpoint *url.URL, result *Result) error {
	base := stripKnownSuffix(endpoint, "/v1")
	data, err := p.request(ctx, http.MethodGet, base+"/server_info?config_format=json", nil)
	if err != nil {
		return fmt.Errorf("probe vLLM /server_info: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("decode vLLM server info: %w", err)
	}
	if value := findIntRecursive(payload, "max_model_len"); value > 0 {
		result.AllocatedContext, result.ContextSource, result.Verified = value, "vllm_server_info_max_model_len", true
		return nil
	}
	return fmt.Errorf("vLLM server info did not report max_model_len")
}

func (p *Prober) probeLlamaCPP(ctx context.Context, endpoint *url.URL, result *Result) error {
	base := stripKnownSuffix(endpoint, "/v1")
	data, propsErr := p.request(ctx, http.MethodGet, base+"/props", nil)
	if propsErr == nil {
		var props map[string]any
		if json.Unmarshal(data, &props) == nil {
			settings, _ := props["default_generation_settings"].(map[string]any)
			if value := intFrom(settings["n_ctx"]); value > 0 {
				result.AllocatedContext, result.ContextSource, result.Verified = value, "llamacpp_props_n_ctx", true
			}
		}
	}
	if !result.Verified {
		data, slotsErr := p.request(ctx, http.MethodGet, base+"/slots", nil)
		if slotsErr == nil {
			var slots []any
			if json.Unmarshal(data, &slots) == nil {
				for _, raw := range slots {
					slot, _ := raw.(map[string]any)
					value := intFrom(slot["n_ctx"])
					if value > 0 && (result.AllocatedContext == 0 || value < result.AllocatedContext) {
						result.AllocatedContext = value
					}
				}
				if result.AllocatedContext > 0 {
					result.ContextSource, result.Verified = "llamacpp_slots_min_n_ctx", true
				}
			}
		}
	}

	// OpenAI model metadata is useful for the training maximum, but never
	// substitutes for the runtime n_ctx reported by /props or /slots.
	data, detailErr := p.request(ctx, http.MethodGet, base+"/v1/models/"+url.PathEscape(result.Model), nil)
	var model map[string]any
	if detailErr == nil {
		_ = json.Unmarshal(data, &model)
	}
	if len(model) == 0 {
		data, err := p.request(ctx, http.MethodGet, base+"/v1/models", nil)
		if err != nil {
			return fmt.Errorf("probe local models: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			return err
		}
		models := arrayFrom(payload, "data")
		for _, raw := range models {
			candidate, _ := raw.(map[string]any)
			if modelMatches(candidate, result.Model) || len(models) == 1 {
				model = candidate
				break
			}
		}
	}
	if len(model) == 0 && !result.Verified {
		return fmt.Errorf("model %q was not found on %s", result.Model, base)
	}
	meta, _ := model["meta"].(map[string]any)
	result.TrainingContext = intFrom(meta["n_ctx_train"])
	if !result.Verified {
		return fmt.Errorf("llama.cpp did not report allocated context through /props or /slots")
	}
	return nil
}

func (p *Prober) request(ctx context.Context, method, endpoint string, body []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func validateEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("invalid local model endpoint")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("local model endpoint must use HTTP or HTTPS")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return nil, fmt.Errorf("remote model endpoints are disabled in the local probe")
	}
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed, nil
}

func stripKnownSuffix(endpoint *url.URL, suffixes ...string) string {
	copy := *endpoint
	path := strings.TrimRight(copy.Path, "/")
	for _, suffix := range suffixes {
		if strings.HasSuffix(path, suffix) {
			path = strings.TrimSuffix(path, suffix)
		}
	}
	copy.Path = path
	return strings.TrimRight(copy.String(), "/")
}

var numCtxPattern = regexp.MustCompile(`(?m)^\s*num_ctx\s+([0-9]+)\s*$`)

func numCtxFromParameters(parameters string) int {
	match := numCtxPattern.FindStringSubmatch(parameters)
	if len(match) != 2 {
		return 0
	}
	value, _ := strconv.Atoi(match[1])
	return value
}

func contextFromMap(values map[string]any, contains string) int {
	for key, value := range values {
		if strings.Contains(strings.ToLower(key), contains) {
			if number := intFrom(value); number > 0 {
				return number
			}
		}
	}
	return 0
}

func intFrom(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		number, _ := typed.Int64()
		return int(number)
	case string:
		number, _ := strconv.Atoi(typed)
		return number
	default:
		return 0
	}
}

func arrayFrom(payload map[string]any, key string) []any {
	values, _ := payload[key].([]any)
	return values
}

func modelMatches(model map[string]any, wanted string) bool {
	for _, key := range []string{"id", "key", "model", "name", "path"} {
		if value, _ := model[key].(string); value == wanted || strings.HasSuffix(value, "/"+wanted) {
			return true
		}
	}
	return false
}

func findIntRecursive(value any, wanted string) int {
	switch typed := value.(type) {
	case map[string]any:
		if number := intFrom(typed[wanted]); number > 0 {
			return number
		}
		for _, child := range typed {
			if number := findIntRecursive(child, wanted); number > 0 {
				return number
			}
		}
	case []any:
		for _, child := range typed {
			if number := findIntRecursive(child, wanted); number > 0 {
				return number
			}
		}
	}
	return 0
}

func modeFor(contextLength int) string {
	switch {
	case contextLength >= 65536:
		return "certified-context"
	case contextLength >= 32768:
		return "compact-context"
	default:
		return "limited"
	}
}

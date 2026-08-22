package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/agent"
	"hermetrix-harness/internal/capabilities"
	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/curator"
	"hermetrix-harness/internal/fidelity"
	"hermetrix-harness/internal/learning"
	"hermetrix-harness/internal/localmodel"
	"hermetrix-harness/internal/mcp"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/qualification"
	"hermetrix-harness/internal/runtime"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	toolruntime "hermetrix-harness/internal/tools"
)

func testHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	estimator := ctxcompiler.NewAdaptiveEstimator()
	compiler := ctxcompiler.NewCompiler(estimator, ctxcompiler.NewBlobSpiller(dataStore.Blobs), ctxcompiler.StructuredCompactor{})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	skillService := skills.NewService(dataStore)
	gate := runtime.NewInferenceGate()
	learningService := learning.NewService(dataStore, skillService, gate, learning.StructuredReviewer{})
	curatorService := curator.NewService(dataStore, skillService)
	productService := product.NewService(dataStore, skillService)
	providerService := providers.NewService(dataStore, nil)
	toolRegistry, err := toolruntime.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	capabilityCatalog := capabilities.NewCatalog()
	mcpService := mcp.NewService(dataStore, capabilityCatalog, nil)
	toolRegistry.SetCatalog(capabilityCatalog)
	agentService := agent.NewService(dataStore, providerService, compiler, estimator, gate, toolRegistry, skillService).WithLearning(learningService)
	fidelityService := fidelity.NewService(dataStore, compiler)
	if err := fidelityService.EnsureDefaultCorpus(context.Background()); err != nil {
		t.Fatal(err)
	}
	qualificationService := qualification.NewService(dataStore, providerService, localmodel.NewProber(), gate, estimator)
	server := httptest.NewServer(New(skillService, learningService, curatorService, compiler, estimator,
		localmodel.NewProber(), providerService, agentService, logger).WithMCP(mcpService, capabilityCatalog).
		WithFidelity(fidelityService).WithQualification(qualificationService).WithProduct(productService).Handler())
	t.Cleanup(server.Close)
	return server
}

func TestBootstrapCollectionsAreArraysAndUIHasSecurityHeaders(t *testing.T) {
	server := testHTTPServer(t)
	response, err := http.Get(server.URL + "/api/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"skills", "candidates", "archives", "relations", "reviews", "curator_runs", "profiles", "providers", "mcp_servers", "sessions"} {
		if string(body[field]) == "null" || len(body[field]) == 0 {
			t.Fatalf("%s must be a JSON array, got %s", field, body[field])
		}
	}
	page, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()
	if page.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP")
	}
	content, _ := io.ReadAll(page.Body)
	if !bytes.Contains(content, []byte("Agent Workspace")) || !bytes.Contains(content, []byte("Hermetrix Engine")) {
		t.Fatal("embedded UI was not served")
	}
	logo, err := http.Get(server.URL + "/assets/brand/hermetrix-favicon-v3-48.png")
	if err != nil {
		t.Fatal(err)
	}
	defer logo.Body.Close()
	logoBytes, _ := io.ReadAll(logo.Body)
	if logo.StatusCode != http.StatusOK || logo.Header.Get("Content-Type") != "image/png" || len(logoBytes) < 1000 {
		t.Fatalf("brand asset response: status=%d type=%q bytes=%d", logo.StatusCode, logo.Header.Get("Content-Type"), len(logoBytes))
	}
}

func TestHTTPMCPDiscoveryAndDeferredCatalog(t *testing.T) {
	var calls int
	mcpRuntime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode MCP request: %v", err)
			return
		}
		if r.Header.Get("MCP-Protocol-Version") != mcp.ProtocolCurrent || r.Header.Get("Mcp-Method") != request.Method {
			http.Error(w, "missing MCP metadata", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"resultType":"complete","tools":[{"name":"web_echo","description":"HTTP catalog test","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}}]}}`, request.ID)
	}))
	defer mcpRuntime.Close()
	harness := testHTTPServer(t)
	created := requestJSON(t, harness.URL+"/api/mcp/servers", http.MethodPost, map[string]any{
		"name": "web-mcp", "transport_kind": "streamable-http", "endpoint": mcpRuntime.URL,
		"protocol_mode": mcp.ProtocolCurrent, "trust_annotations": true, "request_timeout_ms": 5000,
	}, http.StatusCreated)
	var profile mcp.Server
	if err := json.Unmarshal(created, &profile); err != nil {
		t.Fatal(err)
	}
	discovered := requestJSON(t, harness.URL+"/api/mcp/servers/"+profile.ID+"/discover", http.MethodPost, map[string]any{}, http.StatusOK)
	if !bytes.Contains(discovered, []byte(`"tools":1`)) || calls != 1 {
		t.Fatalf("discovery=%s calls=%d", discovered, calls)
	}
	response, err := http.Get(harness.URL + "/api/capabilities?query=web_echo&limit=5")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var search struct {
		Results []capabilities.SearchResult `json:"results"`
	}
	if err := json.NewDecoder(response.Body).Decode(&search); err != nil {
		t.Fatal(err)
	}
	if len(search.Results) != 1 {
		t.Fatalf("search=%+v", search)
	}
	described, err := http.Get(harness.URL + "/api/capabilities/" + search.Results[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	defer described.Body.Close()
	var entry capabilities.Entry
	if err := json.NewDecoder(described.Body).Decode(&entry); err != nil {
		t.Fatal(err)
	}
	if entry.Revision == "" || !bytes.Contains(entry.InputSchema, []byte(`"type":"object"`)) {
		t.Fatalf("describe=%+v", entry)
	}
}

func TestHTTPProposalPromotionAndContextCompilation(t *testing.T) {
	server := testHTTPServer(t)
	input := map[string]any{"canonical_name": "http-skill", "scope_kind": "user", "origin": "user_created",
		"owner": "user", "change_kind": "create", "created_by": "user", "trigger_kind": "manual",
		"reason": "HTTP integration", "evidence_refs": []string{"test:http"}, "files": []any{},
		"markdown": "---\nname: http-skill\ndescription: \"HTTP integration skill\"\ntags: []\ntools: []\n---\n\n# Procedure\n\n1. Verify the response.\n"}
	created := requestJSON(t, server.URL+"/api/candidates", http.MethodPost, input, http.StatusCreated)
	var candidate skills.Candidate
	if err := json.Unmarshal(created, &candidate); err != nil {
		t.Fatal(err)
	}
	promoted := requestJSON(t, server.URL+"/api/candidates/"+candidate.ID+"/promote", http.MethodPost,
		map[string]any{"actor": "user", "expected_revision": candidate.Revision}, http.StatusOK)
	var skill skills.Skill
	if err := json.Unmarshal(promoted, &skill); err != nil {
		t.Fatal(err)
	}
	if skill.State != skills.StateActive {
		t.Fatalf("skill state = %s", skill.State)
	}
	compiled := requestJSON(t, server.URL+"/api/context/compile", http.MethodPost, map[string]any{
		"profile_name": "compact-32k", "fragments": []map[string]any{{"id": "goal", "kind": "user_goal",
			"scope": "session", "provenance": "user", "trust": "user", "version": "v1", "priority": 100,
			"pinned": true, "cache_class": "session", "content": "preserve me"}},
		"direct_tools": []any{}, "worst_case_tool_burst": 512}, http.StatusOK)
	if !bytes.Contains(compiled, []byte(`"profile":"compact-32k"`)) {
		t.Fatalf("unexpected context response %s", compiled)
	}
}

func TestHTTPLocalModelProbeUsesRuntimeAllocation(t *testing.T) {
	modelRuntime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			_, _ = io.WriteString(w, `{"models":[{"model":"agent-model","context_length":65536}]}`)
		case "/api/show":
			_, _ = io.WriteString(w, `{"parameters":"num_ctx 32768\n","model_info":{"qwen.context_length":131072}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer modelRuntime.Close()
	harness := testHTTPServer(t)
	body := requestJSON(t, harness.URL+"/api/local-model/probe", http.MethodPost,
		map[string]any{"runtime": "ollama", "endpoint": modelRuntime.URL, "model": "agent-model"}, http.StatusOK)
	var result localmodel.Result
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Verified || result.AllocatedContext != 65536 || result.ConfiguredContext != 32768 || result.Mode != "certified-context" {
		t.Fatalf("probe result = %+v", result)
	}
}

func TestHTTPProviderSessionAndStreamingTurn(t *testing.T) {
	t.Setenv("HERMETRIX_WEB_TEST_KEY", "web-runtime-secret")
	modelRuntime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer web-runtime-secret" {
			http.Error(w, "bad binding", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ready\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":1,\"total_tokens\":21}}\n\ndata: [DONE]\n\n")
	}))
	defer modelRuntime.Close()
	harness := testHTTPServer(t)
	createdProvider := requestJSON(t, harness.URL+"/api/providers", http.MethodPost, map[string]any{
		"name": "web-test", "adapter_kind": "openai-compatible", "base_url": modelRuntime.URL + "/v1",
		"model": "qwen-test", "api_key_env": "HERMETRIX_WEB_TEST_KEY", "context_window": 131072,
		"context_evidence": "declared", "max_output_tokens": 4096}, http.StatusCreated)
	var provider providers.Profile
	if err := json.Unmarshal(createdProvider, &provider); err != nil {
		t.Fatal(err)
	}
	skillCandidateBody := requestJSON(t, harness.URL+"/api/candidates", http.MethodPost, map[string]any{
		"canonical_name": "runtime-skill", "scope_kind": "user", "origin": "user_created", "owner": "user",
		"change_kind": "create", "created_by": "user", "trigger_kind": "manual", "reason": "runtime activation test",
		"evidence_refs": []string{"test:runtime"}, "files": []any{}, "markdown": "---\nname: runtime-skill\ndescription: \"Guide runtime verification\"\ntags: []\ntools: []\n---\n\n# Procedure\n\n1. Preserve runtime evidence.\n"}, http.StatusCreated)
	var skillCandidate skills.Candidate
	if err := json.Unmarshal(skillCandidateBody, &skillCandidate); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, harness.URL+"/api/candidates/"+skillCandidate.ID+"/promote", http.MethodPost,
		map[string]any{"actor": "user", "expected_revision": skillCandidate.Revision}, http.StatusOK)
	createdSession := requestJSON(t, harness.URL+"/api/sessions", http.MethodPost, map[string]any{
		"title": "HTTP stream", "provider_id": provider.ID, "context_profile": "extended-128k",
		"qualification_override": map[string]any{"actor": "test", "reason": "reviewed HTTP test fixture"}}, http.StatusCreated)
	var session agent.Session
	if err := json.Unmarshal(createdSession, &session); err != nil {
		t.Fatal(err)
	}
	requestBody, _ := json.Marshal(map[string]any{"content": "use runtime-skill and say ready"})
	response, err := http.Post(harness.URL+"/api/sessions/"+session.ID+"/turns", "application/json", bytes.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	stream, _ := io.ReadAll(response.Body)
	for _, marker := range []string{`"type":"user_committed"`, `"type":"step_bound"`, `"content":"ready"`, `"type":"completed"`} {
		if !bytes.Contains(stream, []byte(marker)) {
			t.Fatalf("stream missing %s: %s", marker, stream)
		}
	}
	detail, err := http.Get(harness.URL + "/api/sessions/" + session.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer detail.Body.Close()
	detailBody, _ := io.ReadAll(detail.Body)
	if !bytes.Contains(detailBody, []byte(`"event_kind":"model_step_bound"`)) || !bytes.Contains(detailBody, []byte(`"role":"assistant"`)) {
		t.Fatalf("session detail missing durable events: %s", detailBody)
	}
	skillsResponse, err := http.Get(harness.URL + "/api/skills")
	if err != nil {
		t.Fatal(err)
	}
	defer skillsResponse.Body.Close()
	skillsBody, _ := io.ReadAll(skillsResponse.Body)
	if !bytes.Contains(skillsBody, []byte(`"injected_count":1`)) {
		t.Fatalf("runtime skill activation receipt missing: %s", skillsBody)
	}
}

func TestEndToEndAgentSearchDescribeAndCallRealMCPServer(t *testing.T) {
	var mcpListCalls, mcpToolCalls, modelSteps int
	mcpRuntime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode MCP: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "tools/list":
			mcpListCalls++
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"resultType":"complete","tools":[{"name":"e2e_echo","description":"Return deterministic E2E evidence","inputSchema":{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]},"annotations":{"readOnlyHint":true,"openWorldHint":false}}]}}`, request.ID)
		case "tools/call":
			mcpToolCalls++
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"resultType":"complete","content":[{"type":"text","text":"MCP_ECHO_OK"}],"isError":false}}`, request.ID)
		default:
			http.Error(w, "unexpected MCP method", http.StatusBadRequest)
		}
	}))
	defer mcpRuntime.Close()

	writeToolCall := func(w http.ResponseWriter, id, name, arguments string) {
		payload := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
			map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": arguments}},
		}}, "finish_reason": "tool_calls"}}}
		encoded, _ := json.Marshal(payload)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", encoded)
	}
	modelRuntime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelSteps++
		var request struct {
			Messages []providers.Message        `json:"messages"`
			Tools    []providers.ToolDefinition `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
			return
		}
		// The point of the deferred catalog is that a 1,500-tool catalog does
		// not reach the prompt. Assert the ceiling, not today's exact number.
		if len(request.Tools) > 8 {
			t.Errorf("direct tool prompt grew with the catalog: %d tools", len(request.Tools))
		}
		for _, tool := range request.Tools {
			if strings.HasPrefix(tool.Function.Name, "mcp") || strings.Contains(tool.Function.Name, "fixture") {
				t.Errorf("a catalog capability leaked into the direct prompt: %s", tool.Function.Name)
			}
		}
		var last toolruntime.Receipt
		for i := len(request.Messages) - 1; i >= 0; i-- {
			if request.Messages[i].Role == "tool" {
				_ = json.Unmarshal([]byte(request.Messages[i].Content), &last)
				break
			}
		}
		switch modelSteps {
		case 1:
			writeToolCall(w, "call-search", "tool_search", `{"query":"e2e_echo","source":"mcp","limit":5}`)
		case 2:
			var search struct {
				Results []capabilities.SearchResult `json:"results"`
			}
			if err := json.Unmarshal([]byte(last.Output), &search); err != nil || len(search.Results) != 1 {
				t.Errorf("search receipt=%+v err=%v", last, err)
				return
			}
			arguments, _ := json.Marshal(map[string]any{"capability_id": search.Results[0].ID})
			writeToolCall(w, "call-describe", "tool_describe", string(arguments))
		case 3:
			var entry capabilities.Entry
			if err := json.Unmarshal([]byte(last.Output), &entry); err != nil || entry.Revision == "" {
				t.Errorf("describe receipt=%+v err=%v", last, err)
				return
			}
			arguments, _ := json.Marshal(map[string]any{"capability_id": entry.ID, "revision": entry.Revision,
				"arguments": map[string]any{"text": "hello"}})
			writeToolCall(w, "call-mcp", "tool_call", string(arguments))
		case 4:
			if last.Status != "succeeded" || !strings.Contains(last.Output, "MCP_ECHO_OK") || last.Metadata["automatic_retry"] != false {
				t.Errorf("MCP receipt = %+v", last)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"E2E complete\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			http.Error(w, "too many model steps", http.StatusInternalServerError)
		}
	}))
	defer modelRuntime.Close()

	harness := testHTTPServer(t)
	mcpProfileBody := requestJSON(t, harness.URL+"/api/mcp/servers", http.MethodPost, map[string]any{
		"name": "e2e-mcp", "transport_kind": "streamable-http", "endpoint": mcpRuntime.URL,
		"protocol_mode": mcp.ProtocolCurrent, "trust_annotations": true, "request_timeout_ms": 5000,
	}, http.StatusCreated)
	var mcpProfile mcp.Server
	if err := json.Unmarshal(mcpProfileBody, &mcpProfile); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, harness.URL+"/api/mcp/servers/"+mcpProfile.ID+"/discover", http.MethodPost, map[string]any{}, http.StatusOK)
	providerBody := requestJSON(t, harness.URL+"/api/providers", http.MethodPost, map[string]any{
		"name": "e2e-model", "adapter_kind": "openai-compatible", "base_url": modelRuntime.URL + "/v1",
		"model": "deterministic-provider", "context_window": 131072, "context_evidence": "qualified", "max_output_tokens": 4096,
	}, http.StatusCreated)
	var provider providers.Profile
	if err := json.Unmarshal(providerBody, &provider); err != nil {
		t.Fatal(err)
	}
	sessionBody := requestJSON(t, harness.URL+"/api/sessions", http.MethodPost, map[string]any{
		"title": "MCP E2E", "provider_id": provider.ID, "context_profile": "certified-64k",
		"qualification_override": map[string]any{"actor": "test", "reason": "reviewed MCP E2E fixture"},
	}, http.StatusCreated)
	var session agent.Session
	if err := json.Unmarshal(sessionBody, &session); err != nil {
		t.Fatal(err)
	}
	requestBody, _ := json.Marshal(map[string]any{"content": "Find and call the E2E echo MCP capability"})
	response, err := http.Post(harness.URL+"/api/sessions/"+session.ID+"/turns", "application/json", bytes.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	stream, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !bytes.Contains(stream, []byte(`"content":"E2E complete"`)) || !bytes.Contains(stream, []byte(`"type":"completed"`)) {
		t.Fatalf("E2E stream status=%d body=%s", response.StatusCode, stream)
	}
	if modelSteps != 4 || mcpListCalls != 1 || mcpToolCalls != 1 {
		t.Fatalf("steps=%d list=%d call=%d", modelSteps, mcpListCalls, mcpToolCalls)
	}
	detail, err := http.Get(harness.URL + "/api/sessions/" + session.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer detail.Body.Close()
	detailBody, _ := io.ReadAll(detail.Body)
	for _, proof := range []string{"tool_search", "tool_describe", "tool_call", "MCP_ECHO_OK", "capability_revision"} {
		if !bytes.Contains(detailBody, []byte(proof)) {
			t.Fatalf("durable E2E history missing %s: %s", proof, detailBody)
		}
	}
}

func TestHTTPSkillReplayCapabilityReviewAndFidelityEvidence(t *testing.T) {
	harness := testHTTPServer(t)
	markdown := "---\nname: replay-http\ndescription: \"Preserve exact replay behavior across revisions\"\ntags: [replay]\ntools: []\n---\n\n# Procedure\n\n1. Preserve exact replay behavior.\n"
	created := requestJSON(t, harness.URL+"/api/candidates", http.MethodPost, map[string]any{
		"canonical_name": "replay-http", "scope_kind": "user", "origin": "user_created", "owner": "user",
		"change_kind": "create", "created_by": "user", "trigger_kind": "manual", "reason": "HTTP replay test",
		"evidence_refs": []string{"test:http-replay"}, "markdown": markdown, "files": []any{}}, http.StatusCreated)
	var candidate skills.Candidate
	if err := json.Unmarshal(created, &candidate); err != nil {
		t.Fatal(err)
	}
	promoted := requestJSON(t, harness.URL+"/api/candidates/"+candidate.ID+"/promote", http.MethodPost,
		map[string]any{"actor": "user", "expected_revision": candidate.Revision}, http.StatusOK)
	var active skills.Skill
	if err := json.Unmarshal(promoted, &active); err != nil {
		t.Fatal(err)
	}
	improvementBody := requestJSON(t, harness.URL+"/api/skills/"+active.ID+"/improvements", http.MethodPost,
		map[string]any{"actor": "user", "reason": "add an explicitly reviewed read capability"}, http.StatusCreated)
	var improvement skills.Candidate
	if err := json.Unmarshal(improvementBody, &improvement); err != nil {
		t.Fatal(err)
	}
	updatedMarkdown := strings.Replace(markdown, "tools: []", "tools: [filesystem.read]", 1) + "\n2. Read the exact evidence.\n"
	updatedBody := requestJSON(t, harness.URL+"/api/candidates/"+improvement.ID, http.MethodPatch,
		map[string]any{"markdown": updatedMarkdown, "actor": "user", "expected_revision": improvement.Revision}, http.StatusOK)
	var updated skills.Candidate
	if err := json.Unmarshal(updatedBody, &updated); err != nil {
		t.Fatal(err)
	}
	replays, err := http.Get(harness.URL + "/api/candidates/" + updated.ID + "/replays")
	if err != nil {
		t.Fatal(err)
	}
	defer replays.Body.Close()
	replayBody, _ := io.ReadAll(replays.Body)
	if replays.StatusCode != http.StatusOK || !bytes.Contains(replayBody, []byte(`"added_tools":["filesystem.read"]`)) ||
		!bytes.Contains(replayBody, []byte(`"candidate_revision":2`)) {
		t.Fatalf("replay body=%s", replayBody)
	}
	requestJSON(t, harness.URL+"/api/candidates/"+updated.ID+"/capability-review", http.MethodPost,
		map[string]any{"actor": "user", "decision": "approve", "expected_revision": updated.Revision}, http.StatusOK)
	requestJSON(t, harness.URL+"/api/candidates/"+updated.ID+"/promote", http.MethodPost,
		map[string]any{"actor": "user", "expected_revision": updated.Revision}, http.StatusOK)
	casesResponse, err := http.Get(harness.URL + "/api/fidelity/cases")
	if err != nil {
		t.Fatal(err)
	}
	var cases []fidelity.Case
	if err := json.NewDecoder(casesResponse.Body).Decode(&cases); err != nil {
		t.Fatal(err)
	}
	casesResponse.Body.Close()
	if len(cases) < 2 {
		t.Fatalf("seeded fidelity cases=%d", len(cases))
	}
	run := requestJSON(t, harness.URL+"/api/fidelity/cases/"+cases[0].ID+"/run", http.MethodPost,
		map[string]any{"profile_name": "compact-32k"}, http.StatusOK)
	if !bytes.Contains(run, []byte(`"essential_exact_retention"`)) || !bytes.Contains(run, []byte(`"hallucination_count"`)) {
		t.Fatalf("fidelity run=%s", run)
	}
}

func TestHTTPProjectCommandArtifactsBackupAndMaintenance(t *testing.T) {
	harness := testHTTPServer(t)
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "evidence.txt"), []byte("real HTTP evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectBody := requestJSON(t, harness.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "HTTP project", "root_path": projectRoot}, http.StatusCreated)
	var project product.Project
	if err := json.Unmarshal(projectBody, &project); err != nil {
		t.Fatal(err)
	}
	files, err := http.Get(harness.URL + "/api/projects/" + project.ID + "/files?path=")
	if err != nil {
		t.Fatal(err)
	}
	filesBody, _ := io.ReadAll(files.Body)
	files.Body.Close()
	if !bytes.Contains(filesBody, []byte("evidence.txt")) {
		t.Fatalf("files=%s", filesBody)
	}
	jobBody := requestJSON(t, harness.URL+"/api/projects/"+project.ID+"/commands", http.MethodPost,
		map[string]any{"actor": "user", "executable": "python3", "arguments": []string{"-c", "print('HTTP_JOB_OK')"},
			"working_dir": ".", "timeout_seconds": 10}, http.StatusAccepted)
	var job product.Job
	if err := json.Unmarshal(jobBody, &job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		jobsResponse, err := http.Get(harness.URL + "/api/jobs")
		if err != nil {
			t.Fatal(err)
		}
		var jobs []product.Job
		if err := json.NewDecoder(jobsResponse.Body).Decode(&jobs); err != nil {
			jobsResponse.Body.Close()
			t.Fatal(err)
		}
		jobsResponse.Body.Close()
		for _, current := range jobs {
			if current.ID == job.ID && current.State == "completed" {
				if !strings.Contains(current.Result["output"].(string), "HTTP_JOB_OK") || current.Result["artifact_id"] == "" {
					t.Fatalf("job=%+v", current)
				}
				goto commandComplete
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("HTTP background command did not complete")
		}
		time.Sleep(20 * time.Millisecond)
	}
commandComplete:
	backupBody := requestJSON(t, harness.URL+"/api/backups", http.MethodPost, map[string]any{"actor": "user"}, http.StatusCreated)
	var backup product.BackupRun
	if err := json.Unmarshal(backupBody, &backup); err != nil {
		t.Fatal(err)
	}
	download, err := http.Get(harness.URL + "/api/backups/" + backup.ID + "/download")
	if err != nil {
		t.Fatal(err)
	}
	backupData, _ := io.ReadAll(download.Body)
	download.Body.Close()
	if download.StatusCode != http.StatusOK || download.Header.Get("X-Content-Checksum") == "" {
		t.Fatalf("download status=%d", download.StatusCode)
	}
	previewRequest, _ := http.NewRequest(http.MethodPost, harness.URL+"/api/imports/preview?actor=user", bytes.NewReader(backupData))
	previewRequest.Header.Set("Content-Type", "application/vnd.hermetrix.backup+json")
	previewResponse, err := http.DefaultClient.Do(previewRequest)
	if err != nil {
		t.Fatal(err)
	}
	var preview product.ImportPreview
	if err := json.NewDecoder(previewResponse.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	previewResponse.Body.Close()
	if previewResponse.StatusCode != http.StatusCreated || preview.State != "awaiting_apply" {
		t.Fatalf("preview=%+v status=%d", preview, previewResponse.StatusCode)
	}
	requestJSON(t, harness.URL+"/api/imports/"+preview.ID+"/apply", http.MethodPost,
		map[string]any{"actor": "user"}, http.StatusOK)
	gcBody := requestJSON(t, harness.URL+"/api/maintenance/gc/dry-run", http.MethodPost, map[string]any{}, http.StatusCreated)
	var gc curator.GCRun
	if err := json.Unmarshal(gcBody, &gc); err != nil || gc.State != "planned" {
		t.Fatalf("gc=%+v err=%v", gc, err)
	}
	requestJSON(t, harness.URL+"/api/maintenance/gc/"+gc.ID+"/apply", http.MethodPost,
		map[string]any{"actor": "user"}, http.StatusOK)
}

func requestJSON(t *testing.T, url, method string, value any, wantStatus int) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(method, url, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s: status=%d body=%s", method, url, response.StatusCode, strings.TrimSpace(string(body)))
	}
	return body
}

// R-14: the ADR-7 exit criterion has to be reachable, not just computable.
func TestSkillRetrievalMetricsAreServed(t *testing.T) {
	server := testHTTPServer(t)
	response, err := server.Client().Get(server.URL + "/api/skill-retrieval")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("skill retrieval metrics returned %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var stats []agent.SkillRetrievalStats
	if err := json.Unmarshal(body, &stats); err != nil {
		t.Fatalf("decode metrics: %v (%s)", err, body)
	}
	if stats == nil {
		t.Fatal("metrics must serialise as an array, never null")
	}
}

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
	"hermetrix-harness/internal/secrets"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	toolruntime "hermetrix-harness/internal/tools"
)

func testHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(testHandler(t))
	t.Cleanup(server.Close)
	return server
}

type webTeamAgent struct{}

func (webTeamAgent) CreateSession(_ context.Context, input agent.CreateSessionInput) (agent.Session, error) {
	return agent.Session{ID: "web-child-" + strings.ReplaceAll(input.Title, " ", "-"), Title: input.Title}, nil
}

func (webTeamAgent) RunTurn(_ context.Context, _ string, input agent.TurnInput, _ func(agent.StreamEvent) error) (agent.TurnResult, error) {
	return agent.TurnResult{AssistantEvent: agent.Event{Content: "web handler result for " + input.Content},
		Usage: providers.Usage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18}}, nil
}

func (webTeamAgent) DecideApproval(_ context.Context, _ string, _ agent.ApprovalDecisionInput,
	_ func(agent.StreamEvent) error) (agent.TurnResult, error) {
	return agent.TurnResult{AssistantEvent: agent.Event{Content: "web approval resolved"},
		Usage: providers.Usage{PromptTokens: 13, CompletionTokens: 8, TotalTokens: 21}}, nil
}

func testHandler(t *testing.T) http.Handler {
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
	productService := product.NewService(dataStore, skillService).WithAgentRunner(webTeamAgent{})
	t.Cleanup(productService.Close)
	vault, err := secrets.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	providerService := providers.NewService(dataStore, nil).WithVault(vault)
	toolRegistry, err := toolruntime.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	capabilityCatalog := capabilities.NewCatalog()
	mcpService := mcp.NewService(dataStore, capabilityCatalog, nil).WithVault(vault)
	toolRegistry.SetCatalog(capabilityCatalog)
	agentService := agent.NewService(dataStore, providerService, compiler, estimator, gate, toolRegistry, skillService).WithLearning(learningService)
	fidelityService := fidelity.NewService(dataStore, compiler)
	if err := fidelityService.EnsureDefaultCorpus(context.Background()); err != nil {
		t.Fatal(err)
	}
	qualificationService := qualification.NewService(dataStore, providerService, localmodel.NewProber(), gate, estimator)
	return New(skillService, learningService, curatorService, compiler, estimator,
		localmodel.NewProber(), providerService, agentService, dataStore, logger).WithMCP(mcpService, capabilityCatalog).
		WithFidelity(fidelityService).WithQualification(qualificationService).WithProduct(productService).Handler()
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

func requestHandlerJSON(t *testing.T, handler http.Handler, path, method string, payload any, status int) []byte {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, body)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != status {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, recorder.Code, status, recorder.Body.String())
	}
	return recorder.Body.Bytes()
}

func TestWorkbenchRoutesReachRealStorageAndCustomTeamDAGWithoutAListener(t *testing.T) {
	handler := testHandler(t)
	root := t.TempDir()
	createdProject := requestHandlerJSON(t, handler, "/api/projects", http.MethodPost,
		map[string]any{"name": "No-listener workbench", "root_path": root}, http.StatusCreated)
	var project product.Project
	if err := json.Unmarshal(createdProject, &project); err != nil {
		t.Fatal(err)
	}
	written := requestHandlerJSON(t, handler, "/api/projects/"+project.ID+"/file", http.MethodPut,
		map[string]any{"path": "plan.md", "content": "verified through ServeMux", "actor": "test-user"}, http.StatusOK)
	var fileResult product.WriteFileResult
	if err := json.Unmarshal(written, &fileResult); err != nil {
		t.Fatal(err)
	}
	if fileResult.Document.SHA256 == "" || fileResult.ReceiptArtifact.ID == "" {
		t.Fatalf("file write lacks optimistic revision or receipt: %+v", fileResult)
	}

	deliverableBytes := requestHandlerJSON(t, handler, "/api/deliverables", http.MethodPost,
		map[string]any{"project_id": project.ID, "format": "docx", "title": "Route proof", "actor": "test-user",
			"paragraphs": []string{"Generated through the real router and CAS."}}, http.StatusCreated)
	var deliverable product.Artifact
	if err := json.Unmarshal(deliverableBytes, &deliverable); err != nil {
		t.Fatal(err)
	}
	contentRequest := httptest.NewRequest(http.MethodGet, "/api/artifacts/"+deliverable.ID+"/content", nil)
	contentRecorder := httptest.NewRecorder()
	handler.ServeHTTP(contentRecorder, contentRequest)
	if contentRecorder.Code != http.StatusOK || !bytes.HasPrefix(contentRecorder.Body.Bytes(), []byte("PK")) {
		t.Fatalf("DOCX route did not return a real OOXML package: status=%d prefix=%q", contentRecorder.Code, contentRecorder.Body.Bytes()[:min(8, contentRecorder.Body.Len())])
	}

	teamBytes := requestHandlerJSON(t, handler, "/api/teams", http.MethodPost, map[string]any{
		"project_id": project.ID, "name": "Custom graph team", "instructions": "Keep evidence and authority isolated.", "actor": "test-user",
		"members": []map[string]any{
			{"name": "Scout", "role": "research", "instructions": "Gather evidence.", "is_lead": false},
			{"name": "Lead", "role": "synthesis", "instructions": "Synthesize only labelled evidence.", "is_lead": true},
		},
	}, http.StatusCreated)
	var team product.AgentTeam
	if err := json.Unmarshal(teamBytes, &team); err != nil {
		t.Fatal(err)
	}
	runBytes := requestHandlerJSON(t, handler, "/api/team-runs", http.MethodPost, map[string]any{
		"team_id": team.ID, "project_id": project.ID, "objective": "Exercise a custom dependency graph", "provider_id": "fake-provider",
		"context_profile": "certified-64k", "actor": "test-user", "max_parallel": 2,
		"tasks": []map[string]any{
			{"id": "evidence", "member_id": team.Members[0].ID, "title": "Evidence", "prompt": "Collect evidence."},
			{"id": "synthesis", "member_id": team.Members[1].ID, "title": "Synthesis", "prompt": "Synthesize evidence.", "depends_on": []string{"evidence"}},
		},
	}, http.StatusAccepted)
	var run product.TeamRun
	if err := json.Unmarshal(runBytes, &run); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		runBytes = requestHandlerJSON(t, handler, "/api/team-runs/"+run.ID, http.MethodGet, nil, http.StatusOK)
		if err := json.Unmarshal(runBytes, &run); err != nil {
			t.Fatal(err)
		}
		if run.State == "completed" || run.State == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("custom DAG did not finish through HTTP handler: %+v", run)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.State != "completed" || len(run.Tasks) != 2 || run.PromptTokens != 22 || run.CompletionTokens != 14 {
		t.Fatalf("custom DAG result=%+v", run)
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
	// One discovery asks for all three catalogs a server can publish: tools,
	// resources and prompts. A server that implements only tools answers the
	// other two with "method not found", which is not a failure.
	if !bytes.Contains(discovered, []byte(`"tools":1`)) || calls != 3 {
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
		// Raised to 11 when skill_manage joined the waist.
		// Raised to 12 when workspace.run joined the waist.
		if len(request.Tools) > 12 {
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
		"evidence_refs": []string{"test:http-replay"}, "markdown": markdown,
		// A replay that only confirmed the manifest no longer allows promotion,
		// so the Skill this flow promotes carries a fixture the way a real one
		// would.
		// File.Content is []byte, so it travels as base64 over JSON.
		"files": []any{map[string]any{"path": "tests/replay.json", "content": "eyJpZCI6InJlcGxheSIsInByb21wdCI6IndoYXQgZG9lcyB0aGUgcHJvY2VkdXJlIHByZXNlcnZlIiwicmVxdWlyZWRfcGhyYXNlcyI6WyJyZXBsYXkiXX0="}}},
		http.StatusCreated)
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
	// Promotion of an improvement is gated on a passing behavioral evaluation,
	// so the HTTP surface has to be able to record one -- without this route an
	// operator could never promote an improvement through the API at all.
	requestJSON(t, harness.URL+"/api/candidates/"+updated.ID+"/behavioral-eval", http.MethodPost,
		map[string]any{"tasks": 5, "baseline_passed": 5, "candidate_passed": 5}, http.StatusCreated)
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

// O-12: an API path that matches no route must not answer with the SPA. A
// typo or a wrong method used to return 200 and HTML, which a client reads as
// success.
func TestUnmatchedAPIRoutesReturnJSONNotFound(t *testing.T) {
	server := testHTTPServer(t)
	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/does-not-exist"},
		{http.MethodGet, "/api/activations"}, // real route, GET is not registered
		{http.MethodPost, "/api/usage"},      // real route, wrong method
	}
	for _, testCase := range cases {
		request, err := http.NewRequest(testCase.method, server.URL+testCase.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s returned %d, want 404", testCase.method, testCase.path, response.StatusCode)
		}
		if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "application/json") {
			t.Fatalf("%s %s answered %s, want JSON", testCase.method, testCase.path, contentType)
		}
		if strings.Contains(string(body), "<!doctype") {
			t.Fatalf("%s %s answered with the SPA:\n%s", testCase.method, testCase.path, body[:80])
		}
	}
}

// TestHealthReportsTheSchemaTheDatabaseActuallyHas covers a health endpoint
// that answered with a hardcoded 16 while the database had migrated to 17.
// Health is what a client reads to decide whether the server is the one it
// expects, so a number typed by hand is worse than no number.
func TestHealthReportsTheSchemaTheDatabaseActuallyHas(t *testing.T) {
	server := testHTTPServer(t)
	response, err := http.Get(server.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body struct {
		OK             bool `json:"ok"`
		Schema         int  `json:"schema"`
		ExpectedSchema int  `json:"expected_schema"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.OK {
		t.Fatal("health reported not ok")
	}
	if body.ExpectedSchema != store.CurrentSchemaVersion {
		t.Fatalf("expected_schema = %d, build targets %d", body.ExpectedSchema, store.CurrentSchemaVersion)
	}
	// The reported version must come from the database, not from the same
	// constant the expectation came from: a freshly opened store has migrated,
	// so the two agree here, and they must agree for that reason.
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	fromDatabase, err := dataStore.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if body.Schema != fromDatabase {
		t.Fatalf("health reported schema %d, PRAGMA user_version = %d", body.Schema, fromDatabase)
	}
	if body.Schema != store.CurrentSchemaVersion {
		t.Fatalf("migrated database is at %d, build targets %d", body.Schema, store.CurrentSchemaVersion)
	}
}

// TestStoredArtifactContentCannotRunAsAPage covers the one route that returns
// bytes a caller supplied, under a MIME type the same caller chose.
//
// GET /api/artifacts/{id}/content writes the stored blob with
// Content-Type: <whatever was posted> and Content-Disposition: inline. Post
// text/html and the browser is being handed a page on the app's own origin --
// the origin that also serves an API with no authentication, because the
// listener is loopback-only. Nothing in the corpus ever created an artifact, so
// this route had never been exercised at all.
//
// It is in fact defended: securityHeaders wraps the whole mux, so the response
// carries a CSP without unsafe-inline and X-Content-Type-Options: nosniff. That
// defence was untested here -- the existing check reads the CSP off the UI page
// only, which would keep passing if raw content were ever served from a mux
// that skipped the middleware.
func TestStoredArtifactContentCannotRunAsAPage(t *testing.T) {
	server := testHTTPServer(t)
	defer server.Close()

	payload := `{"name":"probe.html","kind":"report","mime_type":"text/html",` +
		`"content":"<script>alert(1)</script><b>hi</b>"}`
	created, err := http.Post(server.URL+"/api/artifacts", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(created.Body)
		t.Fatalf("create artifact: %d %s", created.StatusCode, body)
	}
	var artifact struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(created.Body).Decode(&artifact); err != nil {
		t.Fatal(err)
	}

	fetched, err := http.Get(server.URL + "/api/artifacts/" + artifact.ID + "/content")
	if err != nil {
		t.Fatal(err)
	}
	defer fetched.Body.Close()

	// The premise: the route really does hand back the caller's HTML under the
	// caller's content type. If either stops being true this test is guarding
	// something that no longer exists.
	if got := fetched.Header.Get("Content-Type"); got != "text/html" {
		t.Fatalf("premise changed: content type is %q", got)
	}
	body, _ := io.ReadAll(fetched.Body)
	if !bytes.Contains(body, []byte("<script>")) {
		t.Fatalf("premise changed: the stored markup was altered: %s", body)
	}

	// The guarantee.
	policy := fetched.Header.Get("Content-Security-Policy")
	if policy == "" {
		t.Fatal("stored content is served without a CSP; it executes on the app's own origin")
	}
	if !strings.Contains(policy, "script-src 'self'") || strings.Contains(policy, "unsafe-inline") {
		t.Fatalf("CSP would let stored markup execute: %q", policy)
	}
	if fetched.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("stored content is sniffable, so a benign MIME type is not binding")
	}
	if fetched.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatal("stored content can be framed by another page")
	}
}

// TestEmptyListEndpointsAnswerAnArrayNotNull pins the JSON contract the cockpit
// reads. A nil Go slice encodes as null, and on a fresh data directory
// /api/terminals, /api/browser/tabs, /api/teams and /api/team-runs all did:
// the cockpit's initial load threw on the first .length and every panel stayed
// blank, which looked like the server had failed rather than like there was
// nothing to list yet.
func TestEmptyListEndpointsAnswerAnArrayNotNull(t *testing.T) {
	server := testHTTPServer(t)
	for _, path := range []string{
		"/api/terminals", "/api/browser/tabs", "/api/teams", "/api/team-runs", "/api/jobs",
		"/api/artifacts", "/api/projects", "/api/settings", "/api/memories", "/api/backups",
		"/api/qualifications", "/api/fidelity/runs", "/api/maintenance/schedules", "/api/maintenance/gc",
		"/api/skill-authority/actions",
	} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if response.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200: %s", path, response.StatusCode, body)
			continue
		}
		var decoded []json.RawMessage
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Errorf("GET %s did not answer a JSON array: %s", path, bytes.TrimSpace(body))
		}
	}
}

// TestSavingAProviderWithAnAPIKeyStoresItWithoutEchoingIt covers the reason the
// vault exists: a token can be typed into the control center, it makes the
// provider ready immediately with no environment variable and no restart, and
// it never comes back out of the API.
func TestSavingAProviderWithAnAPIKeyStoresItWithoutEchoingIt(t *testing.T) {
	server := testHTTPServer(t)
	const token = "sk-typed-into-the-ui-9f2c"
	body := `{"name":"Typed key","adapter_kind":"openai-compatible","base_url":"https://host.example/v1",` +
		`"model":"m-1","context_window":131072,"max_output_tokens":8192,"api_key":"` + token + `"}`
	response, err := http.Post(server.URL+"/api/providers", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/providers = %d: %s", response.StatusCode, payload)
	}
	if bytes.Contains(payload, []byte(token)) {
		t.Fatal("the provider response echoed the API key back to the browser")
	}
	var created struct {
		ID               string `json:"id"`
		CredentialReady  bool   `json:"credential_ready"`
		CredentialStored bool   `json:"credential_stored"`
	}
	if err := json.Unmarshal(payload, &created); err != nil {
		t.Fatal(err)
	}
	if !created.CredentialReady || !created.CredentialStored {
		t.Fatalf("a saved API key did not make the provider ready: %+v", created)
	}

	// Listing must not leak it either, and clearing the key must take effect.
	listed, err := http.Get(server.URL + "/api/providers")
	if err != nil {
		t.Fatal(err)
	}
	listing, _ := io.ReadAll(listed.Body)
	listed.Body.Close()
	if bytes.Contains(listing, []byte(token)) {
		t.Fatal("the provider listing leaked the API key")
	}
	request, err := http.NewRequest(http.MethodPut,
		server.URL+"/api/providers/"+created.ID+"/credential", strings.NewReader(`{"api_key":""}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	cleared, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	clearedBody, _ := io.ReadAll(cleared.Body)
	cleared.Body.Close()
	if cleared.StatusCode != http.StatusOK {
		t.Fatalf("PUT credential = %d: %s", cleared.StatusCode, clearedBody)
	}
	var afterClear struct {
		CredentialStored bool `json:"credential_stored"`
	}
	if err := json.Unmarshal(clearedBody, &afterClear); err != nil {
		t.Fatal(err)
	}
	if afterClear.CredentialStored {
		t.Error("clearing the API key left a stored credential behind")
	}
}

// TestPickerDataChangesAndTellsTheTruth covers what the picker orders itself by,
// and the rule that it never shows a count for a subsystem that does not exist
// yet: a zero next to "tasks" would be a claim that there are no tasks, when the
// truth is that there is no task system.
func TestPickerDataChangesAndTellsTheTruth(t *testing.T) {
	server := testHTTPServer(t)
	created := requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Daily life"}, http.StatusCreated)
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created, &project); err != nil {
		t.Fatal(err)
	}

	pinned := requestJSON(t, server.URL+"/api/projects/"+project.ID+"/pin", http.MethodPut,
		map[string]any{"pinned": true}, http.StatusOK)
	if !bytes.Contains(pinned, []byte(`"pinned":true`)) {
		t.Fatalf("pin did not take: %s", pinned)
	}
	opened := requestJSON(t, server.URL+"/api/projects/"+project.ID+"/open", http.MethodPost,
		map[string]any{}, http.StatusOK)
	if !bytes.Contains(opened, []byte(`"last_opened_at"`)) {
		t.Fatalf("open did not record a time: %s", opened)
	}

	listing := requestJSON(t, server.URL+"/api/projects", http.MethodGet, nil, http.StatusOK)
	if !bytes.Contains(listing, []byte(`"session_count"`)) {
		t.Error("listing does not carry the one count that has a system behind it")
	}
	for _, absent := range []string{"task_count", "note_count"} {
		if bytes.Contains(listing, []byte(absent)) {
			t.Errorf("listing reports %s although no such system exists yet", absent)
		}
	}
}

// TestHTTPRefusesRootlessProjectFilesTerminalsCommandsAndBrowser is the HTTP
// half of the fix for the shell redesign's one Critical finding. The product
// layer now refuses files, terminal, command and browser operations against a
// project with no code folder, but a handler that let that refusal fall
// through writeError's default case would still answer 500, which tells an
// API caller nothing it can act on. Each route here has to come back as 422
// with a body that names the missing folder, matching what the product layer
// already asserts through errors.Is(err, product.ErrProjectHasNoCode).
func TestHTTPRefusesRootlessProjectFilesTerminalsCommandsAndBrowser(t *testing.T) {
	handler := testHandler(t)
	created := requestHandlerJSON(t, handler, "/api/projects", http.MethodPost,
		map[string]any{"name": "No folder yet"}, http.StatusCreated)
	var project product.Project
	if err := json.Unmarshal(created, &project); err != nil {
		t.Fatal(err)
	}
	if project.RootPath != "" {
		t.Fatalf("project unexpectedly has a root: %+v", project)
	}

	assertNamesMissingFolder := func(t *testing.T, body []byte) {
		t.Helper()
		var decoded struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatalf("error body was not JSON: %s", body)
		}
		if !strings.Contains(decoded.Error, "no code folder") {
			t.Fatalf("error did not name the missing folder: %q", decoded.Error)
		}
	}

	assertNamesMissingFolder(t, requestHandlerJSON(t, handler, "/api/projects/"+project.ID+"/files?path=",
		http.MethodGet, nil, http.StatusUnprocessableEntity))

	assertNamesMissingFolder(t, requestHandlerJSON(t, handler, "/api/projects/"+project.ID+"/file?path=notes.md",
		http.MethodGet, nil, http.StatusUnprocessableEntity))

	assertNamesMissingFolder(t, requestHandlerJSON(t, handler, "/api/projects/"+project.ID+"/file", http.MethodPut,
		map[string]any{"path": "notes.md", "content": "x", "actor": "test-user"}, http.StatusUnprocessableEntity))

	assertNamesMissingFolder(t, requestHandlerJSON(t, handler, "/api/projects/"+project.ID+"/commands", http.MethodPost,
		map[string]any{"actor": "user", "executable": "ls"}, http.StatusUnprocessableEntity))

	assertNamesMissingFolder(t, requestHandlerJSON(t, handler, "/api/terminals", http.MethodPost,
		map[string]any{"project_id": project.ID, "actor": "user", "shell": "sh"}, http.StatusUnprocessableEntity))

	// A file:// browser URL is refused before Chrome is ever launched, since
	// validateBrowserURL runs first: this route needs no browser binary to
	// exercise the refusal.
	assertNamesMissingFolder(t, requestHandlerJSON(t, handler, "/api/browser/tabs", http.MethodPost,
		map[string]any{"project_id": project.ID, "url": "file:///whatever", "actor": "user"}, http.StatusUnprocessableEntity))
}

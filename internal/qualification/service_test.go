package qualification

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/localmodel"
	"hermetrix-harness/internal/providers"
	hruntime "hermetrix-harness/internal/runtime"
	"hermetrix-harness/internal/store"
)

func qualificationRuntime(t *testing.T) *httptest.Server {
	t.Helper()
	return qualificationServerEchoingLabels(t, []string{"HEAD", "QUARTER", "MIDDLE", "THREE_QUARTER", "TAIL"})
}

// qualificationServerEchoingLabels models a runtime that can recover only
// the sentinels at the given positions, which is how a real model behaves
// when a tier holds its head but loses its middle.
func qualificationServerEchoingLabels(t *testing.T, labels []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			_, _ = fmt.Fprint(w, `{"models":[{"model":"qualified-model","context_length":65536}]}`)
			return
		case "/api/show":
			_, _ = fmt.Fprint(w, `{"parameters":"num_ctx 65536\n","model_info":{"qwen.context_length":131072}}`)
			return
		case "/v1/chat/completions":
			var request struct {
				Messages []providers.Message `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode provider request: %v", err)
				return
			}
			content := ""
			for _, message := range request.Messages {
				content += "\n" + message.Content
			}
			w.Header().Set("Content-Type", "text/event-stream")
			switch {
			case strings.Contains(content, "[QUALIFY:CONNECT]"):
				writeQualificationChunk(w, map[string]any{"content": "HERMETRIX_QUALIFIED_OK"}, "stop", 24, 4)
			case strings.Contains(content, "[QUALIFY:RECALL]"):
				// Echo back only the sentinels this probe actually planted, so
				// the fixture cannot pass a probe it never received.
				var recovered []string
				for _, label := range labels {
					marker := "HERMETRIX_SENTINEL_7F3A_" + label
					if strings.Contains(content, marker) {
						recovered = append(recovered, marker)
					}
				}
				writeQualificationChunk(w, map[string]any{"content": strings.Join(recovered, "\n")}, "stop", 43000, 5)
			case strings.Contains(content, "[QUALIFY:THAI_TOOL]"):
				writeToolChunk(w, []map[string]any{toolDelta(0, "thai", "qualification_echo", `{"text":"ภาษาไทย","mode":"safe"}`)})
			case strings.Contains(content, "[QUALIFY:SEQUENTIAL]"):
				writeToolChunk(w, []map[string]any{toolDelta(0, "first", "qualification_first", `{}`),
					toolDelta(1, "second", "qualification_second", `{}`)})
			case strings.Contains(content, "[QUALIFY:RECOVERY]"):
				writeToolChunk(w, []map[string]any{toolDelta(0, "recovered", "qualification_echo", `{"text":"recovered","mode":"safe"}`)})
			case strings.Contains(content, "[QUALIFY:DEFERRED]"):
				writeToolChunk(w, []map[string]any{toolDelta(0, "search", "tool_search", `{"query":"calendar"}`)})
			default:
				writeQualificationChunk(w, map[string]any{"content": "ok"}, "stop", 10, 1)
			}
		default:
			http.NotFound(w, r)
		}
	}))
}

func toolDelta(index int, id, name, arguments string) map[string]any {
	return map[string]any{"index": index, "id": id, "type": "function",
		"function": map[string]any{"name": name, "arguments": arguments}}
}

func writeToolChunk(w http.ResponseWriter, calls []map[string]any) {
	writeQualificationChunk(w, map[string]any{"tool_calls": calls}, "tool_calls", 40, 8)
}

func writeQualificationChunk(w http.ResponseWriter, delta map[string]any, finish string, prompt, completion int) {
	payload := map[string]any{"choices": []any{map[string]any{"delta": delta, "finish_reason": finish}},
		"usage": map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion}}
	encoded, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", encoded)
}

func TestRealHTTPQualificationCertifiesAllocationToolsAndRecall(t *testing.T) {
	runtimeServer := qualificationRuntime(t)
	defer runtimeServer.Close()
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	providerService := providers.NewService(dataStore, providers.NewOpenAIAdapter(runtimeServer.Client()))
	profile, err := providerService.Save(context.Background(), providers.SaveInput{Name: "qualified", BaseURL: runtimeServer.URL + "/v1",
		Model: "qualified-model", ContextWindow: 65536, ContextEvidence: "declared", MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(dataStore, providerService, localmodel.NewProberWithClient(runtimeServer.Client()),
		hruntime.NewInferenceGate(), ctxcompiler.NewAdaptiveEstimator())
	run, err := service.Run(context.Background(), Input{ProviderID: profile.ID, RequestedProfile: "certified-64k",
		RuntimeProbe: &localmodel.ProbeRequest{Runtime: "ollama", Endpoint: runtimeServer.URL, Model: "qualified-model"}})
	if err != nil {
		t.Fatal(err)
	}
	if run.ContextTier != "certified-64k" || run.CapabilityGrade != "A" || !run.Eligible || run.RequiresDecision ||
		!run.Results.LongContextRecall || !run.Results.ThaiEnglishSchema || !run.Results.ForegroundPreemption ||
		!run.Results.UsageCalibration || run.AllocatedContext != 65536 {
		t.Fatalf("qualification=%+v", run)
	}
	runs, err := service.List(context.Background(), 10)
	if err != nil || len(runs) != 1 || runs[0].RequestedProfile != "certified-64k" || !runs[0].Eligible {
		t.Fatalf("persisted runs=%+v err=%v", runs, err)
	}
}

func TestUnverifiedRuntimeNeverSilentlyCertifiesDeclaredContext(t *testing.T) {
	runtimeServer := qualificationRuntime(t)
	defer runtimeServer.Close()
	dataStore, _ := store.Open(context.Background(), t.TempDir())
	defer dataStore.Close()
	providerService := providers.NewService(dataStore, providers.NewOpenAIAdapter(runtimeServer.Client()))
	profile, _ := providerService.Save(context.Background(), providers.SaveInput{Name: "declared-only", BaseURL: runtimeServer.URL + "/v1",
		Model: "qualified-model", ContextWindow: 65536, ContextEvidence: "declared", MaxOutputTokens: 4096})
	service := NewService(dataStore, providerService, nil, hruntime.NewInferenceGate(), ctxcompiler.NewAdaptiveEstimator())
	run, err := service.Run(context.Background(), Input{ProviderID: profile.ID, RequestedProfile: "certified-64k"})
	if err != nil {
		t.Fatal(err)
	}
	if run.ContextTier != "limited" || run.Eligible || !run.RequiresDecision || len(run.Remediation) == 0 {
		t.Fatalf("silent downgrade guard=%+v", run)
	}
}

func TestContextTierCoversEverySelectableEnvelope(t *testing.T) {
	cases := []struct {
		allocated int
		tier      string
	}{
		{32768, "compact-32k"}, {65536, "certified-64k"}, {131072, "extended-128k"},
		{262144, "extended-256k"}, {1048576, "ultra-1m"},
	}
	for _, item := range cases {
		if got := contextTier(item.allocated, true); got != item.tier {
			t.Fatalf("allocated=%d tier=%s want=%s", item.allocated, got, item.tier)
		}
		if got := contextCapacity(item.tier); got != item.allocated {
			t.Fatalf("tier=%s capacity=%d want=%d", item.tier, got, item.allocated)
		}
	}
}

// --- O-6: recall evidence per position ---
//
// The probe used to plant one sentinel at the head of the prompt, so a model
// that echoed the opening line certified any tier up to its allocation. A tier
// is an envelope, and certifying one means reaching into its middle.
func TestHeadOnlyRecallDoesNotCertifyATier(t *testing.T) {
	server := qualificationServerEchoingLabels(t, []string{"HEAD"})
	defer server.Close()
	run := runQualificationAgainst(t, server, "certified-64k")
	if run.Results.LongContextRecall {
		t.Fatal("recall passed on the head sentinel alone")
	}
	if run.ContextTier != "limited" || run.Eligible {
		t.Fatalf("a head-only echo certified tier=%s eligible=%v", run.ContextTier, run.Eligible)
	}
	if len(run.Results.RecallPositions) != 5 {
		t.Fatalf("probe recorded %d positions, want 5", len(run.Results.RecallPositions))
	}
	recovered := 0
	for _, position := range run.Results.RecallPositions {
		if position.Recovered {
			recovered++
		}
	}
	if recovered != 1 {
		t.Fatalf("%d positions were recovered from a head-only echo, want 1", recovered)
	}
}

func TestEveryPositionRecoveredCertifiesTheTier(t *testing.T) {
	server := qualificationServerEchoingLabels(t, []string{"HEAD", "QUARTER", "MIDDLE", "THREE_QUARTER", "TAIL"})
	defer server.Close()
	run := runQualificationAgainst(t, server, "certified-64k")
	if !run.Results.LongContextRecall {
		t.Fatalf("full recall did not pass: %+v", run.Results.RecallPositions)
	}
	if run.ContextTier != "certified-64k" || !run.Eligible {
		t.Fatalf("full recall did not certify: tier=%s eligible=%v", run.ContextTier, run.Eligible)
	}
}

func runQualificationAgainst(t *testing.T, server *httptest.Server, requestedProfile string) Run {
	t.Helper()
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	providerService := providers.NewService(dataStore, providers.NewOpenAIAdapter(server.Client()))
	profile, err := providerService.Save(ctx, providers.SaveInput{Name: "qualified", BaseURL: server.URL + "/v1",
		Model: "qualified-model", ContextWindow: 65536, ContextEvidence: "declared", MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(dataStore, providerService, localmodel.NewProberWithClient(server.Client()),
		hruntime.NewInferenceGate(), ctxcompiler.NewAdaptiveEstimator())
	run, err := service.Run(ctx, Input{ProviderID: profile.ID, RequestedProfile: requestedProfile,
		RuntimeProbe: &localmodel.ProbeRequest{Runtime: "ollama", Endpoint: server.URL, Model: "qualified-model"}})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

// --- O-8: probe output budget on reasoning models ---
//
// The suite reads content out of every probe. Reasoning models bill their
// thinking as completion tokens, so a budget sized for the answer truncates the
// answer and the suite reports a capability failure that is really an output
// budget failure. Measured live: the same recall prompt at max_tokens=256
// produced 377 characters of reasoning unstreamed and 656 streamed, and only
// the streamed run was cut off mid-sentinel.
func TestEveryProbeUsesTheSharedOutputBudget(t *testing.T) {
	source, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatal(err)
	}
	hardcoded := regexp.MustCompile(`MaxTokens:\s*\d+`)
	if matches := hardcoded.FindAllString(string(source), -1); len(matches) > 0 {
		t.Fatalf("probes must use qualificationOutputBudget, found hardcoded: %v", matches)
	}
	if qualificationOutputBudget < 512 {
		t.Fatalf("probe budget %d is too small to hold reasoning plus an answer", qualificationOutputBudget)
	}
}

// TestRemoteProviderCannotReachQualifiedMode records a consequence of the
// runtime-probe requirement that nothing had written down.
//
// The guard itself is right: TestUnverifiedRuntimeNeverSilentlyCertifiesDeclared
// Context exists because a declared context window is a vendor claim, not
// evidence. But the probe is loopback-only by design -- validateEndpoint
// refuses anything else with "remote model endpoints are disabled in the local
// probe" -- so a provider behind a gateway cannot supply one. Not "has not
// yet": cannot.
//
// Run live against the gateway, every behavioural check passed and the grade
// was A, and the run still came back ineligible:
//
//	capability_grade  A
//	native_tool_call  true    sequential_tool_calls true
//	long_context_recall true  (5 of 5 positions recovered)
//	cancellation      true    foreground_preemption true
//	allocated_context 0    -> context_tier "limited" -> eligible false
//
// So of the three qualification modes, a gateway user can reach two:
// "compatibility", which only exists for compact-32k, and "explicit_override",
// which expires after 24 hours. Across the driven corpus that is exactly what
// happened -- 103 sessions compatibility, 2 explicit_override, 0 qualified. An
// override nobody can avoid stops being a review and becomes a daily
// formality.
//
// This test does not assert the behaviour is wrong. It asserts it is real, so
// that changing it is a decision someone takes rather than a side effect.
func TestRemoteProviderCannotReachQualifiedMode(t *testing.T) {
	runtimeServer := qualificationRuntime(t)
	defer runtimeServer.Close()
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	providerService := providers.NewService(dataStore, providers.NewOpenAIAdapter(runtimeServer.Client()))
	profile, err := providerService.Save(context.Background(), providers.SaveInput{Name: "gateway",
		BaseURL: runtimeServer.URL + "/v1", Model: "qualified-model", ContextWindow: 131072,
		ContextEvidence: "declared", MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(dataStore, providerService, localmodel.NewProberWithClient(runtimeServer.Client()),
		hruntime.NewInferenceGate(), ctxcompiler.NewAdaptiveEstimator())

	// A gateway has no loopback runtime to probe, so the request carries none.
	run, err := service.Run(context.Background(), Input{ProviderID: profile.ID, RequestedProfile: "certified-64k"})
	if err != nil {
		t.Fatal(err)
	}
	// The premise: nothing about the model's behaviour is what fails.
	if run.CapabilityGrade != "A" {
		t.Fatalf("premise broken: grade %q, want A -- this test is about a provider that passes everything",
			run.CapabilityGrade)
	}
	if !run.Results.LongContextRecall || !run.Results.NativeToolCall || !run.Results.Cancellation {
		t.Fatalf("premise broken: a behavioural check failed: %+v", run.Results)
	}
	// The finding.
	if run.Eligible {
		t.Fatal("a remote provider reached eligible; qualification is no longer override-only for gateways")
	}
	if run.AllocatedContext != 0 || run.ContextTier != "limited" {
		t.Fatalf("expected the tier to collapse for want of an allocation, got tier=%q allocated=%d",
			run.ContextTier, run.AllocatedContext)
	}
	// And the reason has to be legible in the report, not inferred from code.
	joined := strings.Join(run.Remediation, " ")
	if !strings.Contains(joined, "Runtime allocation is unverified") {
		t.Fatalf("the report does not say why it refused: %v", run.Remediation)
	}
	// The probe is not merely absent -- it is unavailable. Confirm the door is
	// actually shut rather than assuming it.
	_, err = localmodel.NewProberWithClient(runtimeServer.Client()).Probe(context.Background(),
		localmodel.ProbeRequest{Runtime: "ollama", Endpoint: "https://gateway.example.com", Model: "qualified-model"})
	if err == nil || !strings.Contains(err.Error(), "remote model endpoints are disabled") {
		t.Fatalf("the local probe accepted a remote endpoint: %v", err)
	}
}

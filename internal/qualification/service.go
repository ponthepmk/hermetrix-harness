package qualification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/localmodel"
	"hermetrix-harness/internal/providers"
	hruntime "hermetrix-harness/internal/runtime"
	"hermetrix-harness/internal/store"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const suiteRevision = "local-model-qualification-v2"

type Service struct {
	store     *store.Store
	providers *providers.Service
	prober    *localmodel.Prober
	gate      *hruntime.InferenceGate
	estimator ctxcompiler.Estimator
}

func NewService(dataStore *store.Store, providerService *providers.Service, prober *localmodel.Prober,
	gate *hruntime.InferenceGate, estimator ctxcompiler.Estimator) *Service {
	if prober == nil {
		prober = localmodel.NewProber()
	}
	if estimator == nil {
		estimator = ctxcompiler.NewAdaptiveEstimator()
	}
	return &Service{store: dataStore, providers: providerService, prober: prober, gate: gate, estimator: estimator}
}

func (s *Service) Run(ctx context.Context, input Input) (Run, error) {
	profile, err := s.providers.Get(ctx, input.ProviderID)
	if err != nil {
		return Run{}, err
	}
	requested, ok := ctxcompiler.ProfileByName(input.RequestedProfile)
	if !ok {
		return Run{}, fmt.Errorf("unknown requested context profile %q", input.RequestedProfile)
	}
	started := time.Now().UTC()
	run := Run{ID: identity.New("qual"), ProviderID: profile.ID, ProviderName: profile.Name, Model: profile.Model,
		SuiteRevision: suiteRevision, ProviderRevision: providers.Revision(profile), State: "running", DeclaredContext: profile.ContextWindow,
		ContextTier: "limited", CapabilityGrade: "C", RequestedProfile: requested.Name, StartedAt: started}
	if input.RuntimeProbe != nil {
		run.RuntimeKind, run.RuntimeEndpoint = input.RuntimeProbe.Runtime, input.RuntimeProbe.Endpoint
	}
	if _, err := s.store.DB.ExecContext(ctx, `INSERT INTO model_qualification_runs(id,provider_id,runtime_kind,
	  runtime_endpoint,model,suite_revision,provider_revision,state,declared_context,requested_profile,started_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, run.ID,
		run.ProviderID, run.RuntimeKind, run.RuntimeEndpoint, run.Model, run.SuiteRevision, run.ProviderRevision, run.State,
		run.DeclaredContext, run.RequestedProfile, formatTime(started)); err != nil {
		return Run{}, err
	}

	if input.RuntimeProbe != nil {
		probeStarted := time.Now()
		probe, probeErr := s.prober.Probe(ctx, *input.RuntimeProbe)
		check := Check{Name: "runtime_allocation", LatencyMS: time.Since(probeStarted).Milliseconds(), Evidence: map[string]any{}}
		if probeErr != nil {
			check.State, check.Remediation = "failed", probeErr.Error()
			run.Remediation = append(run.Remediation, "Verify the local runtime endpoint and load the selected model with an explicit context allocation.")
		} else {
			run.AllocatedContext = probe.AllocatedContext
			run.Results.RuntimeAllocation = probe.Verified
			check.Evidence = map[string]any{"allocated_context": probe.AllocatedContext, "configured_context": probe.ConfiguredContext,
				"training_context": probe.TrainingContext, "source": probe.ContextSource}
			if probe.Verified {
				check.State = "passed"
			} else {
				check.State = "failed"
				check.Remediation = "The runtime reported only declared/training context, not the loaded allocation."
			}
		}
		run.Results.Checks = append(run.Results.Checks, check)
	} else {
		run.Results.Checks = append(run.Results.Checks, Check{Name: "runtime_allocation", State: "not_run",
			Remediation: "Attach a loopback runtime probe before certifying a local context tier."})
		run.Remediation = append(run.Remediation, "Runtime allocation is unverified; the declared provider context cannot certify 64k mode.")
	}

	if err := s.runBehavioralSuite(ctx, profile, &run); err != nil {
		return s.fail(context.WithoutCancel(ctx), run, err)
	}
	requestedContext := requested.Total
	if run.AllocatedContext < requestedContext {
		requestedContext = run.AllocatedContext
	}
	run.ContextTier = contextTier(requestedContext, run.Results.LongContextRecall)
	run.CapabilityGrade = capabilityGrade(run.Results)
	requiredContext := requested.Total
	run.Eligible = contextCapacity(run.ContextTier) >= requiredContext && run.CapabilityGrade != "C"
	if !run.Eligible {
		run.RequiresDecision = true
		run.Remediation = append(run.Remediation, fmt.Sprintf("Requested %s needs %d verified tokens and tool grade A/B; keep the current profile or explicitly choose a lower mode after reviewing this report.", requested.Name, requiredContext))
	}
	run.State = "completed"
	completed := time.Now().UTC()
	run.CompletedAt = &completed
	resultsJSON, _ := json.Marshal(run.Results)
	remediationJSON, _ := json.Marshal(unique(run.Remediation))
	run.Remediation = unique(run.Remediation)
	_, err = s.store.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE model_qualification_runs SET state='completed',
		allocated_context=?,context_tier=?,capability_grade=?,eligible=?,requires_decision=?,results_json=?,remediation_json=?,completed_at=? WHERE id=?`,
		run.AllocatedContext, run.ContextTier, run.CapabilityGrade, run.Eligible, run.RequiresDecision, string(resultsJSON), string(remediationJSON),
		formatTime(completed), run.ID)
	return run, err
}

func (s *Service) runBehavioralSuite(ctx context.Context, profile providers.Profile, run *Run) error {
	temperature := 0.0
	connectStarted := time.Now()
	firstDelta := time.Time{}
	completion, err := s.providers.StreamChat(ctx, profile, providers.ChatRequest{Messages: []providers.Message{
		{Role: "system", Content: "Qualification probe. Follow exact output instructions."},
		{Role: "user", Content: "[QUALIFY:CONNECT] Reply exactly HERMETRIX_QUALIFIED_OK"}}, Temperature: &temperature,
		MaxTokens: 128}, func(delta providers.Delta) error {
		if firstDelta.IsZero() && (delta.Content != "" || delta.Reasoning != "" || len(delta.ToolCalls) > 0) {
			firstDelta = time.Now()
		}
		return nil
	})
	connectLatency := time.Since(connectStarted)
	connectPassed := err == nil && strings.Contains(completion.Content, "HERMETRIX_QUALIFIED_OK")
	run.Results.Connectivity = connectPassed
	connectCheck := Check{Name: "connectivity", LatencyMS: connectLatency.Milliseconds(), State: state(connectPassed),
		Evidence: map[string]any{"finish_reason": completion.FinishReason}}
	if err != nil {
		connectCheck.Remediation = err.Error()
	}
	run.Results.Checks = append(run.Results.Checks, connectCheck)
	if !connectPassed {
		return fmt.Errorf("provider failed the exact connectivity probe")
	}
	if !firstDelta.IsZero() {
		run.Results.TTFTMilliseconds = firstDelta.Sub(connectStarted).Milliseconds()
	}
	run.Results.TotalLatencyMilliseconds = connectLatency.Milliseconds()
	if connectLatency > 0 && completion.Usage.CompletionTokens > 0 {
		run.Results.TokensPerSecond = float64(completion.Usage.CompletionTokens) / connectLatency.Seconds()
	}
	predicted := s.estimator.Count("Qualification probe. Follow exact output instructions.\n[QUALIFY:CONNECT] Reply exactly HERMETRIX_QUALIFIED_OK")
	if completion.Usage.PromptTokens > 0 && predicted > 0 {
		run.Results.UsageRatio = float64(completion.Usage.PromptTokens) / float64(predicted)
		run.Results.UsageCalibration = run.Results.UsageRatio >= 0.25 && run.Results.UsageRatio <= 4
	}
	run.Results.Checks = append(run.Results.Checks, Check{Name: "usage_calibration", State: state(run.Results.UsageCalibration),
		Evidence: map[string]any{"predicted_tokens": predicted, "reported_prompt_tokens": completion.Usage.PromptTokens,
			"ratio": run.Results.UsageRatio}, Remediation: remediation(!run.Results.UsageCalibration, "Provider must return non-zero prompt usage for tokenizer calibration.")})

	requested, _ := ctxcompiler.ProfileByName(run.RequestedProfile)
	recallTarget := requested.Total
	if run.AllocatedContext == 0 || recallTarget > run.AllocatedContext {
		recallTarget = run.AllocatedContext
	}
	if recallTarget > profile.ContextWindow {
		recallTarget = profile.ContextWindow
	}
	if recallTarget < 4096 {
		recallTarget = 4096
	}
	sentinel := "HERMETRIX_SENTINEL_7F3A"
	probeTokens := recallTarget * 65 / 100
	unit := "neutral calibration context "
	unitTokens := s.estimator.Count(unit)
	repeats := probeTokens / unitTokens
	if repeats < 1 {
		repeats = 1
	}
	longPrompt := sentinel + "\n" + strings.Repeat(unit, repeats) +
		"\n[QUALIFY:RECALL] Return only the sentinel placed at the beginning."
	recallStarted := time.Now()
	recall, recallErr := s.providers.StreamChat(ctx, profile, providers.ChatRequest{Messages: []providers.Message{{Role: "user", Content: longPrompt}},
		Temperature: &temperature, MaxTokens: 64}, nil)
	run.Results.LongContextRecall = recallErr == nil && strings.Contains(recall.Content, sentinel)
	run.Results.RecallProbedTokens = probeTokens
	run.Results.Checks = append(run.Results.Checks, Check{Name: "long_context_recall", State: state(run.Results.LongContextRecall),
		LatencyMS: time.Since(recallStarted).Milliseconds(), Evidence: map[string]any{"allocated_context": recallTarget, "probe_tokens": probeTokens,
			"reported_prompt_tokens": recall.Usage.PromptTokens}, Remediation: remediation(!run.Results.LongContextRecall,
			"Increase runtime allocation or reduce the selected context tier; the sentinel was not recovered.")})

	toolSchema := map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"},
		"mode": map[string]any{"type": "string", "enum": []string{"safe"}}}, "required": []string{"text", "mode"}, "additionalProperties": false}
	tool := providers.ToolDefinition{Type: "function", Function: providers.ToolFunction{Name: "qualification_echo",
		Description: "Return qualification evidence", Parameters: toolSchema}}
	toolCompletion, toolErr := s.providers.StreamChat(ctx, profile, providers.ChatRequest{Messages: []providers.Message{{Role: "user",
		Content: "[QUALIFY:THAI_TOOL] กรุณาเรียก qualification_echo ด้วย text=ภาษาไทย และ mode=safe"}}, Tools: []providers.ToolDefinition{tool}, Temperature: &temperature, MaxTokens: 256}, nil)
	validTool := toolErr == nil && len(toolCompletion.ToolCalls) == 1 && toolCompletion.ToolCalls[0].Name == "qualification_echo" &&
		validateArguments(toolSchema, toolCompletion.ToolCalls[0].Arguments) == nil
	run.Results.NativeToolCall, run.Results.ThaiEnglishSchema = validTool, validTool
	run.Results.Checks = append(run.Results.Checks, Check{Name: "thai_user_english_schema", State: state(validTool),
		Evidence: map[string]any{"tool_calls": len(toolCompletion.ToolCalls)}, Remediation: remediation(!validTool,
			"Use constrained tool envelopes or a model with reliable native JSON-schema tool calling.")})

	sequential, sequentialErr := s.providers.StreamChat(ctx, profile, providers.ChatRequest{Messages: []providers.Message{{Role: "user",
		Content: "[QUALIFY:SEQUENTIAL] Call qualification_first and qualification_second in this response."}}, Tools: []providers.ToolDefinition{
		{Type: "function", Function: providers.ToolFunction{Name: "qualification_first", Parameters: emptySchema()}},
		{Type: "function", Function: providers.ToolFunction{Name: "qualification_second", Parameters: emptySchema()}},
	}, Temperature: &temperature, MaxTokens: 256}, nil)
	names := map[string]bool{}
	for _, call := range sequential.ToolCalls {
		names[call.Name] = validateArguments(emptySchema(), call.Arguments) == nil
	}
	run.Results.SequentialToolCalls = sequentialErr == nil && names["qualification_first"] && names["qualification_second"]
	run.Results.Checks = append(run.Results.Checks, Check{Name: "sequential_tools", State: state(run.Results.SequentialToolCalls),
		Evidence: map[string]any{"tool_calls": len(sequential.ToolCalls)}, Remediation: remediation(!run.Results.SequentialToolCalls,
			"Use the constrained sequential envelope and expose one required tool at a time.")})

	recoveryMessages := []providers.Message{
		{Role: "user", Content: "[QUALIFY:RECOVERY] Correct the previous invalid call and call qualification_echo with text=recovered and mode=safe."},
		{Role: "assistant", ToolCalls: []providers.MessageToolCall{
			{ID: "bad", Type: "function", Function: providers.ToolCallInvocation{Name: "qualification_echo", Arguments: `{"mode":"unsafe"}`}},
		}},
		{Role: "tool", ToolCallID: "bad", Content: `{"status":"failed","error":"schema validation failed"}`},
	}
	recovery, recoveryErr := s.providers.StreamChat(ctx, profile, providers.ChatRequest{Messages: recoveryMessages,
		Tools: []providers.ToolDefinition{tool}, Temperature: &temperature, MaxTokens: 256}, nil)
	run.Results.MalformedRecovery = recoveryErr == nil && len(recovery.ToolCalls) == 1 &&
		validateArguments(toolSchema, recovery.ToolCalls[0].Arguments) == nil
	run.Results.Checks = append(run.Results.Checks, Check{Name: "malformed_argument_recovery", State: state(run.Results.MalformedRecovery),
		Remediation: remediation(!run.Results.MalformedRecovery, "Harness should constrain the next step to the exact schema after a malformed call.")})

	deferredTool := providers.ToolDefinition{Type: "function", Function: providers.ToolFunction{Name: "tool_search",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}},
			"required": []string{"query"}, "additionalProperties": false}}}
	deferred, deferredErr := s.providers.StreamChat(ctx, profile, providers.ChatRequest{Messages: []providers.Message{{Role: "user",
		Content: "[QUALIFY:DEFERRED] Search for a calendar capability using tool_search."}}, Tools: []providers.ToolDefinition{deferredTool},
		Temperature: &temperature, MaxTokens: 256}, nil)
	run.Results.DeferredToolCall = deferredErr == nil && len(deferred.ToolCalls) == 1 && deferred.ToolCalls[0].Name == "tool_search" &&
		validateArguments(deferredTool.Function.Parameters, deferred.ToolCalls[0].Arguments) == nil
	run.Results.Checks = append(run.Results.Checks, Check{Name: "deferred_tool", State: state(run.Results.DeferredToolCall),
		Remediation: remediation(!run.Results.DeferredToolCall, "Use capability preselection or capability grade C chat mode.")})

	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, cancelErr := s.providers.StreamChat(cancelCtx, profile, providers.ChatRequest{Messages: []providers.Message{{Role: "user", Content: "cancel"}}}, nil)
	run.Results.Cancellation = errors.Is(cancelErr, context.Canceled) || strings.Contains(strings.ToLower(errorString(cancelErr)), "canceled")
	run.Results.Checks = append(run.Results.Checks, Check{Name: "cancellation", State: state(run.Results.Cancellation)})

	if s.gate != nil {
		backgroundStarted := make(chan struct{})
		backgroundDone := make(chan error, 1)
		go func() {
			backgroundDone <- s.gate.RunBackground(ctx, func(backgroundCtx context.Context) error {
				close(backgroundStarted)
				<-backgroundCtx.Done()
				return backgroundCtx.Err()
			})
		}()
		select {
		case <-backgroundStarted:
			preemptStarted := time.Now()
			err := s.gate.RunForeground(ctx, func(context.Context) error { return nil })
			run.Results.ForegroundPreemption = err == nil && time.Since(preemptStarted) < time.Second
			<-backgroundDone
		case <-time.After(time.Second):
			run.Results.ForegroundPreemption = false
		}
	} else {
		run.Results.ForegroundPreemption = false
	}
	run.Results.Checks = append(run.Results.Checks, Check{Name: "foreground_preemption", State: state(run.Results.ForegroundPreemption)})
	return nil
}

func (s *Service) List(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT q.id,q.provider_id,p.name,q.runtime_kind,q.runtime_endpoint,q.model,
		q.suite_revision,q.provider_revision,q.state,q.declared_context,q.allocated_context,q.context_tier,q.capability_grade,q.requested_profile,
		q.eligible,q.requires_decision,q.results_json,
      q.remediation_json,q.error,q.started_at,q.completed_at FROM model_qualification_runs q
      LEFT JOIN provider_profiles p ON p.id=q.provider_id ORDER BY q.started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Run{}
	for rows.Next() {
		item, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) fail(ctx context.Context, run Run, failure error) (Run, error) {
	completed := time.Now().UTC()
	run.State, run.Error, run.CompletedAt = "failed", failure.Error(), &completed
	resultsJSON, _ := json.Marshal(run.Results)
	remediationJSON, _ := json.Marshal(run.Remediation)
	_, _ = s.store.DB.ExecContext(ctx, `UPDATE model_qualification_runs SET state='failed',allocated_context=?,
      results_json=?,remediation_json=?,error=?,completed_at=? WHERE id=?`, run.AllocatedContext, string(resultsJSON),
		string(remediationJSON), run.Error, formatTime(completed), run.ID)
	return run, failure
}

func validateArguments(schema map[string]any, raw string) error {
	document, err := jsonschema.UnmarshalJSON(strings.NewReader(mustJSON(schema)))
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("urn:qualification", document); err != nil {
		return err
	}
	compiled, err := compiler.Compile("urn:qualification")
	if err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(strings.NewReader(raw))
	if err != nil {
		return err
	}
	return compiled.Validate(instance)
}

func mustJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func emptySchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false}
}

func contextTier(allocated int, recall bool) string {
	if !recall {
		return "limited"
	}
	if allocated >= 1048576 {
		return "ultra-1m"
	}
	if allocated >= 262144 {
		return "extended-256k"
	}
	if allocated >= 131072 {
		return "extended-128k"
	}
	if allocated >= 65536 {
		return "certified-64k"
	}
	if allocated >= 32768 {
		return "compact-32k"
	}
	return "limited"
}

func contextCapacity(tier string) int {
	switch tier {
	case "ultra-1m":
		return 1048576
	case "extended-256k":
		return 262144
	case "extended-128k":
		return 131072
	case "certified-64k":
		return 65536
	case "compact-32k":
		return 32768
	default:
		return 0
	}
}

func capabilityGrade(result Results) string {
	if result.NativeToolCall && result.SequentialToolCalls && result.MalformedRecovery && result.DeferredToolCall && result.Cancellation {
		return "A"
	}
	if result.NativeToolCall && result.MalformedRecovery && result.Cancellation {
		return "B"
	}
	return "C"
}

func state(passed bool) string {
	if passed {
		return "passed"
	}
	return "failed"
}

func remediation(condition bool, message string) string {
	if condition {
		return message
	}
	return ""
}

func unique(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type runScanner interface{ Scan(...any) error }

func scanRun(row runScanner) (Run, error) {
	var item Run
	var providerName sql.NullString
	var resultsJSON, remediationJSON, started string
	var completed sql.NullString
	if err := row.Scan(&item.ID, &item.ProviderID, &providerName, &item.RuntimeKind, &item.RuntimeEndpoint, &item.Model,
		&item.SuiteRevision, &item.ProviderRevision, &item.State, &item.DeclaredContext, &item.AllocatedContext, &item.ContextTier,
		&item.CapabilityGrade, &item.RequestedProfile, &item.Eligible, &item.RequiresDecision,
		&resultsJSON, &remediationJSON, &item.Error, &started, &completed); err != nil {
		return Run{}, err
	}
	item.ProviderName = providerName.String
	_ = json.Unmarshal([]byte(resultsJSON), &item.Results)
	_ = json.Unmarshal([]byte(remediationJSON), &item.Remediation)
	item.StartedAt, _ = parseTime(started)
	if completed.Valid {
		value, _ := parseTime(completed.String)
		item.CompletedAt = &value
	}
	return item, nil
}

func formatTime(value time.Time) string         { return value.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }

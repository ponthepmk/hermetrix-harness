package taskcoord

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

type decisionAdapter struct {
	completion providers.Completion
	err        error
	calls      int
	request    providers.ChatRequest
	respond    func(providers.ChatRequest) (providers.Completion, error)
}

func (a *decisionAdapter) StreamChat(_ context.Context, _ providers.Profile, _ string, request providers.ChatRequest,
	_ func(providers.Delta) error) (providers.Completion, error) {
	a.calls++
	a.request = request
	if a.respond != nil {
		return a.respond(request)
	}
	return a.completion, a.err
}

func decisionFixture(t *testing.T, adapter *decisionAdapter, baseURL string) (*Service, *taskengine.Service, taskengine.Task, providers.Profile) {
	t.Helper()
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	providerService := providers.NewService(dataStore, adapter)
	profile, err := providerService.Save(ctx, providers.SaveInput{Name: "decision model", BaseURL: baseURL,
		Model: "bonsai-test", ContextWindow: 32768, MaxOutputTokens: 2048})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskengine.NewService(dataStore)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{Title: "Choose next action", Objective: "fix the current step",
		OriginalRequest: "fix it", Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "tests pass"}}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = tasks.CreatePlan(ctx, taskengine.CreatePlanInput{
		TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Reason: "bounded work", Actor: "planner",
		Steps: []taskengine.StepSpec{{
			Key: "fix", Title: "Fix", Instructions: "inspect and correct", Checks: []string{"go test ./..."},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return New(tasks, nil, providerService), tasks, task, profile
}

func TestShadowDecisionComparesLocalModelWithoutMutatingTask(t *testing.T) {
	adapter := &decisionAdapter{completion: providers.Completion{FinishReason: "tool_calls", Usage: providers.Usage{
		PromptTokens: 40, CompletionTokens: 12, TotalTokens: 52}, ToolCalls: []providers.ToolCall{{
		Name: "submit_decision", Arguments: `{"action_id":"search_repository","reason":"find related implementations first"}`,
	}}}}
	coordinator, tasks, task, profile := decisionFixture(t, adapter, "http://127.0.0.1:8088/v1")

	result, err := coordinator.ShadowDecision(context.Background(), ShadowDecisionInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision, ProviderID: profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Model == nil || result.Model.ActionID != "search_repository" || result.Baseline.ActionID != "inspect_file" || result.Agreement {
		t.Fatalf("unexpected comparison: %+v", result)
	}
	if result.EvaluationID == "" || result.CreatedAt.IsZero() {
		t.Fatalf("shadow evaluation was not persisted: %+v", result)
	}
	if result.Usage.TotalTokens != 52 || adapter.calls != 1 || adapter.request.MaxTokens != maxDecisionOutputTokens ||
		adapter.request.Temperature == nil || *adapter.request.Temperature != 0 {
		t.Fatalf("request was not bounded: result=%+v request=%+v calls=%d", result, adapter.request, adapter.calls)
	}
	if len(adapter.request.Tools) != 1 || adapter.request.Tools[0].Function.Name != "submit_decision" {
		t.Fatalf("tool contract=%+v", adapter.request.Tools)
	}
	after, err := tasks.Get(context.Background(), task.ID)
	if err != nil || after.Revision != task.Revision || after.State != task.State {
		t.Fatalf("shadow mutated task: before=%+v after=%+v err=%v", task, after, err)
	}
	runs, err := tasks.ListDecisionShadows(context.Background(), task.ID, 10)
	if err != nil || len(runs) != 1 || runs[0].ID != result.EvaluationID || !runs[0].Valid || runs[0].Agreement ||
		runs[0].Model == nil || runs[0].Model.ActionID != "search_repository" {
		t.Fatalf("stored runs=%+v err=%v", runs, err)
	}
	metrics, err := tasks.DecisionShadowMetrics(context.Background(), task.ID, profile.ID)
	if err != nil || metrics.TotalRuns != 1 || metrics.ValidRuns != 1 || metrics.AgreementRuns != 0 ||
		metrics.InvalidRate != 0 || metrics.AgreementRate != 0 {
		t.Fatalf("metrics=%+v err=%v", metrics, err)
	}
}

func TestShadowDecisionReportsInvalidModelOutputAndKeepsBaseline(t *testing.T) {
	adapter := &decisionAdapter{completion: providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{
		Name: "submit_decision", Arguments: `{"action_id":"delete_workspace","reason":"fast"}`,
	}}}}
	coordinator, tasks, task, profile := decisionFixture(t, adapter, "http://localhost:8088/v1")
	result, err := coordinator.ShadowDecision(context.Background(), ShadowDecisionInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision, ProviderID: profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != nil || result.Baseline.ActionID != "inspect_file" || !strings.Contains(result.ModelError, "invalid candidate") {
		t.Fatalf("invalid output was not observable: %+v", result)
	}
	runs, err := tasks.ListDecisionShadows(context.Background(), task.ID, 10)
	if err != nil || len(runs) != 1 || runs[0].Valid || runs[0].Model != nil || runs[0].ModelError == "" {
		t.Fatalf("invalid receipt=%+v err=%v", runs, err)
	}
	metrics, err := tasks.DecisionShadowMetrics(context.Background(), task.ID, profile.ID)
	if err != nil || metrics.TotalRuns != 1 || metrics.ValidRuns != 0 || metrics.InvalidRate != 1 {
		t.Fatalf("invalid metrics=%+v err=%v", metrics, err)
	}
}

func TestShadowDecisionRejectsRemoteProviderBeforeDispatch(t *testing.T) {
	adapter := &decisionAdapter{}
	coordinator, _, task, profile := decisionFixture(t, adapter, "https://models.example/v1")
	_, err := coordinator.ShadowDecision(context.Background(), ShadowDecisionInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision, ProviderID: profile.ID})
	if err == nil || !strings.Contains(err.Error(), "local provider") || adapter.calls != 0 {
		t.Fatalf("remote profile was not blocked: calls=%d err=%v", adapter.calls, err)
	}
}

func TestShadowDecisionRejectsDisabledProviderBeforeDispatch(t *testing.T) {
	adapter := &decisionAdapter{}
	coordinator, _, task, profile := decisionFixture(t, adapter, "http://127.0.0.1:8088/v1")
	disabled := false
	profile, err := coordinator.providers.Save(context.Background(), providers.SaveInput{ID: profile.ID, Name: profile.Name,
		BaseURL: profile.BaseURL, Model: profile.Model, ContextWindow: profile.ContextWindow,
		MaxOutputTokens: profile.MaxOutputTokens, Enabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	_, err = coordinator.ShadowDecision(context.Background(), ShadowDecisionInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision, ProviderID: profile.ID})
	if err == nil || !strings.Contains(err.Error(), "disabled") || adapter.calls != 0 {
		t.Fatalf("disabled profile was not blocked: calls=%d err=%v", adapter.calls, err)
	}
}

func TestShadowDecisionRejectsStaleRevisionBeforeDispatch(t *testing.T) {
	adapter := &decisionAdapter{}
	coordinator, _, task, profile := decisionFixture(t, adapter, "http://127.0.0.1:8088/v1")
	_, err := coordinator.ShadowDecision(context.Background(), ShadowDecisionInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision - 1, ProviderID: profile.ID})
	if err != taskengine.ErrStaleRevision || adapter.calls != 0 {
		t.Fatalf("stale snapshot reached provider: calls=%d err=%v", adapter.calls, err)
	}
}

func TestBonsaiDecisionRevalidatesProviderBindingAtDispatch(t *testing.T) {
	adapter := &decisionAdapter{}
	coordinator, tasks, task, profile := decisionFixture(t, adapter, "http://127.0.0.1:8088/v1")
	baseline, err := tasks.DecideNextAction(context.Background(), task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewBonsaiDecision(coordinator.providers, profile)
	if err != nil {
		t.Fatal(err)
	}
	engine.beforeProviderRequest = func(context.Context) error { return errors.New("binding changed") }
	_, err = engine.Decide(context.Background(), baseline.State, baseline.Candidates)
	if err == nil || !strings.Contains(err.Error(), "binding changed") || adapter.calls != 0 {
		t.Fatalf("changed binding reached provider: calls=%d err=%v", adapter.calls, err)
	}
}

func TestBonsaiDecisionAgainstLocalServer(t *testing.T) {
	if os.Getenv("HERMETRIX_RUN_BONSAI_DECISION_E2E") == "" {
		t.Skip("set HERMETRIX_RUN_BONSAI_DECISION_E2E=1 to call the local model")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	providerService := providers.NewService(dataStore, nil)
	profile, err := providerService.Save(ctx, providers.SaveInput{Name: "Bonsai E2E", BaseURL: "http://127.0.0.1:8088/v1",
		Model: "prism-ml/Ternary-Bonsai-2-27B-gguf:PQ2_0", ContextWindow: 61440, MaxOutputTokens: 8192})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskengine.NewService(dataStore)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{Title: "Inspect before editing", Objective: "understand a defect before changing it",
		OriginalRequest: "inspect the relevant source", Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "the defect is corrected"}}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = tasks.CreatePlan(ctx, taskengine.CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Reason: "inspect first", Actor: "planner",
		Steps: []taskengine.StepSpec{{Key: "inspect", Title: "Inspect source", Instructions: "read the relevant implementation"}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := New(tasks, nil, providerService).ShadowDecision(ctx, ShadowDecisionInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision, ProviderID: profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Model == nil || result.ModelError != "" || result.EvaluationID == "" {
		t.Fatalf("local Bonsai did not return a valid bounded decision: %+v", result)
	}
	t.Logf("evaluation=%s baseline=%s model=%s agreement=%t latency_ms=%d tokens=%d",
		result.EvaluationID, result.Baseline.ActionID, result.Model.ActionID, result.Agreement,
		result.Model.LatencyMS, result.Usage.TotalTokens)
}

func TestDecisionBenchmarkAdmitsOnlyReadOnlySelection(t *testing.T) {
	adapter := &decisionAdapter{}
	adapter.respond = func(request providers.ChatRequest) (providers.Completion, error) {
		var input struct {
			State taskengine.CompactState `json:"state"`
		}
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &input); err != nil {
			return providers.Completion{}, err
		}
		action := "inspect_file"
		switch {
		case len(input.State.UnresolvedEffects) > 0:
			action = "reconcile_effects"
		case input.State.CompletionEligible:
			action = "finish"
		case input.State.PlanRevision == 0:
			action = "ask_planner"
		case input.State.ConsecutiveFailures >= 2:
			action = "retrieve_memory"
		case len(input.State.FailedChecks) > 0:
			action = "run_test"
		}
		return providers.Completion{FinishReason: "tool_calls", Usage: providers.Usage{PromptTokens: 20,
			CompletionTokens: 5, TotalTokens: 25}, ToolCalls: []providers.ToolCall{{Name: "submit_decision",
			Arguments: `{"action_id":"` + action + `","reason":"matches the bounded fixture state"}`}}}, nil
	}
	coordinator, _, task, profile := decisionFixture(t, adapter, "http://127.0.0.1:8088/v1")
	run, err := coordinator.RunDecisionBenchmark(context.Background(), profile.ID)
	if err != nil || !run.Passed || run.TotalCases != 6 || run.CorrectCases != 6 || run.InvalidRate != 0 {
		t.Fatalf("benchmark=%+v err=%v", run, err)
	}
	policy, err := coordinator.SetDecisionAdmission(context.Background(), profile.ID, run.ID, "owner", "fixture corpus passed", true)
	if err != nil || !policy.Enabled {
		t.Fatalf("policy=%+v err=%v", policy, err)
	}
	decision, err := coordinator.DecideAdmittedReadOnly(context.Background(), task.ID, task.Revision)
	if err != nil || decision.Decision.ActionID != "inspect_file" || decision.Decision.FallbackUsed {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	for _, candidate := range decision.Candidates {
		if candidate.Risk != "read" || candidate.RequiresPolicy {
			t.Fatalf("active candidate escaped read-only admission: %+v", candidate)
		}
	}
}

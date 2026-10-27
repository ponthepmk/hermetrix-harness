package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestChatReasoningChangeReachesActualProviderWireWithoutChangingAuthority(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/props" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"build_info":"b10743-adfffbe41","chat_template":"<think> {{ enable_thinking }}","chat_template_caps":{"supports_reasoning_effort":true}}`)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ANSWER_WITH_RESERVED_BUDGET\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID, ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "legacy baseline"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["reasoning_budget_tokens"]; ok {
		t.Fatal("legacy chat gained unrequested reasoning settings")
	}
	updated, err := service.SetSessionReasoning(context.Background(), session.ID, SessionReasoningInput{Mode: "bounded", TokenCap: 512, Reason: "Owner wants reserved output for tool calls"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CacheEpoch != session.CacheEpoch+1 || updated.ContractRevision == session.ContractRevision || !reflect.DeepEqual(updated.Contract.ToolBindings, session.Contract.ToolBindings) || !reflect.DeepEqual(updated.Contract.Qualification, session.Contract.Qualification) {
		t.Fatal("reasoning update changed tool/qualification authority or missed cache invalidation")
	}
	result, err := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "answer within budget"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.AssistantEvent.Content != "ANSWER_WITH_RESERVED_BUDGET" || payload["reasoning_budget_tokens"] != float64(512) || payload["reasoning_format"] != "deepseek" || result.Binding.EffectiveParameterDigest == "" {
		t.Fatalf("budget missing from wire/receipt: %+v payload=%+v", result, payload)
	}
	if _, err := service.SetSessionReasoning(context.Background(), session.ID, SessionReasoningInput{Mode: "bounded", TokenCap: 99999, Reason: "invalid"}); err == nil {
		t.Fatal("thinking consumed the answer reserve")
	}
	if _, err := service.store.DB.Exec(`UPDATE agent_sessions SET state='awaiting_approval',active_turn_id='pending' WHERE id=?`, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetSessionReasoning(context.Background(), session.ID, SessionReasoningInput{Mode: "disabled", Reason: "must not invalidate pending effect"}); err == nil {
		t.Fatal("reconfigured a pending approval")
	}
}

func TestChatReasoningRejectsUnknownRuntimeTransport(t *testing.T) {
	unknown := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	service, provider, cleanup := testAgentService(t, unknown)
	defer cleanup()
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID, ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetSessionReasoning(context.Background(), session.ID, SessionReasoningInput{Mode: "bounded", TokenCap: 512, Reason: "unsupported endpoint"}); err == nil {
		t.Fatal("invented a reasoning capability")
	}
}

func TestVerifiedReasoningCapUsesActualAnswerReserveAfterHeavyLegacySamples(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/props" {
			fmt.Fprint(w, `{"build_info":"b10743-adfffbe41","chat_template":"<think> {{ enable_thinking }}","chat_template_caps":{"supports_reasoning_effort":true}}`)
			return
		}
		http.NotFound(w, r)
	}))
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if err := service.providers.ObserveReasoning(ctx, provider.ID, 95, 100); err != nil {
			t.Fatal(err)
		}
	}
	input := CreateSessionInput{ProviderID: provider.ID, ContextProfile: "compact-32k"}
	if _, err := service.CreateSession(ctx, input); err == nil {
		t.Fatal("uncapped chat lost its insufficient-answer check")
	}
	input.ChatReasoning = &ChatReasoningSetting{Mode: "bounded", TokenCap: 512}
	session, err := service.CreateSession(ctx, input)
	if err != nil || session.Contract.AnswerBudget != 3584 || session.Contract.ReasoningRatio < .94 ||
		session.Contract.WorkspaceWriteApproval != nil || session.Contract.Qualification.Mode != "compatibility" {
		t.Fatalf("verified cap was refused or changed authority: %+v err=%v", session.Contract, err)
	}
	input.ChatReasoning = nil
	input.ProjectID = createTestProject(t, service, t.TempDir())
	input.WorkspaceWriteApproval = &WorkspaceWriteApprovalInput{Reason: "Owner selected project work"}
	automatic, err := service.CreateSession(ctx, input)
	if err != nil || automatic.Contract.ChatReasoning == nil || automatic.Contract.ChatReasoning.TokenCap != 1024 || automatic.Contract.AnswerBudget != 3072 {
		t.Fatalf("project default still uses the uncapped estimate: %+v err=%v", automatic.Contract, err)
	}
}

func TestActualToolArgumentsAndZeroReasoningAnswersCalibrateUsefulOutput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			arguments, _ := json.Marshal(map[string]string{"path": "candidate.txt", "content": strings.Repeat("generated code ", 60), "expected_sha256": "absent"})
			body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"reasoning_content": "Check the exact target first.", "tool_calls": []any{map[string]any{"index": 0, "id": "propose", "type": "function", "function": map[string]string{"name": "workspace.write_file", "arguments": string(arguments)}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 300, "total_tokens": 400}})
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", body)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Denied file remains unchanged.\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":8,\"total_tokens\":108}}\n\ndata: [DONE]\n\n")
	}))
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	ctx := context.Background()
	project := createTestProject(t, service, t.TempDir())
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID, ProjectID: project, ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "Propose one candidate file"}, nil)
	if err != nil || paused.Approval == nil {
		t.Fatalf("proposal did not reach review: %+v %v", paused, err)
	}
	measured, _ := service.providers.Get(ctx, provider.ID)
	if measured.ReasoningSample != 1 || measured.ReasoningRatio <= 0 || measured.ReasoningRatio >= .2 {
		t.Fatalf("actual tool output was misclassified as thought: %+v", measured)
	}
	if _, err = service.DecideApproval(ctx, paused.Approval.ID, ApprovalDecisionInput{Actor: "owner", Decision: "deny", Reason: "Diagnostic only"}, nil); err != nil {
		t.Fatal(err)
	}
	answer, _ := service.providers.Get(ctx, provider.ID)
	if answer.ReasoningSample != 2 || answer.ReasoningRatio >= measured.ReasoningRatio {
		t.Fatalf("zero-reasoning completion did not contribute a sample: %+v", answer)
	}
}

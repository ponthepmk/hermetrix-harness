package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"hermetrix-harness/internal/capabilities"
	"hermetrix-harness/internal/providers"
	toolruntime "hermetrix-harness/internal/tools"
)

func streamApprovalBatch(w http.ResponseWriter, calls ...providers.ToolCall) {
	items := []any{}
	for i, call := range calls {
		items = append(items, map[string]any{"index": i, "id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": call.Arguments}})
	}
	raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": items}, "finish_reason": "tool_calls"}}})
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
}

func TestPersonalChatCanDiscoverToolsWithoutWorkspaceAccess(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer model.Close()
	s, _, cleanup := testAgentService(t, model)
	defer cleanup()
	s.tools.SetCatalog(capabilities.NewCatalog())
	read := s.executeRegistryTool(context.Background(), Session{}, providers.ToolCall{ID: "discover", Name: "tool_search", Arguments: `{"query":"notes"}`})
	if read.Status != "succeeded" {
		t.Fatalf("personal tool discovery incorrectly requires a project: %+v", read)
	}
	workspace := s.executeRegistryTool(context.Background(), Session{}, providers.ToolCall{ID: "workspace", Name: "workspace.list_files", Arguments: `{"path":"."}`})
	if workspace.Status != "failed" || !strings.Contains(workspace.Error, "personal chat cannot run workspace tools") {
		t.Fatalf("personal chat acquired workspace access: %+v", workspace)
	}
}

func TestOrdinaryChatSerializesMixedApprovalBatchWithoutReplay(t *testing.T) {
	var modelCalls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		if modelCalls.Add(1) == 1 {
			streamApprovalBatch(w,
				providers.ToolCall{ID: "read-before", Name: "skill_search", Arguments: `{"query":"project"}`},
				providers.ToolCall{ID: "exact-open", Name: "browser", Arguments: `{"action":"open","url":"https://example.invalid/"}`},
				providers.ToolCall{ID: "later-open", Name: "browser", Arguments: `{"action":"open","url":"https://other.invalid/"}`},
				providers.ToolCall{ID: "read-after", Name: "context_search", Arguments: `{"query":"project"}`})
			return
		}
		if !strings.Contains(string(body), "Not dispatched") || !strings.Contains(string(body), "Do not repeat actions that already succeeded") {
			t.Error("continuation lacks deferred receipts")
		}
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"RECOVERED_AFTER_APPROVAL"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer model.Close()
	s, provider, cleanup := testAgentService(t, model)
	defer cleanup()
	driver := &fakeDriver{page: BrowserPage{TabID: "fixture-tab", Title: "Fixture", URL: "https://example.invalid/"}}
	s.WithRuntime(nil, driver)
	session, err := s.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID, ProjectID: createTestProject(t, s, ""), ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	start, err := s.RunTurn(context.Background(), session.ID, TurnInput{Content: "Inspect the project"}, nil)
	if err != nil || start.Approval == nil || start.Approval.ToolCallID != "exact-open" || modelCalls.Load() != 1 || len(driver.opened) != 0 {
		t.Fatalf("exact pause: %+v %v", start, err)
	}
	approvals, _ := s.ListApprovals(context.Background(), session.ID)
	if len(approvals) != 1 {
		t.Fatalf("batch authorized multiple effects: %d", len(approvals))
	}
	result, err := s.DecideApproval(context.Background(), start.Approval.ID, ApprovalDecisionInput{Actor: "local-user", Decision: "approve", Reason: "isolated fixture"}, nil)
	if err != nil || result.AssistantEvent.Content != "RECOVERED_AFTER_APPROVAL" || len(driver.opened) != 1 || modelCalls.Load() != 2 {
		t.Fatalf("continuation: %+v %v opens=%d", result, err, len(driver.opened))
	}
	events, _ := s.ListEvents(context.Background(), session.ID)
	receipts := map[string]toolruntime.Receipt{}
	for _, event := range events {
		if event.EventKind == "turn_failed" || event.EventKind == "tool_batch_rejected" {
			t.Fatalf("batch failed: %s", event.Content)
		}
		if event.EventKind == "tool_result" {
			var receipt toolruntime.Receipt
			if err := json.Unmarshal([]byte(event.Content), &receipt); err != nil {
				t.Fatal(err)
			}
			receipts[receipt.ToolCallID] = receipt
		}
	}
	if len(receipts) != 4 || receipts["read-before"].Status != "succeeded" || receipts["exact-open"].Status != "succeeded" || receipts["later-open"].Status != "deferred" || receipts["read-after"].Status != "deferred" {
		t.Fatalf("receipts=%+v", receipts)
	}
	if _, err := s.DecideApproval(context.Background(), start.Approval.ID, ApprovalDecisionInput{Actor: "local-user", Decision: "approve"}, nil); err == nil || len(driver.opened) != 1 {
		t.Fatal("approval replay was possible")
	}
}

func TestOrdinaryChatTwoWritesEachNeedExactApproval(t *testing.T) {
	var modelCalls atomic.Int32
	second := providers.ToolCall{ID: "second-deferred", Name: "workspace.write_file", Arguments: `{"path":"second.txt","content":"second","expected_sha256":"absent"}`}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		switch modelCalls.Add(1) {
		case 1:
			streamApprovalBatch(w, providers.ToolCall{ID: "first-write", Name: "workspace.write_file", Arguments: `{"path":"first.txt","content":"first","expected_sha256":"absent"}`}, second)
		case 2:
			second.ID = "second-reconsidered"
			streamApprovalBatch(w, second)
		default:
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Only the approved file was created"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
		}
	}))
	defer model.Close()
	s, provider, cleanup := testAgentService(t, model)
	defer cleanup()
	root := t.TempDir()
	session, err := s.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID, ProjectID: createTestProject(t, s, root), ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	start, err := s.RunTurn(context.Background(), session.ID, TurnInput{Content: "Create two files"}, nil)
	if err != nil || start.Approval == nil || start.Approval.ToolCallID != "first-write" {
		t.Fatalf("first approval: %+v %v", start, err)
	}
	for _, name := range []string{"first.txt", "second.txt"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("unapproved write %s", name)
		}
	}
	next, err := s.DecideApproval(context.Background(), start.Approval.ID, ApprovalDecisionInput{Actor: "local-user", Decision: "approve"}, nil)
	if err != nil || next.Approval == nil || next.Approval.ToolCallID != "second-reconsidered" {
		t.Fatalf("second exact approval: %+v %v", next, err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "first.txt")); err != nil || string(raw) != "first" {
		t.Fatalf("approved write: %q %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(root, "second.txt")); !os.IsNotExist(err) {
		t.Fatal("second write inherited first grant")
	}
	result, err := s.DecideApproval(context.Background(), next.Approval.ID, ApprovalDecisionInput{Actor: "local-user", Decision: "deny"}, nil)
	if err != nil || result.AssistantEvent.Content != "Only the approved file was created" {
		t.Fatalf("denial continuation: %+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, "second.txt")); !os.IsNotExist(err) {
		t.Fatal("denied write ran")
	}
}

func TestOrdinaryChatReadBatchNeedsNoApproval(t *testing.T) {
	var modelCalls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		if modelCalls.Add(1) == 1 {
			streamApprovalBatch(w,
				providers.ToolCall{ID: "list", Name: "workspace.list_files", Arguments: `{"path":"."}`},
				providers.ToolCall{ID: "read", Name: "workspace.read_file", Arguments: `{"path":"README.md"}`},
				providers.ToolCall{ID: "search", Name: "workspace.search_files", Arguments: `{"path":".","pattern":"read fixture"}`},
				providers.ToolCall{ID: "skills", Name: "skill_search", Arguments: `{"query":"project"}`},
				providers.ToolCall{ID: "context", Name: "context_search", Arguments: `{"query":"Read the fixture"}`})
			return
		}
		if !strings.Contains(string(body), "Perform available read-only work directly") || !strings.Contains(string(body), "read fixture") {
			t.Error("read autonomy or actual file evidence missing")
		}
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Read verified"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer model.Close()
	s, provider, cleanup := testAgentService(t, model)
	defer cleanup()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("read fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	session, err := s.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID, ProjectID: createTestProject(t, s, root), ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.RunTurn(context.Background(), session.ID, TurnInput{Content: "Read the fixture"}, nil)
	if err != nil || result.Approval != nil || result.AssistantEvent.Content != "Read verified" || modelCalls.Load() != 2 {
		t.Fatalf("automatic reads: %+v %v", result, err)
	}
	approvals, _ := s.ListApprovals(context.Background(), session.ID)
	if len(approvals) != 0 {
		t.Fatal("read required approval")
	}
	events, _ := s.ListEvents(context.Background(), session.ID)
	receipts := 0
	for _, event := range events {
		if event.EventKind == "tool_result" {
			var receipt toolruntime.Receipt
			if err := json.Unmarshal([]byte(event.Content), &receipt); err != nil || receipt.Status != "succeeded" {
				t.Fatalf("read failed: %s %v", event.Content, err)
			}
			receipts++
		}
	}
	if receipts != 5 {
		t.Fatalf("read receipts=%d", receipts)
	}
}

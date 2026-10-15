package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
)

func writeConsentProvider(t *testing.T, calls []providers.ToolCall) *httptest.Server {
	t.Helper()
	number := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		number++
		w.Header().Set("Content-Type", "text/event-stream")
		if number == 1 {
			tools := []map[string]any{}
			for index, call := range calls {
				tools = append(tools, map[string]any{"index": index, "id": call.ID, "type": "function",
					"function": map[string]string{"name": call.Name, "arguments": call.Arguments}})
			}
			encoded, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
				"delta": map[string]any{"tool_calls": tools}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", encoded)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"receipts reviewed\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
}

func TestProjectWriteConsentCreatesMultipleFilesWithoutPausing(t *testing.T) {
	startup, root := t.TempDir(), t.TempDir()
	calls := []providers.ToolCall{
		{ID: "first", Name: "workspace.write_file", Arguments: `{"path":"one.txt","content":"one","expected_sha256":"absent"}`},
		{ID: "second", Name: "workspace.write_file", Arguments: `{"path":"two.txt","content":"two","expected_sha256":"absent"}`},
	}
	service, provider, cleanup := testAgentServiceAtRoot(t, writeConsentProvider(t, calls), startup)
	defer cleanup()
	projectID := createTestProject(t, service, root)
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID,
		ProjectID: projectID, ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride(),
		WorkspaceWriteApproval: &WorkspaceWriteApprovalInput{Reason: "Owner requests project file creation"}})
	if err != nil {
		t.Fatal(err)
	}
	var pauses int
	result, err := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "create both files"}, func(event StreamEvent) error {
		if event.Type == "approval_required" {
			pauses++
		}
		return nil
	})
	if err != nil || pauses != 0 || result.Approval != nil || result.AssistantEvent.Content != "receipts reviewed" {
		t.Fatalf("automatic write flow: %+v pauses=%d error=%v", result, pauses, err)
	}
	for _, name := range []string{"one", "two"} {
		content, err := os.ReadFile(filepath.Join(root, name+".txt"))
		if err != nil || string(content) != name {
			t.Fatalf("actual file %s=%q err=%v", name, content, err)
		}
		if _, err := os.Stat(filepath.Join(startup, name+".txt")); !os.IsNotExist(err) {
			t.Fatalf("startup root written: %v", err)
		}
	}
	detail, err := service.GetSessionDetail(context.Background(), session.ID)
	if err != nil || detail.Session.State != "active" || len(detail.Approvals) != 2 {
		t.Fatalf("durable detail %+v err=%v", detail, err)
	}
	for _, approval := range detail.Approvals {
		if approval.State != "executed" || approval.ReceiptEventID == "" || approval.DecidedBy != "owner:"+session.OwnerPrincipalID || approval.Metadata["authorization"] != "project-file-writes-v1" {
			t.Fatalf("missing exact authorization: %+v", approval)
		}
		if _, err := service.DecideApproval(context.Background(), approval.ID, ApprovalDecisionInput{Actor: "user", Decision: "approve"}, nil); err == nil {
			t.Fatal("executed file grant replayed")
		}
	}
}

func TestProjectWriteConsentPreservesRootHashAndStopGuards(t *testing.T) {
	for _, scenario := range []string{"root_changed", "existing_file", "outside_root", "stop_before_write"} {
		t.Run(scenario, func(t *testing.T) {
			root, other := t.TempDir(), t.TempDir()
			call := providers.ToolCall{ID: "write", Name: "workspace.write_file", Arguments: `{"path":"guarded.txt","content":"new","expected_sha256":"absent"}`}
			if scenario == "outside_root" {
				call.Arguments = `{"path":"../outside.txt","content":"new","expected_sha256":"absent"}`
			}
			if scenario == "existing_file" {
				if err := os.WriteFile(filepath.Join(root, "guarded.txt"), []byte("owner draft"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			service, provider, cleanup := testAgentServiceAtRoot(t, writeConsentProvider(t, []providers.ToolCall{call}), t.TempDir())
			defer cleanup()
			projectID := createTestProject(t, service, root)
			session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID, ProjectID: projectID, ContextProfile: "certified-64k",
				QualificationOverride: testQualificationOverride(), WorkspaceWriteApproval: &WorkspaceWriteApprovalInput{Reason: "Owner selected automatic writes"}})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "root_changed" {
				if _, err := service.store.DB.Exec(`UPDATE projects SET root_path=? WHERE id=?`, other, projectID); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, _ = service.RunTurn(ctx, session.ID, TurnInput{Content: "try the requested write"}, func(event StreamEvent) error {
				if scenario == "stop_before_write" && event.Type == "approval_decision" {
					cancel()
				}
				return nil
			})
			for _, folder := range []string{root, other} {
				content, err := os.ReadFile(filepath.Join(folder, "guarded.txt"))
				if scenario == "existing_file" && folder == root {
					if err != nil || string(content) != "owner draft" {
						t.Fatalf("owner changes overwritten: %q %v", content, err)
					}
				} else if !os.IsNotExist(err) {
					t.Fatalf("unexpected write in %s: %q %v", folder, content, err)
				}
			}
			detail, err := service.GetSessionDetail(context.Background(), session.ID)
			if err != nil {
				t.Fatal(err)
			}
			var resultSeen bool
			for _, event := range detail.Events {
				if event.EventKind == "tool_result" && strings.Contains(event.Content, `"status":"failed"`) {
					resultSeen = true
				}
			}
			if !resultSeen {
				t.Fatal("missing failed write receipt")
			}
		})
	}
}

func TestProjectWriteConsentRejectsMissingScopeOrReason(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	for _, reason := range []string{"", "approved"} {
		_, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID, ContextProfile: "compact-32k", WorkspaceWriteApproval: &WorkspaceWriteApprovalInput{Reason: reason}})
		if err == nil {
			t.Fatal("personal conversation received file authority")
		}
	}
}

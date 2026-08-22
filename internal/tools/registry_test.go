package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hermetrix-harness/internal/capabilities"
	"hermetrix-harness/internal/providers"
)

type deferredTestExecutor struct{ calls int }

func (e *deferredTestExecutor) ExecuteCapability(_ context.Context, entry capabilities.Entry, arguments json.RawMessage) (capabilities.CallResult, error) {
	e.calls++
	return capabilities.CallResult{Output: `{"ok":true}`, Metadata: map[string]any{"remote": entry.Name}}, nil
}

func TestReadOnlyToolsStayInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello Hermetrix"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt := registry.Execute(context.Background(), providers.ToolCall{ID: "call-1", Name: "workspace.read_file", Arguments: `{"path":"hello.txt"}`})
	if receipt.Status != "succeeded" || receipt.Output != "hello Hermetrix" || receipt.Effect != "read" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	escape := registry.Execute(context.Background(), providers.ToolCall{ID: "call-2", Name: "workspace.read_file", Arguments: `{"path":"../outside"}`})
	if escape.Status != "failed" || !strings.Contains(escape.Error, "escapes") {
		t.Fatalf("path escape was not denied: %+v", escape)
	}
}

func TestSymlinkEscapeIsDenied(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt := registry.Execute(context.Background(), providers.ToolCall{ID: "call", Name: "workspace.read_file", Arguments: `{"path":"link.txt"}`})
	if receipt.Status != "failed" || !strings.Contains(receipt.Error, "escapes") {
		t.Fatalf("symlink escape was not denied: %+v", receipt)
	}
}

func TestCapabilityRevisionAndDefinitionsAreDeterministic(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Definitions()) != 6 || len(registry.ProviderDefinitions()) != 6 || len(registry.ContextSpecs()) != 6 {
		t.Fatal("expected three workspace tools and three deferred capability primitives")
	}
	if registry.Revision() == "" || registry.Revision() != registry.Revision() {
		t.Fatal("capability revision must be deterministic")
	}
}

func TestDeferredSearchDescribeCallAndDynamicApproval(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog := capabilities.NewCatalog()
	executor := &deferredTestExecutor{}
	catalog.SetExecutor(capabilities.SourceMCP, executor)
	entries := []capabilities.Entry{
		{ID: "mcp:server:read", Name: "lookup", Description: "read lookup", Source: capabilities.SourceMCP,
			SourceRef: "server", Revision: "read-r1", Effect: "read", Readiness: capabilities.ReadinessReady,
			InputSchema: json.RawMessage(`{"type":"object"}`)},
		{ID: "mcp:server:write", Name: "publish", Description: "publish change", Source: capabilities.SourceMCP,
			SourceRef: "server", Revision: "write-r1", Effect: "external_mutation", Readiness: capabilities.ReadinessReady,
			RequiresApproval: true, InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	if err := catalog.ReplaceSourceRef(capabilities.SourceMCP, "server", entries); err != nil {
		t.Fatal(err)
	}
	registry.SetCatalog(catalog)
	search := registry.Execute(context.Background(), providers.ToolCall{ID: "search", Name: "tool_search", Arguments: `{"query":"lookup"}`})
	if search.Status != "succeeded" || strings.Contains(search.Output, "input_schema") || strings.Contains(search.Output, "read-r1") {
		t.Fatalf("search leaked deferred binding: %+v", search)
	}
	describe := registry.Execute(context.Background(), providers.ToolCall{ID: "describe", Name: "tool_describe",
		Arguments: `{"capability_id":"mcp:server:read"}`})
	if describe.Status != "succeeded" || !strings.Contains(describe.Output, "read-r1") || !strings.Contains(describe.Output, "input_schema") {
		t.Fatalf("describe = %+v", describe)
	}
	readCall := providers.ToolCall{ID: "read-call", Name: "tool_call",
		Arguments: `{"capability_id":"mcp:server:read","revision":"read-r1","arguments":{}}`}
	readReceipt := registry.Execute(context.Background(), readCall)
	if readReceipt.Status != "succeeded" || readReceipt.Effect != "read" || executor.calls != 1 {
		t.Fatalf("read receipt=%+v calls=%d", readReceipt, executor.calls)
	}

	writeCall := providers.ToolCall{ID: "write-call", Name: "tool_call",
		Arguments: `{"capability_id":"mcp:server:write","revision":"write-r1","arguments":{"value":"x"}}`}
	requires, err := registry.RequiresApproval(writeCall)
	if err != nil || !requires {
		t.Fatalf("requires approval=%v err=%v", requires, err)
	}
	if receipt := registry.Execute(context.Background(), writeCall); receipt.Status != "failed" || !strings.Contains(receipt.Error, "approval") {
		t.Fatalf("effectful call ran without approval: %+v", receipt)
	}
	plan, err := registry.PlanApproval(context.Background(), writeCall)
	if err != nil || plan.Effect != "external_mutation" || plan.Metadata["automatic_retry"] != false {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	wrong := registry.ExecuteApproved(context.Background(), writeCall, ApprovalGrant{ToolCallID: writeCall.ID, Name: writeCall.Name,
		Revision: plan.Revision, Effect: plan.Effect, ArgumentsHash: "wrong"})
	if wrong.Status != "failed" || executor.calls != 1 {
		t.Fatalf("wrong grant was executed: %+v calls=%d", wrong, executor.calls)
	}
	approved := registry.ExecuteApproved(context.Background(), writeCall, ApprovalGrant{ToolCallID: writeCall.ID, Name: writeCall.Name,
		Revision: plan.Revision, Effect: plan.Effect, ArgumentsHash: plan.ArgumentsHash})
	if approved.Status != "succeeded" || approved.Effect != "external_mutation" || executor.calls != 2 {
		t.Fatalf("approved=%+v calls=%d", approved, executor.calls)
	}
}

func TestDeferredApprovalFailsClosedOnCatalogRevisionDrift(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog := capabilities.NewCatalog()
	executor := &deferredTestExecutor{}
	catalog.SetExecutor(capabilities.SourceMCP, executor)
	entry := capabilities.Entry{ID: "mcp:s:effect", Name: "effect", Description: "effect", Source: capabilities.SourceMCP,
		SourceRef: "s", Revision: "r1", Effect: "unknown", Readiness: capabilities.ReadinessReady, RequiresApproval: true,
		InputSchema: json.RawMessage(`{"type":"object"}`)}
	if err := catalog.ReplaceSourceRef(capabilities.SourceMCP, "s", []capabilities.Entry{entry}); err != nil {
		t.Fatal(err)
	}
	registry.SetCatalog(catalog)
	call := providers.ToolCall{ID: "drift", Name: "tool_call", Arguments: `{"capability_id":"mcp:s:effect","revision":"r1","arguments":{}}`}
	plan, err := registry.PlanApproval(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	entry.Revision = "r2"
	if err := catalog.ReplaceSourceRef(capabilities.SourceMCP, "s", []capabilities.Entry{entry}); err != nil {
		t.Fatal(err)
	}
	receipt := registry.ExecuteApproved(context.Background(), call, ApprovalGrant{ToolCallID: call.ID, Name: call.Name,
		Revision: plan.Revision, Effect: plan.Effect, ArgumentsHash: plan.ArgumentsHash})
	if receipt.Status != "failed" || !strings.Contains(receipt.Error, "revision changed") || executor.calls != 0 {
		t.Fatalf("revision drift was not fail-closed: %+v calls=%d", receipt, executor.calls)
	}
}

func TestWriteRequiresExactApprovalGrant(t *testing.T) {
	root := t.TempDir()
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	call := providers.ToolCall{ID: "call-write", Name: "workspace.write_file",
		Arguments: `{"path":"new.txt","content":"hello from approval","expected_sha256":"absent"}`}
	withoutGrant := registry.Execute(context.Background(), call)
	if withoutGrant.Status != "failed" || !strings.Contains(withoutGrant.Error, "approval") {
		t.Fatalf("write ran without approval: %+v", withoutGrant)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("unapproved write changed the workspace: %v", err)
	}
	plan, err := registry.PlanApproval(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	wrong := registry.ExecuteApproved(context.Background(), call, ApprovalGrant{ToolCallID: call.ID, Name: call.Name,
		Revision: plan.Revision, Effect: plan.Effect, ArgumentsHash: "wrong"})
	if wrong.Status != "failed" || !strings.Contains(wrong.Error, "does not match") {
		t.Fatalf("mismatched grant was accepted: %+v", wrong)
	}
	receipt := registry.ExecuteApproved(context.Background(), call, ApprovalGrant{ToolCallID: call.ID, Name: call.Name,
		Revision: plan.Revision, Effect: plan.Effect, ArgumentsHash: plan.ArgumentsHash})
	if receipt.Status != "succeeded" || receipt.Effect != "write" || receipt.Metadata["atomic"] != true {
		t.Fatalf("approved write failed: %+v", receipt)
	}
	content, err := os.ReadFile(filepath.Join(root, "new.txt"))
	if err != nil || string(content) != "hello from approval" {
		t.Fatalf("unexpected written content %q err=%v", content, err)
	}
}

func TestApprovedWriteFailsClosedWhenFileChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "existing.txt")
	original := []byte("original")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(original)
	call := providers.ToolCall{ID: "call-stale", Name: "workspace.write_file", Arguments: `{"path":"existing.txt","content":"replacement","expected_sha256":"` + hex.EncodeToString(sum[:]) + `"}`}
	plan, err := registry.PlanApproval(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed while waiting"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt := registry.ExecuteApproved(context.Background(), call, ApprovalGrant{ToolCallID: call.ID, Name: call.Name,
		Revision: plan.Revision, Effect: plan.Effect, ArgumentsHash: plan.ArgumentsHash})
	if receipt.Status != "failed" || !strings.Contains(receipt.Error, "optimistic write check failed") {
		t.Fatalf("stale write was not rejected: %+v", receipt)
	}
	content, _ := os.ReadFile(path)
	if string(content) != "changed while waiting" {
		t.Fatalf("stale approval overwrote newer content: %q", content)
	}
}

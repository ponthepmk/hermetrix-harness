package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"hermetrix-harness/internal/capabilities"
	ctxcompiler "hermetrix-harness/internal/context"
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
	// Assert the exact set rather than a bare count. A count alone fails on any
	// deliberate change and says nothing about what leaked in; the set fails
	// only when the waist actually changes, and names the offender.
	want := []string{
		"skill_search", "skill_view", // session-scoped Skill retrieval
		"tool_call", "tool_describe", "tool_search", // deferred capability catalog
		"workspace.list_files", "workspace.read_file", "workspace.search_files", "workspace.write_file",
	}
	var got []string
	for _, definition := range registry.Definitions() {
		got = append(got, definition.Name)
	}
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("direct tool waist is\n  %v\nwant\n  %v", got, want)
	}
	if len(registry.ProviderDefinitions()) != len(want) || len(registry.ContextSpecs()) != len(want) {
		t.Fatalf("provider definitions %d and context specs %d disagree with %d definitions",
			len(registry.ProviderDefinitions()), len(registry.ContextSpecs()), len(want))
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

// --- O-3: direct-tool token accounting ---
//
// ContextSpecs used to hand the estimator only the parameter schema, dropping
// the description and the provider's function wrapper. The compiler budget
// therefore passed on a number smaller than the request it was approving.
func TestContextSpecsCountTheExactProviderPayload(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	specs := registry.ContextSpecs()
	definitions := registry.ProviderDefinitions()
	if len(specs) != len(definitions) || len(specs) == 0 {
		t.Fatalf("specs=%d provider definitions=%d", len(specs), len(definitions))
	}
	for index, spec := range specs {
		definition := definitions[index]
		if spec.Name != definition.Function.Name {
			t.Fatalf("spec %d is %q but provider definition %d is %q", index, spec.Name, index, definition.Function.Name)
		}
		expected, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		if spec.Serialized != string(expected) {
			t.Fatalf("tool %q bills\n  %s\nbut the provider receives\n  %s", spec.Name, spec.Serialized, expected)
		}
		if definition.Function.Description == "" {
			t.Fatalf("tool %q has no description, so this test cannot prove the description is counted", spec.Name)
		}
		if !strings.Contains(spec.BillableText(), definition.Function.Description) {
			t.Fatalf("tool %q does not bill its description", spec.Name)
		}
	}
}

// The gap is not academic: the old accounting understated the direct-tool
// slice, and this records by how much so a regression is visible as a number.
func TestBillableTextIsLargerThanTheBareSchema(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bare, billed := 0, 0
	for _, spec := range registry.ContextSpecs() {
		bare += len(spec.Name + "\n" + spec.Schema)
		billed += len(spec.BillableText())
	}
	if billed <= bare {
		t.Fatalf("billable payload %d bytes is not larger than name+schema %d bytes", billed, bare)
	}
	t.Logf("direct-tool payload: %d bytes billed, %d bytes under the old accounting (+%.0f%%)",
		billed, bare, float64(billed-bare)/float64(bare)*100)
}

// With the real payload counted, the smallest envelope must still fit the
// direct tools. If this fails, the tool waist has outgrown Compact 32k and the
// fix is fewer or smaller tools, not a bigger budget.
func TestRealPayloadFitsEveryProfileDirectToolBudget(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	estimator := ctxcompiler.NewAdaptiveEstimator()
	used := 0
	for _, spec := range registry.ContextSpecs() {
		used += estimator.Count(spec.BillableText())
	}
	for _, profile := range ctxcompiler.Profiles() {
		if used > profile.DirectToolBudget {
			t.Fatalf("direct tools bill %d tokens, over the %s budget of %d", used, profile.Name, profile.DirectToolBudget)
		}
		t.Logf("%s: %d of %d direct-tool tokens used", profile.Name, used, profile.DirectToolBudget)
	}
}

// --- O-14: a value in the middle of a large file must be reachable ---
//
// Driving a real model at a 1400-line file put the answer at line 700. The file
// spilled to an artifact, the model saw its head and tail, said honestly that
// it could not find the rule, and named the reason: no way to search. Reading
// returned the whole file or nothing.

func searchWorkspace(t *testing.T) (*Registry, string) {
	t.Helper()
	root := t.TempDir()
	var builder strings.Builder
	for line := 1; line <= 1400; line++ {
		if line == 700 {
			builder.WriteString("CRITICAL RULE 4242: never net NAKHON against PHUKET.\n")
			continue
		}
		fmt.Fprintf(&builder, "%d. reconcile ledger batch %04d\n", line, line)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte(builder.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	return registry, root
}

func TestSearchFindsAValueBuriedInTheMiddleOfAFile(t *testing.T) {
	registry, _ := searchWorkspace(t)
	receipt := registry.Execute(context.Background(), providers.ToolCall{ID: "c1", Name: "workspace.search_files",
		Arguments: `{"pattern":"RULE 4242","path":"."}`})
	if receipt.Status != "succeeded" {
		t.Fatalf("search failed: %+v", receipt)
	}
	if !strings.Contains(receipt.Output, "notes.md:700:") {
		t.Fatalf("search did not report the line the value sits on:\n%s", receipt.Output)
	}
	if !strings.Contains(receipt.Output, "NAKHON") {
		t.Fatalf("search did not return the matching line:\n%s", receipt.Output)
	}
}

func TestReadFileWindowReachesTheMiddleAndReportsTheWhole(t *testing.T) {
	registry, _ := searchWorkspace(t)
	receipt := registry.Execute(context.Background(), providers.ToolCall{ID: "c2", Name: "workspace.read_file",
		Arguments: `{"path":"notes.md","offset_line":698,"max_lines":5}`})
	if receipt.Status != "succeeded" {
		t.Fatalf("windowed read failed: %+v", receipt)
	}
	if !strings.Contains(receipt.Output, "RULE 4242") {
		t.Fatalf("window did not contain the requested lines:\n%s", receipt.Output)
	}
	if lines := strings.Count(receipt.Output, "\n") + 1; lines > 5 {
		t.Fatalf("window returned %d lines, want at most 5", lines)
	}
	if receipt.Metadata["total_lines"] == nil || receipt.Metadata["offset_line"] == nil {
		t.Fatalf("window receipt does not say where it sits in the file: %+v", receipt.Metadata)
	}
	// The hash must stay the hash of the whole file, or a windowed read could
	// satisfy the expected_sha256 that guards a write.
	whole := registry.Execute(context.Background(), providers.ToolCall{ID: "c3", Name: "workspace.read_file",
		Arguments: `{"path":"notes.md"}`})
	if receipt.Metadata["sha256"] != whole.Metadata["sha256"] {
		t.Fatal("a windowed read reported a different file hash than the whole file")
	}
}

func TestSearchIsBoundedAndRejectsABadPattern(t *testing.T) {
	registry, _ := searchWorkspace(t)
	capped := registry.Execute(context.Background(), providers.ToolCall{ID: "c4", Name: "workspace.search_files",
		Arguments: `{"pattern":"reconcile","path":".","max_matches":10}`})
	if capped.Status != "succeeded" {
		t.Fatalf("bounded search failed: %+v", capped)
	}
	if matches, _ := capped.Metadata["matches"].(int); matches != 10 {
		t.Fatalf("search returned %v matches, want the requested cap of 10", capped.Metadata["matches"])
	}
	if capped.Metadata["truncated"] != true {
		t.Fatalf("a capped search did not say it was truncated: %+v", capped.Metadata)
	}
	broken := registry.Execute(context.Background(), providers.ToolCall{ID: "c5", Name: "workspace.search_files",
		Arguments: `{"pattern":"([unclosed","path":"."}`})
	if broken.Status != "failed" || !strings.Contains(broken.Error, "does not compile") {
		t.Fatalf("an invalid pattern was not reported: %+v", broken)
	}
}

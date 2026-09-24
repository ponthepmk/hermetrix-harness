package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

type testExecutor struct{ calls int }

func (e *testExecutor) ExecuteCapability(_ context.Context, _ Entry, _ json.RawMessage) (CallResult, error) {
	e.calls++
	return CallResult{Output: "ok"}, nil
}

func TestCatalogSearchDoesNotExposeSchemasAndScales(t *testing.T) {
	catalog := NewCatalog()
	entries := make([]Entry, 0, 1500)
	for i := 0; i < 1500; i++ {
		entries = append(entries, Entry{ID: fmt.Sprintf("mcp:server:%04d", i), Name: fmt.Sprintf("tool_%04d", i),
			Description: "bounded deferred capability", Source: SourceMCP, SourceRef: "server", Revision: "r1", Effect: "read",
			Readiness: ReadinessReady, InputSchema: json.RawMessage(`{"type":"object"}`)})
	}
	if err := catalog.ReplaceSourceRef(SourceMCP, "server", entries); err != nil {
		t.Fatal(err)
	}
	results := catalog.Search("tool_1499", "", 10)
	if len(results) != 1 || results[0].Name != "tool_1499" {
		t.Fatalf("unexpected search: %+v", results)
	}
	encoded, _ := json.Marshal(results)
	if len(encoded) > 1000 || string(encoded) == "" {
		t.Fatalf("search payload should be bounded and non-empty: %d", len(encoded))
	}
	if summary := catalog.Summary(); summary.Total != 1500 || summary.BySource[SourceMCP] != 1500 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestCatalogExactRevisionAndAtomicReplacement(t *testing.T) {
	catalog := NewCatalog()
	executor := &testExecutor{}
	catalog.SetExecutor(SourceMCP, executor)
	entry := Entry{ID: "mcp:s:echo", Name: "echo", Description: "echo", Source: SourceMCP, SourceRef: "s",
		Revision: "r1", Effect: "read", Readiness: ReadinessReady, InputSchema: json.RawMessage(`{"type":"object"}`)}
	if err := catalog.ReplaceSourceRef(SourceMCP, "s", []Entry{entry}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalog.Call(context.Background(), entry.ID, "stale", json.RawMessage(`{}`)); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision error = %v", err)
	}
	result, _, err := catalog.Call(context.Background(), entry.ID, "r1", json.RawMessage(`{}`))
	if err != nil || result.Output != "ok" || executor.calls != 1 {
		t.Fatalf("call result=%+v calls=%d err=%v", result, executor.calls, err)
	}
	entry.ID, entry.Name, entry.Revision = "mcp:s:new", "new", "r2"
	if err := catalog.ReplaceSourceRef(SourceMCP, "s", []Entry{entry}); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Describe("mcp:s:echo"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old snapshot survived replacement: %v", err)
	}
}

func TestCatalogPreservesTrustedMetadataAcrossClones(t *testing.T) {
	catalog := NewCatalog()
	catalog.SetExecutor(SourceMCP, &testExecutor{})
	entry := Entry{ID: "mcp:brain:get_context", Name: "get_context", Source: SourceMCP,
		SourceRef: "brain", Revision: "r1", Effect: "read", Readiness: ReadinessReady,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Metadata:    map[string]any{"annotations_trusted": true, "kind": "tool"}}
	if err := catalog.ReplaceSourceRef(SourceMCP, "brain", []Entry{entry}); err != nil {
		t.Fatal(err)
	}
	entry.Metadata["annotations_trusted"] = false
	described, err := catalog.Describe(entry.ID)
	if err != nil || described.Metadata["annotations_trusted"] != true || described.Metadata["kind"] != "tool" {
		t.Fatalf("trusted MCP metadata was lost on catalog replacement: metadata=%v err=%v", described.Metadata, err)
	}
	described.Metadata["annotations_trusted"] = false
	_, called, err := catalog.Call(context.Background(), entry.ID, entry.Revision, json.RawMessage(`{}`))
	if err != nil || called.Metadata["annotations_trusted"] != true {
		t.Fatalf("trusted MCP metadata was lost on capability call: metadata=%v err=%v", called.Metadata, err)
	}
}

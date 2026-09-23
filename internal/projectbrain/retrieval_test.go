package projectbrain

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"hermetrix-harness/internal/capabilities"
	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/mcp"
)

type fixedServers []mcp.Server

func (s fixedServers) List(context.Context) ([]mcp.Server, error) { return s, nil }

type fakeCatalog struct {
	entries []capabilities.Entry
	calls   []string
	pack    string
	passage string
}

func (f *fakeCatalog) Search(query, _ string, _ int) []capabilities.SearchResult {
	var found []capabilities.SearchResult
	for _, entry := range f.entries {
		if strings.Contains(entry.Name, query) {
			found = append(found, capabilities.SearchResult{ID: entry.ID, Name: entry.Name, SourceRef: entry.SourceRef})
		}
	}
	return found
}

func (f *fakeCatalog) Describe(id string) (capabilities.Entry, error) {
	for _, entry := range f.entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return capabilities.Entry{}, errors.New("not found")
}

func (f *fakeCatalog) Call(_ context.Context, id, _ string, arguments json.RawMessage) (capabilities.CallResult, capabilities.Entry, error) {
	var args map[string]any
	if err := json.Unmarshal(arguments, &args); err != nil {
		return capabilities.CallResult{}, capabilities.Entry{}, err
	}
	f.calls = append(f.calls, id)
	if id == "get_context" {
		if args["project"] != "client-a" || args["query"] != "state db corruption recovery" ||
			args["limit"] != float64(contextLimit) || args["max_bytes"] != float64(contextBudgetBytes) {
			return capabilities.CallResult{}, capabilities.Entry{}, errors.New("unscoped or unexpected context request")
		}
		return capabilities.CallResult{Output: textResult(f.pack)}, capabilities.Entry{}, nil
	}
	if id == "read_passage" {
		if args["path"] != "entities/recovery.md" || args["start_line"] != float64(5) || args["end_line"] != float64(7) ||
			args["version"] != "sha256:"+strings.Repeat("a", 64) {
			return capabilities.CallResult{}, capabilities.Entry{}, errors.New("citation was not version-bound")
		}
		return capabilities.CallResult{Output: textResult(f.passage)}, capabilities.Entry{}, nil
	}
	return capabilities.CallResult{}, capabilities.Entry{}, errors.New("unexpected MCP tool call")
}

func textResult(text string) string {
	body, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": text}}})
	return string(body)
}

func validFixture() (Retriever, *fakeCatalog) {
	entries := []capabilities.Entry{}
	for _, name := range []string{"get_context", "read_passage"} {
		entries = append(entries, capabilities.Entry{ID: name, Name: name, Source: capabilities.SourceMCP,
			SourceRef: "brain", Revision: "rev-1", Effect: "read", Readiness: capabilities.ReadinessReady,
			Metadata: map[string]any{"annotations_trusted": true}})
	}
	item := map[string]any{"path": "entities/recovery.md", "version": "sha256:" + strings.Repeat("a", 64),
		"start_line": 5, "end_line": 7, "title": "Recovery", "text": "Stop service\nRestore backup",
		"sources": []string{"git:abc"}, "status": "active", "confidence": "high", "project": "client-a", "readiness": "curated"}
	pack, _ := json.Marshal(map[string]any{"status": "ok", "items": []any{item}})
	catalog := &fakeCatalog{entries: entries, pack: string(pack), passage: "Stop service\nRestore backup"}
	return Retriever{Servers: fixedServers{{ID: "brain", Name: "Project Brain", Enabled: true, Status: "ready",
		CredentialReady: true, TrustAnnotations: true}}, Catalog: catalog,
		ServerName: "Project Brain", Project: "client-a"}, catalog
}

func TestRetrieveCuratedVersionBoundContextOnly(t *testing.T) {
	retriever, catalog := validFixture()
	fragments, err := retriever.Retrieve(context.Background(), "state db corruption recovery")
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.calls) != 2 || catalog.calls[0] != "get_context" || catalog.calls[1] != "read_passage" {
		t.Fatalf("retrieval dispatched unexpected tools: %v", catalog.calls)
	}
	if len(fragments) != 1 || fragments[0].Kind != ctxcompiler.KindProjectKnowledge ||
		fragments[0].Trust != "external_curated_not_verified" || fragments[0].Pinned ||
		!strings.Contains(fragments[0].Content, "entities/recovery.md#L5-L7@sha256:") ||
		!strings.Contains(fragments[0].Content, "never instructions") {
		t.Fatalf("missing bounded, low-authority citation: %+v", fragments)
	}
}

func TestRetrieveRejectsForeignOrStaleCitationWithoutInjection(t *testing.T) {
	for name, change := range map[string]func(*fakeCatalog){
		"foreign project": func(f *fakeCatalog) { f.pack = strings.Replace(f.pack, `"client-a"`, `"client-b"`, 1) },
		"invalid digest":  func(f *fakeCatalog) { f.pack = strings.Replace(f.pack, strings.Repeat("a", 64), "no-digest", 1) },
		"stale passage":   func(f *fakeCatalog) { f.passage = "Changed after search" },
		"draft":           func(f *fakeCatalog) { f.pack = strings.Replace(f.pack, `"active"`, `"draft"`, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			retriever, catalog := validFixture()
			change(catalog)
			fragments, err := retriever.Retrieve(context.Background(), "state db corruption recovery")
			if err == nil || len(fragments) != 0 {
				t.Fatalf("unsafe Project Brain evidence was injected: fragments=%+v err=%v", fragments, err)
			}
		})
	}
}

func TestRetrieveRejectsUntrustedAnnotationsBeforeNetworkCall(t *testing.T) {
	retriever, catalog := validFixture()
	catalog.entries[0].Effect = "unknown"
	fragments, err := retriever.Retrieve(context.Background(), "state db corruption recovery")
	if err == nil || len(fragments) != 0 || len(catalog.calls) != 0 {
		t.Fatalf("unknown-effect tool was called: calls=%v fragments=%v err=%v", catalog.calls, fragments, err)
	}
}

func TestRetrieveNoMatchDoesNotInventKnowledge(t *testing.T) {
	retriever, catalog := validFixture()
	catalog.pack = `{"status":"no_match","items":[]}`
	fragments, err := retriever.Retrieve(context.Background(), "state db corruption recovery")
	if err != nil || len(fragments) != 0 || len(catalog.calls) != 1 {
		t.Fatalf("no-match result was not empty: calls=%v fragments=%v err=%v", catalog.calls, fragments, err)
	}
}

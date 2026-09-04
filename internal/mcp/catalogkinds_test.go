package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestCurrentCatalogRequestsDeriveRequiredMcpName(t *testing.T) {
	tests := []struct {
		method string
		params map[string]any
		want   string
	}{
		{method: "resources/read", params: map[string]any{"uri": "file:///project/readme.md"}, want: "file:///project/readme.md"},
		{method: "prompts/get", params: map[string]any{"name": "review"}, want: "review"},
		{method: "tools/call", params: map[string]any{"name": "echo"}, want: "echo"},
		{method: "resources/list", params: map[string]any{"cursor": "next"}, want: ""},
	}
	for _, test := range tests {
		if got := standardRequestName(test.method, test.params); got != test.want {
			t.Errorf("%s Mcp-Name = %q, want %q", test.method, got, test.want)
		}
	}
}

func TestCurrentResourceAndPromptRequestsSendRequiredMcpName(t *testing.T) {
	var seenMu sync.Mutex
	seen := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		seenMu.Lock()
		seen[request.Method] = r.Header.Get("Mcp-Name")
		seenMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "resources/read":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"contents":[]}}`, request.ID)
		case "prompts/get":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"messages":[]}}`, request.ID)
		default:
			http.Error(w, "unexpected method", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	client := NewClient(nil)
	profile := Server{ID: "standard-headers", Endpoint: server.URL, ProtocolMode: ProtocolCurrent}
	if _, err := client.ReadResource(context.Background(), profile, "", "file:///project/readme.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetPrompt(context.Background(), profile, "", "review", nil); err != nil {
		t.Fatal(err)
	}
	seenMu.Lock()
	resourceName, promptName := seen["resources/read"], seen["prompts/get"]
	seenMu.Unlock()
	if resourceName != "file:///project/readme.md" {
		t.Errorf("resources/read Mcp-Name = %q", resourceName)
	}
	if promptName != "review" {
		t.Errorf("prompts/get Mcp-Name = %q", promptName)
	}
}

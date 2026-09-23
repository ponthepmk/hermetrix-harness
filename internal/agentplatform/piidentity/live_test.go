package piidentity

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"hermetrix-harness/internal/mcp"
)

// Optional read-only discovery check against the actual Pi MCP endpoint. It
// sends no credential and invokes no tool. The four names and input schemas
// must match the source-level P2 contract before an authenticated probe.
func TestLivePiToolDiscovery(t *testing.T) {
	endpoint := os.Getenv("H2A_LIVE_MCP_ENDPOINT")
	if endpoint == "" {
		t.Skip("live Pi endpoint not configured")
	}
	if err := validateEndpoint(endpoint); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(nil)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tools, protocol, err := client.ListTools(ctx, mcp.Server{ID: ServerID, Endpoint: endpoint, ProtocolMode: mcp.ProtocolAuto}, "")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, tool := range tools {
		if !allowed[tool.Name] || len(tool.InputSchema) == 0 {
			continue
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("invalid %s schema: %v", tool.Name, err)
		}
		if tool.Name == "get_node" && schema.Properties["node_key"] == nil {
			t.Fatal("Pi get_node lacks node_key input")
		}
		if tool.Name == "get_agent" && schema.Properties["agent_key"] == nil {
			t.Fatal("Pi get_agent lacks agent_key input")
		}
		found[tool.Name] = true
	}
	for name := range allowed {
		if !found[name] {
			t.Errorf("Pi MCP missing tool %s", name)
		}
	}
	t.Logf("Pi MCP protocol=%s required_read_tools=%d", protocol, len(found))
}

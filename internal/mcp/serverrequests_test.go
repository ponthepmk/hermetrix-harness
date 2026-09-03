package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// bidirectionalFixture asks the client something in the middle of answering a
// tool call, which is what sampling and elicitation actually are. A stub that
// only ever answers would not prove the pump works.
const bidirectionalFixture = `import json, sys
def send(p):
    sys.stdout.write(json.dumps(p) + "\n"); sys.stdout.flush()

def ask(rid, method, params):
    send({"jsonrpc": "2.0", "id": rid, "method": method, "params": params})
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        m = json.loads(line)
        if m.get("id") == rid and ("result" in m or "error" in m):
            return m
    return {"error": {"code": -1, "message": "no answer"}}

for line in sys.stdin:
    line = line.strip()
    if not line: continue
    m = json.loads(line); method = m.get("method")
    if method == "initialize":
        send({"jsonrpc": "2.0", "id": m["id"], "result": {"protocolVersion": "2025-11-25",
              "capabilities": {"tools": {}}, "clientCapabilities": m["params"].get("capabilities", {})}})
    elif method == "tools/list":
        send({"jsonrpc": "2.0", "id": m["id"], "result": {"tools": [
            {"name": "ask_then_answer", "description": "Ask the client something first",
             "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False}}]}})
    elif method == "tools/call":
        which = m["params"]["arguments"].get("which", "sample")
        if which == "sample":
            reply = ask(9001, "sampling/createMessage", {
                "messages": [{"role": "user", "content": {"type": "text", "text": "name one colour"}}],
                "maxTokens": 16})
        else:
            reply = ask(9002, "elicitation/create", {
                "message": "Which environment should I deploy to?",
                "requestedSchema": {"type": "object", "properties": {"env": {"type": "string"}}, "required": ["env"]}})
        send({"jsonrpc": "2.0", "id": m["id"], "result": {
            "content": [{"type": "text", "text": json.dumps(reply)}], "isError": False}})
`

func writeBidirectionalFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	path := filepath.Join(t.TempDir(), "bidi.py")
	if err := os.WriteFile(path, []byte(bidirectionalFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// recordingHandler stands in for the agent bridge.
type recordingHandler struct {
	mu       sync.Mutex
	sampled  []json.RawMessage
	elicited []json.RawMessage
	sample   func(json.RawMessage) (json.RawMessage, error)
	elicit   func(json.RawMessage) (json.RawMessage, error)
}

func (h *recordingHandler) Sample(_ context.Context, _ Server, params json.RawMessage) (json.RawMessage, error) {
	h.mu.Lock()
	h.sampled = append(h.sampled, params)
	h.mu.Unlock()
	if h.sample != nil {
		return h.sample(params)
	}
	return SamplingResult("test-model", "blue"), nil
}

func (h *recordingHandler) Elicit(_ context.Context, _ Server, params json.RawMessage) (json.RawMessage, error) {
	h.mu.Lock()
	h.elicited = append(h.elicited, params)
	h.mu.Unlock()
	if h.elicit != nil {
		return h.elicit(params)
	}
	return ElicitationAccepted(map[string]any{"env": "staging"}), nil
}

// TestServerCanSampleAndElicitDuringAToolCall is the whole point of the
// bidirectional pump: a request arriving while we wait for our own response is
// answered on the same pipe, and the tool call then completes normally.
func TestServerCanSampleAndElicitDuringAToolCall(t *testing.T) {
	script := writeBidirectionalFixture(t)
	handler := &recordingHandler{}
	client := NewClient(nil).WithHandler(handler)
	t.Cleanup(client.Close)
	server := Server{ID: "bidi", Name: "Bidirectional", TransportKind: TransportStdio,
		Endpoint: "python3 " + script, RequestTimeoutMS: 15000, Enabled: true, TrustAnnotations: true}
	ctx := context.Background()

	sampled, err := client.CallToolStdio(ctx, server, "", "ask_then_answer", []byte(`{"which":"sample"}`))
	if err != nil {
		t.Fatalf("tool call with sampling: %v", err)
	}
	if !strings.Contains(string(sampled.Result), "blue") {
		t.Errorf("the server did not receive the sampled text: %s", sampled.Result)
	}
	if len(handler.sampled) != 1 {
		t.Fatalf("handler saw %d sampling requests, want 1", len(handler.sampled))
	}
	if !strings.Contains(string(handler.sampled[0]), "name one colour") {
		t.Errorf("sampling params did not reach the handler: %s", handler.sampled[0])
	}

	elicited, err := client.CallToolStdio(ctx, server, "", "ask_then_answer", []byte(`{"which":"elicit"}`))
	if err != nil {
		t.Fatalf("tool call with elicitation: %v", err)
	}
	if !strings.Contains(string(elicited.Result), "staging") {
		t.Errorf("the server did not receive the user's answer: %s", elicited.Result)
	}
	if len(handler.elicited) != 1 || !strings.Contains(string(handler.elicited[0]), "Which environment") {
		t.Errorf("elicitation params did not reach the handler: %+v", handler.elicited)
	}
}

// TestClientWithoutHandlerRefusesRatherThanHangs keeps a server from waiting
// forever on a request this build cannot answer.
func TestClientWithoutHandlerRefusesRatherThanHangs(t *testing.T) {
	script := writeBidirectionalFixture(t)
	client := NewClient(nil)
	t.Cleanup(client.Close)
	server := Server{ID: "bidi", Name: "Bidirectional", TransportKind: TransportStdio,
		Endpoint: "python3 " + script, RequestTimeoutMS: 15000, Enabled: true}
	result, err := client.CallToolStdio(context.Background(), server, "", "ask_then_answer", []byte(`{"which":"sample"}`))
	if err != nil {
		t.Fatalf("the call hung or failed instead of refusing cleanly: %v", err)
	}
	if !strings.Contains(string(result.Result), "-32601") {
		t.Errorf("the server was not told the request is unsupported: %s", result.Result)
	}
}

// TestCapabilitiesAreOnlyDeclaredWhenTheyCanBeAnswered stops Hermetrix telling
// a server it supports sampling and then refusing every request.
func TestCapabilitiesAreOnlyDeclaredWhenTheyCanBeAnswered(t *testing.T) {
	if got := NewClient(nil).clientCapabilities(); len(got) != 0 {
		t.Errorf("a client with no handler declared %v", got)
	}
	got := NewClient(nil).WithHandler(&recordingHandler{}).clientCapabilities()
	if _, ok := got["sampling"]; !ok {
		t.Errorf("sampling was not declared: %v", got)
	}
	if _, ok := got["elicitation"]; !ok {
		t.Errorf("elicitation was not declared: %v", got)
	}
}

// TestSamplingRequestIsBounded pins the caps on remote input: the server writes
// the prompt, so it does not also get to choose the size or the budget.
func TestSamplingRequestIsBounded(t *testing.T) {
	if _, err := DecodeSamplingRequest([]byte(`{"messages":[]}`), 512); err == nil {
		t.Error("an empty sampling request was accepted")
	}
	if _, err := DecodeSamplingRequest([]byte(`not json`), 512); err == nil {
		t.Error("invalid JSON was accepted")
	}
	request, err := DecodeSamplingRequest([]byte(`{"messages":[{"role":"user","content":{"type":"text","text":"hi"}}],"maxTokens":999999}`), 512)
	if err != nil {
		t.Fatal(err)
	}
	if request.MaxTokens != 512 {
		t.Errorf("maxTokens = %d, want the ceiling 512", request.MaxTokens)
	}
}

// TestElicitationAnswersAreTheThreeMCPActions keeps a timeout from being
// reported to the server as a refusal by the user.
func TestElicitationAnswersAreTheThreeMCPActions(t *testing.T) {
	if !strings.Contains(string(ElicitationAccepted(map[string]any{"a": "b"})), `"accept"`) {
		t.Error("accepted answer is not an accept")
	}
	if !strings.Contains(string(ElicitationDeclined()), `"decline"`) {
		t.Error("declined answer is not a decline")
	}
	if !strings.Contains(string(ElicitationCancelled()), `"cancel"`) {
		t.Error("cancelled answer is not a cancel")
	}
}

package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// crashingFixture answers normally until it is told to die, then exits. That is
// how a real MCP server fails: not with a JSON-RPC error, but by going away.
const crashingFixture = `import json, os, sys
def send(p):
    sys.stdout.write(json.dumps(p) + "\n"); sys.stdout.flush()
marker = os.environ.get("CRASH_MARKER", "")
for line in sys.stdin:
    line = line.strip()
    if not line: continue
    m = json.loads(line); method = m.get("method")
    if method == "initialize":
        send({"jsonrpc": "2.0", "id": m["id"], "result": {"protocolVersion": "2025-11-25"}})
    elif method == "tools/list":
        send({"jsonrpc": "2.0", "id": m["id"], "result": {"tools": [
            {"name": "ping", "description": "Answer pong",
             "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False}}]}})
    elif method == "tools/call":
        if marker and os.path.exists(marker):
            os.remove(marker)
            sys.exit(1)
        send({"jsonrpc": "2.0", "id": m["id"], "result": {
            "content": [{"type": "text", "text": "pong " + str(os.getpid())}], "isError": False}})
`

// TestPooledServerIsReusedButNeverReplaysACrashedToolCall covers both halves of
// the pool contract: healthy calls reuse a process, but a connection failure
// during tools/call is surfaced rather than replayed. The following, distinct
// call reconnects on a fresh process.
func TestPooledServerIsReusedButNeverReplaysACrashedToolCall(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	root := t.TempDir()
	script := filepath.Join(root, "crashing.py")
	if err := os.WriteFile(script, []byte(crashingFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "die")
	client := NewClient(nil)
	t.Cleanup(client.Close)
	// The marker path rides in as this server's credential, which also proves
	// the child receives its own token under the name the server declares.
	server := Server{ID: "pooled", Name: "Crashing", TransportKind: TransportStdio,
		Endpoint: "python3 " + script, APIKeyEnv: "CRASH_MARKER", RequestTimeoutMS: 15000, Enabled: true}
	ctx := context.Background()

	first, err := client.CallToolStdio(ctx, server, marker, "ping", []byte(`{}`))
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := client.CallToolStdio(ctx, server, marker, "ping", []byte(`{}`))
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	// Same process both times: the pool is what makes a local server usable,
	// because relaunching it per call costs seconds.
	if string(first.Result) != string(second.Result) {
		t.Errorf("the pool relaunched the server between calls:\n  %s\n  %s", first.Result, second.Result)
	}

	// Now make the live process die mid-call. Replaying this same tools/call
	// would be unsafe: the server might have performed an effect before exiting.
	if err := os.WriteFile(marker, []byte("die"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CallToolStdio(ctx, server, marker, "ping", []byte(`{}`)); err == nil {
		t.Fatal("a tools/call whose connection died was replayed automatically")
	}
	third, err := client.CallToolStdio(ctx, server, marker, "ping", []byte(`{}`))
	if err != nil {
		t.Fatalf("the call after a crashed session did not reconnect: %v", err)
	}
	if string(third.Result) == string(first.Result) {
		t.Errorf("the later call was answered by the crashed process: %s", third.Result)
	}
}

const hangingFixture = `import json, os, sys, time
def send(p):
    sys.stdout.write(json.dumps(p) + "\n"); sys.stdout.flush()
marker = os.environ.get("HANG_MARKER", "")
for line in sys.stdin:
    m = json.loads(line); method = m.get("method")
    if method == "initialize":
        send({"jsonrpc": "2.0", "id": m["id"], "result": {"protocolVersion": "2025-11-25"}})
    elif method == "tools/call":
        if marker and os.path.exists(marker):
            os.remove(marker)
            while True: time.sleep(1)
        send({"jsonrpc": "2.0", "id": m["id"], "result": {
            "content": [{"type": "text", "text": "recovered"}], "isError": False}})
`

// TestTimedOutStdioCallKillsItsSessionAndReleasesThePool proves the deadline is
// a transport boundary, not a status checked only before a blocking read. The
// first process never writes a response; the call must still return promptly,
// discard that process and let a later call launch a usable one.
func TestTimedOutStdioCallKillsItsSessionAndReleasesThePool(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	root := t.TempDir()
	script := filepath.Join(root, "hanging.py")
	if err := os.WriteFile(script, []byte(hangingFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "hang-once")
	if err := os.WriteFile(marker, []byte("hang"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(nil)
	t.Cleanup(client.Close)
	server := Server{ID: "hanging", Name: "Hanging", TransportKind: TransportStdio,
		Endpoint: "python3 " + script, APIKeyEnv: "HANG_MARKER", RequestTimeoutMS: 15000, Enabled: true}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	started := time.Now()
	_, err := client.CallToolStdio(ctx, server, marker, "hang", []byte(`{}`))
	cancel()
	if err == nil {
		t.Fatal("a stdio server that never answered did not time out")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("deadline took %v to release a blocked stdio read", elapsed)
	}
	if got := client.liveCount(); got != 0 {
		t.Fatalf("timed-out stdio session remains live in the pool: %d", got)
	}
	if _, err := client.CallToolStdio(context.Background(), server, marker, "hang", []byte(`{}`)); err != nil {
		t.Fatalf("call after the timeout did not get a fresh session: %v", err)
	}
}

func TestOnlyCatalogListingIsAutomaticallyRetryable(t *testing.T) {
	for _, method := range []string{"tools/list", "resources/list", "prompts/list"} {
		if !retryableMCPMethod(method) {
			t.Errorf("%s should be safe to retry", method)
		}
	}
	for _, method := range []string{"tools/call", "resources/read", "prompts/get", "elicitation/create"} {
		if retryableMCPMethod(method) {
			t.Errorf("%s was allowed to replay after an uncertain connection failure", method)
		}
	}
}

// TestChangedSettingsGetANewProcess stops an edited command or a rotated token
// from being answered by the process started under the old ones.
func TestChangedSettingsGetANewProcess(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	script := filepath.Join(t.TempDir(), "server.py")
	if err := os.WriteFile(script, []byte(stdioFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(nil)
	t.Cleanup(client.Close)
	server := Server{ID: "same-id", Name: "Fixture", TransportKind: TransportStdio,
		Endpoint: "python3 " + script, RequestTimeoutMS: 15000, Enabled: true}
	ctx := context.Background()
	if _, err := client.CallToolStdio(ctx, server, "", "echo", []byte(`{"text":"a"}`)); err != nil {
		t.Fatal(err)
	}
	before := client.liveCount()
	if before != 1 {
		t.Fatalf("pool holds %d processes after one call, want 1", before)
	}
	// Same server id, different credential: the identity changed, so the old
	// process must be dropped rather than reused with the new token implied.
	if _, err := client.CallToolStdio(ctx, server, "rotated-token", "echo", []byte(`{"text":"b"}`)); err != nil {
		t.Fatal(err)
	}
	if got := client.liveCount(); got != 1 {
		t.Errorf("pool holds %d processes for one server, want 1", got)
	}
	client.CloseServer(server.ID)
	if got := client.liveCount(); got != 0 {
		t.Errorf("CloseServer left %d processes behind", got)
	}
}

// TestBrokenConnectionIsNotAServerRefusal keeps reconnect from re-running an
// effect the server deliberately refused.
func TestBrokenConnectionIsNotAServerRefusal(t *testing.T) {
	if isBrokenConnection(&wireError{rpc: &rpcError{Code: -32602, Message: "invalid params"}}) {
		t.Error("a JSON-RPC refusal was treated as a dead connection and would be retried")
	}
	for _, message := range []string{"write to server: broken pipe", "server exited without answering"} {
		if !isBrokenConnection(errString(message)) {
			t.Errorf("%q was not recognised as a dead connection", message)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// TestIdleServersAreReaped stops a machine from holding processes for sessions
// that finished hours ago.
func TestIdleServersAreReaped(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	script := filepath.Join(t.TempDir(), "server.py")
	if err := os.WriteFile(script, []byte(stdioFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(nil)
	t.Cleanup(client.Close)
	server := Server{ID: "idle", Name: "Fixture", TransportKind: TransportStdio,
		Endpoint: "python3 " + script, RequestTimeoutMS: 15000, Enabled: true}
	if _, err := client.CallToolStdio(context.Background(), server, "", "echo", []byte(`{"text":"a"}`)); err != nil {
		t.Fatal(err)
	}
	client.pool.mu.Lock()
	client.pool.live[server.ID].lastUsed = time.Now().Add(-2 * poolIdleTimeout)
	client.pool.mu.Unlock()
	client.sweepIdle()
	if got := client.liveCount(); got != 0 {
		t.Errorf("an idle server survived the sweep: %d live", got)
	}
}

func TestStdioFixtureIsShared(t *testing.T) {
	if !strings.Contains(stdioFixture, "tools/call") {
		t.Fatal("the shared fixture no longer answers tools/call")
	}
}

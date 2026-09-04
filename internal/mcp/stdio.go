package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Most MCP servers published today ship as a local program speaking JSON-RPC
// over stdin and stdout, not as an HTTP endpoint. Without this transport the
// capability graph could only reach servers somebody had already put behind a
// URL, which in practice meant it could reach almost nothing.
//
// A session is one process. Sessions are pooled per configured server because
// sampling and elicitation require a connection that outlives one request. A
// timed-out or broken session is killed and discarded; a later call may start a
// fresh process, but an effectful request is never replayed automatically.

// stdioLaunchers are the programs an MCP server may be started with. It is an
// allowlist for the same reason the project workbench has one: this is a local
// program launched from a config screen, and the set of things that legitimately
// host an MCP server is small and nameable.
var stdioLaunchers = map[string]bool{
	"npx": true, "node": true, "bun": true, "deno": true,
	"uvx": true, "uv": true, "python": true, "python3": true,
	"docker": true, "go": true,
}

// maxStdioLine bounds one JSON-RPC message. A server that writes more than this
// on one line is malfunctioning, and reading it without a ceiling would let it
// choose how much memory Hermetrix allocates.
const maxStdioLine = 8 << 20

// stdioSession is one running server process and the framing around it.
type stdioSession struct {
	command  *exec.Cmd
	cancel   context.CancelFunc
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	stopOnce sync.Once
	protocol string
	server   Server
	handler  ServerRequestHandler
}

// StdioCommand splits a server's stored command line into the launcher and its
// arguments. The command is stored as one string because that is how MCP
// servers are documented and copied ("npx -y @modelcontextprotocol/server-git"),
// and it is split here rather than passed to a shell: no globbing, no
// substitution, no operators.
func StdioCommand(commandLine string) (string, []string, error) {
	fields := strings.Fields(commandLine)
	if len(fields) == 0 {
		return "", nil, errors.New("a stdio MCP server needs a command, for example: npx -y @modelcontextprotocol/server-everything")
	}
	launcher := fields[0]
	if filepath.Base(launcher) != launcher {
		return "", nil, fmt.Errorf("the command must be a program name without a path, not %q", launcher)
	}
	if !stdioLaunchers[launcher] {
		return "", nil, fmt.Errorf("%q is not an allowed MCP launcher; allowed: %s", launcher, allowedLaunchers())
	}
	if len(fields) > 64 {
		return "", nil, errors.New("a stdio MCP command may not have more than 64 arguments")
	}
	for _, argument := range fields[1:] {
		if strings.ContainsAny(argument, "\x00\n\r") {
			return "", nil, errors.New("stdio MCP arguments may not contain control characters")
		}
	}
	return launcher, fields[1:], nil
}

func allowedLaunchers() string {
	names := make([]string, 0, len(stdioLaunchers))
	for name := range stdioLaunchers {
		names = append(names, name)
	}
	// Sorted so the error message is stable and testable.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return strings.Join(names, ", ")
}

// startStdio launches the server and completes the MCP initialize handshake.
// The caller must call stop on the returned session.
func (c *Client) startStdio(ctx context.Context, server Server, credential string) (*stdioSession, error) {
	launcher, arguments, err := StdioCommand(server.Endpoint)
	if err != nil {
		return nil, &Error{Kind: ErrorConfiguration, Operation: "stdio launch", ServerID: server.ID, Message: err.Error()}
	}
	// The process outlives the call that started it: it is pooled, and binding
	// it to this context would kill it the moment this call returned. Its
	// lifetime is the session's, ended by stop.
	processCtx, cancelProcess := context.WithCancel(context.WithoutCancel(ctx))
	command := exec.CommandContext(processCtx, launcher, arguments...)
	command.Env = stdioEnvironment(server, credential)
	command.Dir = os.TempDir()
	command.Stderr = io.Discard
	configureStdioTermination(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		cancelProcess()
		return nil, &Error{Kind: ErrorTransport, Operation: "stdio stdin", ServerID: server.ID, Message: err.Error(), Cause: err}
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancelProcess()
		return nil, &Error{Kind: ErrorTransport, Operation: "stdio stdout", ServerID: server.ID, Message: err.Error(), Cause: err}
	}
	if err := command.Start(); err != nil {
		cancelProcess()
		return nil, &Error{Kind: ErrorTransport, Operation: "stdio start", ServerID: server.ID,
			Message: launcher + ": " + err.Error(), Cause: err}
	}
	session := &stdioSession{command: command, cancel: cancelProcess, stdin: stdin,
		stdout: bufio.NewReaderSize(stdout, 64<<10), server: server, handler: c.handler}
	protocol, err := c.initializeStdio(ctx, server, session)
	if err != nil {
		session.stop()
		return nil, err
	}
	session.protocol = protocol
	return session, nil
}

// stdioEnvironment gives the server process the minimum it needs to run, plus
// its own credential. The parent environment is not inherited: it holds every
// other provider and server token this Hermetrix knows.
func stdioEnvironment(server Server, credential string) []string {
	values := []string{}
	for _, key := range []string{"PATH", "HOME", "LANG", "LC_ALL", "TMPDIR", "SystemRoot", "APPDATA", "LOCALAPPDATA"} {
		if value := os.Getenv(key); value != "" {
			values = append(values, key+"="+value)
		}
	}
	if strings.TrimSpace(credential) != "" {
		name := server.APIKeyEnv
		if name == "" {
			name = "MCP_API_KEY"
		}
		values = append(values, name+"="+credential)
	}
	return values
}

func (session *stdioSession) stop() {
	if session == nil || session.command == nil {
		return
	}
	session.stopOnce.Do(func() {
		// Closing stdin asks the server to exit cleanly; cancelling the process
		// context takes the whole group down if it does not. stop may be reached
		// both by the cancellation-aware reader and by pool discard, so the
		// whole sequence, including Wait, must run only once.
		_ = session.stdin.Close()
		if session.cancel != nil {
			session.cancel()
		}
		if session.command.Process != nil {
			_ = session.command.Process.Kill()
		}
		_ = session.command.Wait()
	})
}

// inbound is one line from the server, which can be three different things: the
// response to what we asked, a notification we ignore, or a *request* of its
// own. The third is what makes sampling and elicitation possible: a server can
// ask the client to run a model or to ask the user a question, and it waits on
// the same pipe for the answer.
type inbound struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// call writes one JSON-RPC request and pumps the stream until the matching
// response arrives, answering anything the server asks along the way.
func (session *stdioSession) call(ctx context.Context, message rpcRequest) (rpcResponse, error) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return rpcResponse{}, err
	}
	if _, err := session.stdin.Write(append(encoded, '\n')); err != nil {
		return rpcResponse{}, fmt.Errorf("write to server: %w", err)
	}
	for {
		line, err := session.readLineContext(ctx)
		if err != nil {
			return rpcResponse{}, err
		}
		var frame inbound
		if err := json.Unmarshal(line, &frame); err != nil {
			continue
		}
		// A method plus an id is the server asking us something. A method with
		// no id is a notification, which needs no answer.
		if frame.Method != "" {
			if len(frame.ID) > 0 {
				session.answerServerRequest(ctx, frame)
			}
			continue
		}
		if message.ID == 0 || matchingID(frame.ID, strconv.FormatInt(message.ID, 10)) {
			response := rpcResponse{JSONRPC: frame.JSONRPC, ID: frame.ID, Result: frame.Result, Error: frame.Error}
			if frame.Error != nil {
				return response, &wireError{rpc: frame.Error}
			}
			return response, nil
		}
	}
}

type stdioReadResult struct {
	line []byte
	err  error
}

// readLineContext makes the request context a real boundary for stdio. A
// bufio.Reader cannot be interrupted by checking ctx before ReadBytes: once a
// server stops writing, that check is never reached again. The read therefore
// runs beside the context wait. Cancellation kills the session, which closes
// the pipe, releases the reader goroutine and prevents a late response from a
// timed-out effect being mistaken for a later request.
func (session *stdioSession) readLineContext(ctx context.Context) ([]byte, error) {
	result := make(chan stdioReadResult, 1)
	go func() {
		line, err := session.readLine()
		result <- stdioReadResult{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		session.stop()
		return nil, ctx.Err()
	case read := <-result:
		return read.line, read.err
	}
}

// answerServerRequest handles one server-to-client request and writes the reply
// back on the same pipe. A failure here is answered as a JSON-RPC error rather
// than dropped: a server left waiting forever on an unanswered request is a
// hung tool call with no explanation.
func (session *stdioSession) answerServerRequest(ctx context.Context, frame inbound) {
	reply := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(frame.ID)}
	result, err := session.dispatchServerRequest(ctx, frame)
	if err != nil {
		code := -32603
		var refusal *RequestRefused
		if errors.As(err, &refusal) {
			code = refusal.Code
		}
		reply["error"] = map[string]any{"code": code, "message": err.Error()}
	} else {
		reply["result"] = result
	}
	encoded, marshalErr := json.Marshal(reply)
	if marshalErr != nil {
		return
	}
	_, _ = session.stdin.Write(append(encoded, '\n'))
}

func (session *stdioSession) dispatchServerRequest(ctx context.Context, frame inbound) (json.RawMessage, error) {
	if session.handler == nil {
		return nil, &RequestRefused{Code: -32601, Message: "this client does not accept " + frame.Method}
	}
	switch frame.Method {
	case "sampling/createMessage":
		return session.handler.Sample(ctx, session.server, frame.Params)
	case "elicitation/create":
		return session.handler.Elicit(ctx, session.server, frame.Params)
	case "ping":
		return json.RawMessage(`{}`), nil
	default:
		return nil, &RequestRefused{Code: -32601, Message: "unsupported server request " + frame.Method}
	}
}

func (session *stdioSession) notify(message rpcRequest) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = session.stdin.Write(append(encoded, '\n'))
	return err
}

func (session *stdioSession) readLine() ([]byte, error) {
	line, err := session.stdout.ReadBytes('\n')
	if len(line) > maxStdioLine {
		return nil, fmt.Errorf("server wrote a %d byte message, over the %d byte ceiling", len(line), maxStdioLine)
	}
	if err != nil {
		if errors.Is(err, io.EOF) && len(strings.TrimSpace(string(line))) > 0 {
			return line, nil
		}
		if errors.Is(err, io.EOF) {
			return nil, errors.New("server exited without answering")
		}
		return nil, err
	}
	return line, nil
}

func (c *Client) initializeStdio(ctx context.Context, server Server, session *stdioSession) (string, error) {
	response, err := session.call(ctx, rpcRequest{JSONRPC: "2.0", ID: c.nextID.Add(1), Method: "initialize", Params: map[string]any{
		"protocolVersion": ProtocolLegacy,
		"capabilities":    c.clientCapabilities(),
		"clientInfo":      map[string]any{"name": "hermetrix", "version": "1"},
	}})
	if err != nil {
		return "", &Error{Kind: ErrorProtocol, Operation: "initialize", ServerID: server.ID, Message: err.Error(), Cause: err}
	}
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(response.Result, &result)
	if err := session.notify(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}); err != nil {
		return "", &Error{Kind: ErrorTransport, Operation: "initialized", ServerID: server.ID, Message: err.Error(), Cause: err}
	}
	protocol := result.ProtocolVersion
	if protocol == "" {
		protocol = ProtocolLegacy
	}
	return protocol, nil
}

// ListToolsStdio discovers one stdio server's catalog through the pooled
// process, following pagination the same way the HTTP transport does.
func (c *Client) ListToolsStdio(ctx context.Context, server Server, credential string) ([]RemoteTool, string, error) {
	ctx, cancel := context.WithTimeout(ctx, stdioTimeout(server))
	defer cancel()
	items, err := c.listPages(ctx, func(cursor string) (listToolsResult, error) {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.pooledCall(ctx, server, credential, "tools/list", params)
		if err != nil {
			return listToolsResult{}, err
		}
		return decodeToolList(raw)
	})
	if err != nil {
		return nil, "", classify("tools/list", server.ID, err)
	}
	return items, c.negotiatedProtocol(server), nil
}

// CallToolStdio runs one tool on a stdio server.
func (c *Client) CallToolStdio(ctx context.Context, server Server, credential, name string,
	arguments json.RawMessage) (callResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, stdioTimeout(server))
	defer cancel()
	raw, err := c.pooledCall(ctx, server, credential, "tools/call",
		map[string]any{"name": name, "arguments": json.RawMessage(arguments)})
	if err != nil {
		return callResponse{}, classify("tools/call", server.ID, err)
	}
	return validateCallResult(c.negotiatedProtocol(server), raw)
}

// negotiatedProtocol reports what the live process agreed to at initialize.
func (c *Client) negotiatedProtocol(server Server) string {
	c.pool.mu.Lock()
	entry, ok := c.pool.live[server.ID]
	c.pool.mu.Unlock()
	if !ok {
		return ProtocolLegacy
	}
	if !entry.mu.TryLock() {
		return ProtocolLegacy
	}
	defer entry.mu.Unlock()
	if entry.session == nil || entry.session.protocol == "" {
		return ProtocolLegacy
	}
	return entry.session.protocol
}

// stdioTimeout adds the process launch to the configured request budget: `npx`
// resolving a package on first run is slower than any single RPC, and charging
// that to the request timeout made every first discovery look like a failure.
func stdioTimeout(server Server) time.Duration {
	budget := time.Duration(server.RequestTimeoutMS) * time.Millisecond
	if budget <= 0 {
		budget = 15 * time.Second
	}
	return budget + 45*time.Second
}

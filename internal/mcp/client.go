package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

const (
	maxResponseBytes = 8 << 20
	maxToolCount     = 10_000
	maxListPages     = 100
)

type Client struct {
	httpClient *http.Client
	nextID     atomic.Int64
	pool       *sessionPool
	handler    ServerRequestHandler
}

func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	}
	return &Client{httpClient: client, pool: newSessionPool()}
}

type rpcRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int64          `json:"id,omitempty"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type wireResult struct {
	response rpcResponse
	headers  http.Header
}

type wireError struct {
	status int
	rpc    *rpcError
	body   string
}

func (e *wireError) Error() string {
	if e.rpc != nil {
		return fmt.Sprintf("HTTP %d JSON-RPC %d: %s", e.status, e.rpc.Code, e.rpc.Message)
	}
	if e.body != "" {
		return fmt.Sprintf("HTTP %d: %s", e.status, e.body)
	}
	return fmt.Sprintf("HTTP %d", e.status)
}

type legacySession struct {
	protocol  string
	sessionID string
}

func (c *Client) ListTools(ctx context.Context, server Server, credential string) ([]RemoteTool, string, error) {
	switch server.ProtocolMode {
	case ProtocolCurrent:
		items, err := c.listCurrent(ctx, server, credential)
		return items, ProtocolCurrent, classify("tools/list", server.ID, err)
	case ProtocolLegacy:
		items, protocol, err := c.listLegacy(ctx, server, credential)
		return items, protocol, classify("tools/list", server.ID, err)
	default:
		items, err := c.listCurrent(ctx, server, credential)
		if err == nil {
			return items, ProtocolCurrent, nil
		}
		if !shouldFallbackToLegacy(err) {
			return nil, "", classify("tools/list", server.ID, err)
		}
		items, protocol, legacyErr := c.listLegacy(ctx, server, credential)
		return items, protocol, classify("tools/list", server.ID, legacyErr)
	}
}

func (c *Client) CallTool(ctx context.Context, server Server, credential, protocol, name string,
	arguments json.RawMessage, inputSchema json.RawMessage) (callResponse, error) {
	if protocol == ProtocolCurrent {
		headers, err := customToolHeaders(name, arguments, inputSchema)
		if err != nil {
			return callResponse{}, &Error{Kind: ErrorProtocol, Operation: "tools/call headers", ServerID: server.ID, Message: err.Error(), Cause: err}
		}
		params := map[string]any{"name": name, "arguments": json.RawMessage(arguments)}
		result, err := c.currentRequest(ctx, server, credential, "tools/call", name, params, headers)
		if err != nil {
			return callResponse{}, classify("tools/call", server.ID, err)
		}
		return validateCallResult(ProtocolCurrent, result.response.Result)
	}
	if protocol == "" {
		protocol = ProtocolLegacy
	}
	session, err := c.initializeLegacy(ctx, server, credential)
	if err != nil {
		return callResponse{}, classify("initialize", server.ID, err)
	}
	defer c.closeLegacy(ctx, server, credential, session)
	result, err := c.legacyRequest(ctx, server, credential, session, "tools/call", map[string]any{
		"name": name, "arguments": json.RawMessage(arguments),
	})
	if err != nil {
		return callResponse{}, classify("tools/call", server.ID, err)
	}
	return validateCallResult(session.protocol, result.response.Result)
}

func validateCallResult(protocol string, raw json.RawMessage) (callResponse, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return callResponse{}, &Error{Kind: ErrorProtocol, Operation: "tools/call", Message: "server returned an invalid tool result"}
	}
	var marker struct {
		ResultType string `json:"resultType"`
		IsError    bool   `json:"isError"`
	}
	if err := json.Unmarshal(raw, &marker); err != nil {
		return callResponse{}, &Error{Kind: ErrorProtocol, Operation: "tools/call", Message: "decode tool result", Cause: err}
	}
	if marker.ResultType == "input_required" {
		return callResponse{}, &Error{Kind: ErrorProtocol, Operation: "tools/call", Message: "server requested multi-round-trip input, which is not enabled in this phase"}
	}
	if marker.IsError {
		return callResponse{}, &Error{Kind: ErrorRemote, Operation: "tools/call", Message: compactRaw(raw)}
	}
	return callResponse{Protocol: protocol, Result: append(json.RawMessage(nil), raw...)}, nil
}

func (c *Client) listCurrent(ctx context.Context, server Server, credential string) ([]RemoteTool, error) {
	return c.listPages(ctx, func(cursor string) (listToolsResult, error) {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		result, err := c.currentRequest(ctx, server, credential, "tools/list", "", params, nil)
		if err != nil {
			return listToolsResult{}, err
		}
		return decodeToolList(result.response.Result)
	})
}

func (c *Client) listLegacy(ctx context.Context, server Server, credential string) ([]RemoteTool, string, error) {
	session, err := c.initializeLegacy(ctx, server, credential)
	if err != nil {
		return nil, "", err
	}
	defer c.closeLegacy(ctx, server, credential, session)
	items, err := c.listPages(ctx, func(cursor string) (listToolsResult, error) {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		result, err := c.legacyRequest(ctx, server, credential, session, "tools/list", params)
		if err != nil {
			return listToolsResult{}, err
		}
		return decodeToolList(result.response.Result)
	})
	return items, session.protocol, err
}

type listToolsResult struct {
	ResultType string       `json:"resultType,omitempty"`
	Tools      []RemoteTool `json:"tools"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

func decodeToolList(raw json.RawMessage) (listToolsResult, error) {
	var result listToolsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, &Error{Kind: ErrorProtocol, Message: "decode tools/list result", Cause: err}
	}
	if result.ResultType != "" && result.ResultType != "complete" {
		return result, &Error{Kind: ErrorProtocol, Message: "tools/list did not return a complete result"}
	}
	return result, nil
}

func (c *Client) listPages(ctx context.Context, page func(string) (listToolsResult, error)) ([]RemoteTool, error) {
	items := make([]RemoteTool, 0)
	seenNames := map[string]bool{}
	seenCursors := map[string]bool{}
	cursor := ""
	for pageNumber := 0; pageNumber < maxListPages; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := page(cursor)
		if err != nil {
			return nil, err
		}
		for _, tool := range result.Tools {
			if seenNames[tool.Name] {
				return nil, &Error{Kind: ErrorProtocol, Message: "tools/list returned duplicate tool name " + tool.Name}
			}
			seenNames[tool.Name] = true
			items = append(items, tool)
			if len(items) > maxToolCount {
				return nil, &Error{Kind: ErrorProtocol, Message: "tools/list exceeds 10,000 tool safety limit"}
			}
		}
		if result.NextCursor == "" {
			return items, nil
		}
		if seenCursors[result.NextCursor] {
			return nil, &Error{Kind: ErrorProtocol, Message: "tools/list pagination cursor loop"}
		}
		seenCursors[result.NextCursor] = true
		cursor = result.NextCursor
	}
	return nil, &Error{Kind: ErrorProtocol, Message: "tools/list exceeds 100 page safety limit"}
}

func (c *Client) currentRequest(ctx context.Context, server Server, credential, method, name string,
	params map[string]any, extraHeaders http.Header) (wireResult, error) {
	params = cloneParams(params)
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    ProtocolCurrent,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "Hermetrix Harness", "version": "0.2.0"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	request := rpcRequest{JSONRPC: "2.0", ID: c.nextID.Add(1), Method: method, Params: params}
	headers := cloneHeader(extraHeaders)
	headers.Set("MCP-Protocol-Version", ProtocolCurrent)
	headers.Set("Mcp-Method", method)
	if name != "" {
		headers.Set("Mcp-Name", encodeHeaderValue(name))
	}
	return c.doRPC(ctx, server.Endpoint, credential, request, headers)
}

func (c *Client) initializeLegacy(ctx context.Context, server Server, credential string) (legacySession, error) {
	request := rpcRequest{JSONRPC: "2.0", ID: c.nextID.Add(1), Method: "initialize", Params: map[string]any{
		"protocolVersion": ProtocolLegacy,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "Hermetrix Harness", "version": "0.2.0"},
	}}
	result, err := c.doRPC(ctx, server.Endpoint, credential, request, nil)
	if err != nil {
		return legacySession{}, err
	}
	var initialized struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(result.response.Result, &initialized); err != nil {
		return legacySession{}, &Error{Kind: ErrorProtocol, Message: "decode initialize result", Cause: err}
	}
	if !supportedLegacyProtocol(initialized.ProtocolVersion) {
		return legacySession{}, &Error{Kind: ErrorProtocol, Message: "unsupported negotiated protocol " + initialized.ProtocolVersion}
	}
	session := legacySession{protocol: initialized.ProtocolVersion, sessionID: result.headers.Get("Mcp-Session-Id")}
	notification := rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}
	headers := http.Header{}
	headers.Set("MCP-Protocol-Version", session.protocol)
	if session.sessionID != "" {
		headers.Set("Mcp-Session-Id", session.sessionID)
	}
	if err := c.doNotification(ctx, server.Endpoint, credential, notification, headers); err != nil {
		return legacySession{}, err
	}
	return session, nil
}

func (c *Client) legacyRequest(ctx context.Context, server Server, credential string, session legacySession,
	method string, params map[string]any) (wireResult, error) {
	request := rpcRequest{JSONRPC: "2.0", ID: c.nextID.Add(1), Method: method, Params: params}
	headers := http.Header{}
	headers.Set("MCP-Protocol-Version", session.protocol)
	if session.sessionID != "" {
		headers.Set("Mcp-Session-Id", session.sessionID)
	}
	return c.doRPC(ctx, server.Endpoint, credential, request, headers)
}

func (c *Client) closeLegacy(ctx context.Context, server Server, credential string, session legacySession) {
	if session.sessionID == "" || ctx.Err() != nil {
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, server.Endpoint, nil)
	if err != nil {
		return
	}
	request.Header.Set("Mcp-Session-Id", session.sessionID)
	request.Header.Set("MCP-Protocol-Version", session.protocol)
	setCredential(request.Header, credential)
	response, err := c.httpClient.Do(request)
	if err == nil {
		response.Body.Close()
	}
}

func (c *Client) doRPC(ctx context.Context, endpoint, credential string, message rpcRequest, headers http.Header) (wireResult, error) {
	body, err := json.Marshal(message)
	if err != nil {
		return wireResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return wireResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	setCredential(request.Header, credential)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return wireResult{}, err
	}
	defer response.Body.Close()
	rpc, raw, parseErr := readRPCResponse(response, strconv.FormatInt(message.ID, 10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if parseErr == nil {
			return wireResult{}, &wireError{status: response.StatusCode, rpc: rpc.Error, body: compactRaw(raw)}
		}
		return wireResult{}, &wireError{status: response.StatusCode, body: compactRaw(raw)}
	}
	if parseErr != nil {
		return wireResult{}, parseErr
	}
	if rpc.Error != nil {
		return wireResult{}, &Error{Kind: ErrorRemote, HTTPStatus: response.StatusCode, RPCCode: rpc.Error.Code,
			Message: rpc.Error.Message}
	}
	if len(rpc.Result) == 0 {
		return wireResult{}, &Error{Kind: ErrorProtocol, Message: "JSON-RPC response has neither result nor error"}
	}
	return wireResult{response: rpc, headers: response.Header.Clone()}, nil
}

func (c *Client) doNotification(ctx context.Context, endpoint, credential string, message rpcRequest, headers http.Header) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	setCredential(request.Header, credential)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4097))
		return &wireError{status: response.StatusCode, body: compactRaw(raw)}
	}
	return nil
}

func readRPCResponse(response *http.Response, expectedID string) (rpcResponse, []byte, error) {
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		return rpcResponse{}, nil, &Error{Kind: ErrorProtocol, Message: "invalid MCP response content type", Cause: err}
	}
	switch mediaType {
	case "application/json":
		raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
		if err != nil {
			return rpcResponse{}, raw, err
		}
		if len(raw) > maxResponseBytes {
			return rpcResponse{}, raw, &Error{Kind: ErrorProtocol, Message: "MCP response exceeds 8 MiB"}
		}
		var rpc rpcResponse
		if err := json.Unmarshal(raw, &rpc); err != nil {
			return rpc, raw, &Error{Kind: ErrorProtocol, Message: "invalid JSON-RPC response", Cause: err}
		}
		if !matchingID(rpc.ID, expectedID) {
			return rpc, raw, &Error{Kind: ErrorProtocol, Message: "JSON-RPC response ID mismatch"}
		}
		return rpc, raw, nil
	case "text/event-stream":
		return readSSEResponse(response.Body, expectedID)
	default:
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4097))
		return rpcResponse{}, raw, &Error{Kind: ErrorProtocol, Message: "unsupported MCP response content type " + mediaType}
	}
}

func readSSEResponse(reader io.Reader, expectedID string) (rpcResponse, []byte, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, maxResponseBytes+1))
	scanner.Buffer(make([]byte, 64<<10), maxResponseBytes+1)
	dataLines := []string{}
	total := 0
	var final rpcResponse
	var finalRaw []byte
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		raw := []byte(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		var message rpcResponse
		if err := json.Unmarshal(raw, &message); err != nil {
			return &Error{Kind: ErrorProtocol, Message: "invalid JSON-RPC message in SSE response", Cause: err}
		}
		if len(message.ID) > 0 {
			if !matchingID(message.ID, expectedID) {
				return &Error{Kind: ErrorProtocol, Message: "JSON-RPC response ID mismatch in SSE stream"}
			}
			final, finalRaw = message, append([]byte(nil), raw...)
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		total += len(line) + 1
		if total > maxResponseBytes {
			return rpcResponse{}, finalRaw, &Error{Kind: ErrorProtocol, Message: "MCP SSE response exceeds 8 MiB"}
		}
		if line == "" {
			if err := flush(); err != nil {
				return rpcResponse{}, finalRaw, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return rpcResponse{}, finalRaw, err
	}
	if err := flush(); err != nil {
		return rpcResponse{}, finalRaw, err
	}
	if len(final.ID) == 0 {
		return rpcResponse{}, finalRaw, &Error{Kind: ErrorProtocol, Message: "SSE stream ended without a final JSON-RPC response"}
	}
	return final, finalRaw, nil
}

func shouldFallbackToLegacy(err error) bool {
	var wire *wireError
	if !errors.As(err, &wire) || wire.status != http.StatusBadRequest {
		return false
	}
	if wire.rpc == nil {
		return true
	}
	var data map[string]any
	if json.Unmarshal(wire.rpc.Data, &data) == nil {
		if _, advertised := data["supported"]; advertised {
			return false
		}
	}
	message := strings.ToLower(wire.rpc.Message)
	return strings.Contains(message, "initialize") || strings.Contains(message, "not initialized")
}

func supportedLegacyProtocol(value string) bool {
	switch value {
	case "2025-11-25", "2025-06-18", "2025-03-26":
		return true
	default:
		return false
	}
}

func setCredential(headers http.Header, credential string) {
	if credential != "" {
		headers.Set("Authorization", "Bearer "+credential)
	}
}

func cloneParams(input map[string]any) map[string]any {
	output := make(map[string]any, len(input)+1)
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneHeader(input http.Header) http.Header {
	if input == nil {
		return http.Header{}
	}
	return input.Clone()
}

func matchingID(raw json.RawMessage, expected string) bool {
	return strings.TrimSpace(string(raw)) == expected || strings.Trim(strings.TrimSpace(string(raw)), `"`) == expected
}

func compactRaw(raw []byte) string {
	value := strings.TrimSpace(string(raw))
	if len(value) > 1000 {
		value = value[:800] + "…"
	}
	return value
}

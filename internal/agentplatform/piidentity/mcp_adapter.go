package piidentity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"hermetrix-harness/internal/mcp"
	"hermetrix-harness/internal/secrets"
)

const ServerID = "pi-h2a-readonly"

var allowed = map[string]bool{
	"whoami": true, "get_node": true, "get_agent": true, "list_capabilities": true,
}

type MCPCaller struct {
	client   *mcp.Client
	server   mcp.Server
	token    string
	protocol string
	schemas  map[string]json.RawMessage
}

// NewMCPCaller reads only the existing Hermetrix vault. An environment variable
// is neither configured nor consulted. The profile remains local and in memory.
func NewMCPCaller(dataRoot, endpoint string) (*MCPCaller, error) {
	if err := validateEndpoint(endpoint); err != nil {
		return nil, err
	}
	vault, err := secrets.Open(dataRoot)
	if err != nil {
		return nil, errors.New("cannot open Hermetrix credential vault")
	}
	token, ok := vault.Get(mcp.CredentialRef(ServerID))
	if !ok {
		return nil, errors.New("H2A credential missing from Hermetrix vault at mcp:pi-h2a-readonly")
	}
	if len(token) < 32 || strings.ContainsAny(token, "\r\n \t") {
		return nil, errors.New("H2A credential format rejected")
	}
	return &MCPCaller{client: mcp.NewClient(nil), token: token,
		server: mcp.Server{ID: ServerID, Name: "Pi H2A read-only", TransportKind: mcp.TransportStreamableHTTP,
			Endpoint: endpoint, ProtocolMode: mcp.ProtocolAuto, APIKeyEnv: "", RequestTimeoutMS: 10000},
		schemas: map[string]json.RawMessage{}}, nil
}

func (c *MCPCaller) Close() { c.client.Close() }

func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u == nil || u.Host == "" || u.Path != "/mcp" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("H2A endpoint must be an HTTPS or loopback /mcp URL without credentials or query")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
	}
	return errors.New("H2A refuses remote plaintext HTTP")
}

func (c *MCPCaller) Call(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error) {
	if !allowed[name] {
		return nil, errors.New("H2A tool is not on fixed read-only allowlist")
	}
	if c.protocol == "" {
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		tools, protocol, err := c.client.ListTools(callCtx, c.server, c.token)
		if err != nil {
			return nil, safeMCPError(err)
		}
		for _, tool := range tools {
			if allowed[tool.Name] {
				c.schemas[tool.Name] = tool.InputSchema
			}
		}
		for required := range allowed {
			if len(c.schemas[required]) == 0 {
				return nil, fmt.Errorf("Pi MCP missing required read tool %s", required)
			}
		}
		c.protocol = protocol
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	response, err := c.client.CallTool(callCtx, c.server, c.token, c.protocol, name, arguments, c.schemas[name])
	if err != nil {
		c.protocol = "" // reconnect must rediscover; the probe then starts at whoami
		return nil, safeMCPError(err)
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil || result.IsError || len(result.Content) != 1 || result.Content[0].Type != "text" || !json.Valid([]byte(result.Content[0].Text)) {
		return nil, errors.New("Pi MCP returned an invalid or rejected read result")
	}
	return json.RawMessage(result.Content[0].Text), nil
}

func safeMCPError(err error) error {
	var typed *mcp.Error
	if errors.As(err, &typed) {
		if typed.Kind == mcp.ErrorTimeout {
			return fmt.Errorf("Pi MCP timed out: %w", ErrTransient)
		}
		if typed.Kind == mcp.ErrorTransport && !strings.Contains(typed.Message, "HTTP ") {
			return fmt.Errorf("Pi MCP network unavailable: %w", ErrTransient)
		}
		return errors.New("Pi MCP request denied or protocol failed")
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return fmt.Errorf("Pi MCP network unavailable: %w", ErrTransient)
	}
	return errors.New("Pi MCP request failed")
}

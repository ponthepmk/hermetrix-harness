package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// An MCP server publishes three kinds of thing. Tools are actions it can run;
// resources are data it can hand over; prompts are templates it wants used
// verbatim. Discovery indexed only the first, so a server whose entire purpose
// was the data behind it looked empty after a successful discovery.
//
// Resources and prompts reach the model through the same deferred catalog the
// tools use, so adding them costs no direct prompt tokens and does not widen
// the tool waist: they are found with tool_search, inspected with
// tool_describe, and fetched with tool_call.

const (
	// KindTool, KindResource and KindPrompt are carried in a catalog entry's
	// metadata so the executor knows which MCP method answers it.
	KindTool     = "tool"
	KindResource = "resource"
	KindPrompt   = "prompt"

	maxCatalogEntries = 2000
)

// RemoteResource is one entry of a server's resources/list answer.
type RemoteResource struct {
	URI         string          `json:"uri"`
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	MimeType    string          `json:"mimeType"`
	Annotations json.RawMessage `json:"annotations"`
}

// RemotePrompt is one entry of a server's prompts/list answer.
type RemotePrompt struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Arguments   json.RawMessage `json:"arguments"`
}

type listResourcesResult struct {
	Resources  []RemoteResource `json:"resources"`
	NextCursor string           `json:"nextCursor"`
}

type listPromptsResult struct {
	Prompts    []RemotePrompt `json:"prompts"`
	NextCursor string         `json:"nextCursor"`
}

// catalogRPC is the one shape both transports satisfy: send a method with
// params, get a result back. It lets discovery walk resources and prompts
// without a second copy of the pagination loop per transport.
type catalogRPC func(ctx context.Context, method string, params map[string]any) (json.RawMessage, error)

// rpcFor returns the caller for one server. A stdio server holds its process
// open across the whole discovery rather than launching one per page.
func (c *Client) rpcFor(ctx context.Context, server Server, credential string) (catalogRPC, func(), error) {
	if server.TransportKind == TransportStdio {
		// Pooled: the process is shared with every other call to this server
		// and outlives this one, and a dead pipe reconnects instead of failing.
		call := func(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
			return c.pooledCall(ctx, server, credential, method, params)
		}
		return call, func() {}, nil
	}
	call := func(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
		result, err := c.currentRequest(ctx, server, credential, method, "", params, nil)
		if err != nil {
			return nil, err
		}
		return result.response.Result, nil
	}
	return call, func() {}, nil
}

// ListResources walks a server's resources. A server that does not implement
// resources answers "method not found", which is not a failure: it means this
// server has none, and discovery must still succeed for its tools.
func (c *Client) ListResources(ctx context.Context, server Server, credential string) ([]RemoteResource, error) {
	call, stop, err := c.rpcFor(ctx, server, credential)
	if err != nil {
		return nil, err
	}
	defer stop()
	items := []RemoteResource{}
	cursor := ""
	for page := 0; page < 50; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := call(ctx, "resources/list", params)
		if err != nil {
			if isUnsupportedMethod(err) {
				return nil, nil
			}
			return nil, err
		}
		var result listResourcesResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("decode resources/list: %w", err)
		}
		items = append(items, result.Resources...)
		if len(items) > maxCatalogEntries {
			return items[:maxCatalogEntries], nil
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	return items, nil
}

// ListPrompts walks a server's prompts, with the same "not implemented is not
// an error" rule as resources.
func (c *Client) ListPrompts(ctx context.Context, server Server, credential string) ([]RemotePrompt, error) {
	call, stop, err := c.rpcFor(ctx, server, credential)
	if err != nil {
		return nil, err
	}
	defer stop()
	items := []RemotePrompt{}
	cursor := ""
	for page := 0; page < 50; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := call(ctx, "prompts/list", params)
		if err != nil {
			if isUnsupportedMethod(err) {
				return nil, nil
			}
			return nil, err
		}
		var result listPromptsResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("decode prompts/list: %w", err)
		}
		items = append(items, result.Prompts...)
		if len(items) > maxCatalogEntries {
			return items[:maxCatalogEntries], nil
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	return items, nil
}

// ReadResource fetches one resource's contents.
func (c *Client) ReadResource(ctx context.Context, server Server, credential, uri string) (json.RawMessage, error) {
	callCtx, cancel := context.WithTimeout(ctx, catalogTimeout(server))
	defer cancel()
	call, stop, err := c.rpcFor(callCtx, server, credential)
	if err != nil {
		return nil, err
	}
	defer stop()
	raw, err := call(callCtx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return nil, classify("resources/read", server.ID, err)
	}
	return raw, nil
}

// GetPrompt renders one prompt template with the arguments the model supplied.
func (c *Client) GetPrompt(ctx context.Context, server Server, credential, name string,
	arguments map[string]any) (json.RawMessage, error) {
	callCtx, cancel := context.WithTimeout(ctx, catalogTimeout(server))
	defer cancel()
	call, stop, err := c.rpcFor(callCtx, server, credential)
	if err != nil {
		return nil, err
	}
	defer stop()
	params := map[string]any{"name": name}
	if len(arguments) > 0 {
		params["arguments"] = arguments
	}
	raw, err := call(callCtx, "prompts/get", params)
	if err != nil {
		return nil, classify("prompts/get", server.ID, err)
	}
	return raw, nil
}

// isUnsupportedMethod distinguishes "this server does not do resources" from a
// real failure. JSON-RPC reserves -32601 for an unknown method, and some
// servers answer with a plain message instead.
func isUnsupportedMethod(err error) bool {
	if err == nil {
		return false
	}
	var wire *wireError
	if errors.As(err, &wire) && wire.rpc != nil && wire.rpc.Code == -32601 {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "method not found") || strings.Contains(text, "not supported") ||
		strings.Contains(text, "unknown method") || strings.Contains(text, "-32601")
}

func catalogTimeout(server Server) time.Duration {
	if server.TransportKind == TransportStdio {
		return stdioTimeout(server)
	}
	budget := time.Duration(server.RequestTimeoutMS) * time.Millisecond
	if budget <= 0 {
		budget = 15 * time.Second
	}
	return budget
}

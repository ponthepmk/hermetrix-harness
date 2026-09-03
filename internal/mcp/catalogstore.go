package mcp

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"hermetrix-harness/internal/capabilities"
)

// resourceSchema is the argument shape for reading one resource. The URI is
// fixed at discovery, so the model supplies nothing: naming the capability is
// the whole request, and an editable URI would be a way to point a trusted
// server's reader at somewhere it was never meant to go.
var resourceSchema = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)

// contentRevision fingerprints one catalog entry. A revision changes when the
// content the model was shown changes, which is what lets a call be refused
// after a server quietly rewrote the thing it described.
func contentRevision(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

// dbExecer is the subset of *sql.Tx that replaceCatalogKinds needs, so it can
// be called inside the discovery transaction without importing the whole type.
type dbExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// storedResource and storedPrompt are the validated forms written by discovery.
type storedResource struct {
	URI, Name, Title, Description, MimeType, Revision string
	Annotations                                       json.RawMessage
	RequiresApproval                                  bool
}

type storedPrompt struct {
	Name, Title, Description, Revision string
	Arguments                          json.RawMessage
	RequiresApproval                   bool
}

// prepareResource validates one resources/list entry. Everything a remote
// server sends is untrusted input, so it is bounded here rather than at render
// time, which is the same rule prepareTool follows for a tool.
func prepareResource(server Server, remote RemoteResource) (storedResource, error) {
	uri := strings.TrimSpace(remote.URI)
	if uri == "" || len(uri) > 2048 || !utf8.ValidString(uri) {
		return storedResource{}, fmt.Errorf("invalid MCP resource URI")
	}
	annotations := remote.Annotations
	if len(annotations) == 0 || !json.Valid(annotations) {
		annotations = json.RawMessage("{}")
	}
	item := storedResource{
		URI: uri, Name: boundedText(remote.Name, 256), Title: boundedText(remote.Title, 256),
		Description: boundedText(remote.Description, 4096), MimeType: boundedText(remote.MimeType, 128),
		Annotations: annotations,
		// Reading a resource still crosses to a remote server and returns
		// content the model will act on, so it is gated exactly like a tool:
		// fail closed unless the user has said they trust this server's own
		// risk annotations.
		RequiresApproval: !server.TrustAnnotations,
	}
	item.Revision = contentRevision("resource", server.ID, item.URI, item.Name, item.Title, item.Description, item.MimeType, string(annotations))
	return item, nil
}

func preparePrompt(server Server, remote RemotePrompt) (storedPrompt, error) {
	name := strings.TrimSpace(remote.Name)
	if name == "" || len(name) > 256 || !utf8.ValidString(name) {
		return storedPrompt{}, fmt.Errorf("invalid MCP prompt name")
	}
	arguments := remote.Arguments
	if len(arguments) == 0 || len(arguments) > maxSchemaBytes || !json.Valid(arguments) {
		arguments = json.RawMessage("[]")
	}
	item := storedPrompt{
		Name: name, Title: boundedText(remote.Title, 256), Description: boundedText(remote.Description, 4096),
		Arguments: arguments, RequiresApproval: !server.TrustAnnotations,
	}
	item.Revision = contentRevision("prompt", server.ID, item.Name, item.Title, item.Description, string(arguments))
	return item, nil
}

// replaceCatalogKinds rewrites one server's resources and prompts inside the
// discovery transaction, so a server's three catalogs are always one snapshot.
func replaceCatalogKinds(ctx context.Context, tx dbExecer, serverID string, resources []storedResource,
	prompts []storedPrompt, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_resources WHERE server_id=?`, serverID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_prompts WHERE server_id=?`, serverID); err != nil {
		return err
	}
	for _, item := range resources {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_resources
      (server_id,uri,name,title,description,mime_type,annotations_json,revision,requires_approval,discovered_at)
      VALUES(?,?,?,?,?,?,?,?,?,?)`, serverID, item.URI, item.Name, item.Title, item.Description, item.MimeType,
			string(item.Annotations), item.Revision, boolInt(item.RequiresApproval), formatTime(now)); err != nil {
			return err
		}
	}
	for _, item := range prompts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_prompts
      (server_id,name,title,description,arguments_json,revision,requires_approval,discovered_at)
      VALUES(?,?,?,?,?,?,?,?)`, serverID, item.Name, item.Title, item.Description, string(item.Arguments),
			item.Revision, boolInt(item.RequiresApproval), formatTime(now)); err != nil {
			return err
		}
	}
	return nil
}

// nonToolEntries turns the stored resources and prompts into catalog entries.
// The capability name is prefixed so a resource and a tool can never collide on
// one id, and so tool_describe can say which kind it is describing.
func (s *Service) nonToolEntries(ctx context.Context, server Server, readiness string) ([]capabilities.Entry, error) {
	entries := []capabilities.Entry{}
	resources, err := s.store.DB.QueryContext(ctx, `SELECT uri,name,title,description,mime_type,annotations_json,
    revision,requires_approval FROM mcp_resources WHERE server_id=? ORDER BY uri`, server.ID)
	if err != nil {
		return nil, err
	}
	defer resources.Close()
	for resources.Next() {
		var uri, name, title, description, mimeType, annotations, revision string
		var approval int
		if err := resources.Scan(&uri, &name, &title, &description, &mimeType, &annotations, &revision, &approval); err != nil {
			return nil, err
		}
		label := firstNonEmpty(title, name, uri)
		entries = append(entries, capabilities.Entry{
			ID: capabilityID(server.ID, resourceCapabilityName(uri)), Name: resourceCapabilityName(uri), Title: label,
			Description: firstNonEmpty(description, "Resource published by "+server.Name) + " (" + uri + ")",
			Source:      capabilities.SourceMCP, SourceRef: server.ID, Revision: revision, Effect: "read",
			Readiness: readiness, RequiresApproval: approval != 0, InputSchema: resourceSchema,
			Annotations: json.RawMessage(annotations),
			Metadata: map[string]any{"server_name": server.Name, "protocol": server.LastProtocol,
				"annotations_trusted": server.TrustAnnotations, "untrusted_output": true,
				"kind": KindResource, "uri": uri, "mime_type": mimeType},
		})
	}
	if err := resources.Err(); err != nil {
		return nil, err
	}
	prompts, err := s.store.DB.QueryContext(ctx, `SELECT name,title,description,arguments_json,revision,
    requires_approval FROM mcp_prompts WHERE server_id=? ORDER BY name`, server.ID)
	if err != nil {
		return nil, err
	}
	defer prompts.Close()
	for prompts.Next() {
		var name, title, description, arguments, revision string
		var approval int
		if err := prompts.Scan(&name, &title, &description, &arguments, &revision, &approval); err != nil {
			return nil, err
		}
		entries = append(entries, capabilities.Entry{
			ID: capabilityID(server.ID, promptCapabilityName(name)), Name: promptCapabilityName(name),
			Title:       firstNonEmpty(title, name),
			Description: firstNonEmpty(description, "Prompt template published by "+server.Name),
			Source:      capabilities.SourceMCP, SourceRef: server.ID, Revision: revision, Effect: "read",
			Readiness: readiness, RequiresApproval: approval != 0,
			InputSchema: promptInputSchema(json.RawMessage(arguments)),
			Metadata: map[string]any{"server_name": server.Name, "protocol": server.LastProtocol,
				"annotations_trusted": server.TrustAnnotations, "untrusted_output": true,
				"kind": KindPrompt, "prompt_name": name},
		})
	}
	return entries, prompts.Err()
}

// promptInputSchema turns an MCP prompt's argument list into the JSON Schema
// the catalog validates against, so a prompt is described to the model the same
// way a tool is.
func promptInputSchema(arguments json.RawMessage) json.RawMessage {
	var declared []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Required    bool   `json:"required"`
	}
	if err := json.Unmarshal(arguments, &declared); err != nil || len(declared) == 0 {
		return resourceSchema
	}
	properties := map[string]any{}
	required := []string{}
	for _, argument := range declared {
		name := strings.TrimSpace(argument.Name)
		if name == "" {
			continue
		}
		properties[name] = map[string]any{"type": "string", "description": boundedText(argument.Description, 1024)}
		if argument.Required {
			required = append(required, name)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return resourceSchema
	}
	return encoded
}

func resourceCapabilityName(uri string) string { return "resource:" + uri }
func promptCapabilityName(name string) string  { return "prompt:" + name }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// kindOf reads the catalog kind an entry was indexed with. An entry stored
// before resources and prompts existed has no kind and is a tool, which is what
// it was.
func kindOf(entry capabilities.Entry) string {
	if entry.Metadata == nil {
		return KindTool
	}
	if kind, ok := entry.Metadata["kind"].(string); ok && kind != "" {
		return kind
	}
	return KindTool
}

// executeResource reads one resource, refusing if the server has rewritten it
// since the model was shown its description.
func (s *Service) executeResource(ctx context.Context, server Server, entry capabilities.Entry) (capabilities.CallResult, error) {
	uri, _ := entry.Metadata["uri"].(string)
	if uri == "" {
		return capabilities.CallResult{}, &Error{Kind: ErrorProtocol, Operation: "resources/read", ServerID: server.ID,
			Message: "capability does not carry a resource URI"}
	}
	var currentRevision string
	err := s.store.DB.QueryRowContext(ctx, `SELECT revision FROM mcp_resources WHERE server_id=? AND uri=?`,
		server.ID, uri).Scan(&currentRevision)
	if err != nil {
		return capabilities.CallResult{}, classify("resolve resource", server.ID, err)
	}
	if currentRevision != entry.Revision {
		return capabilities.CallResult{}, &Error{Kind: ErrorRevision, Operation: "resources/read", ServerID: server.ID,
			Message: "MCP resource revision changed after it was described"}
	}
	credential, err := s.serverCredential(server)
	if err != nil {
		return capabilities.CallResult{}, err
	}
	raw, err := s.client.ReadResource(ctx, server, credential, uri)
	if err != nil {
		return capabilities.CallResult{}, redactError(err, credential)
	}
	return capabilities.CallResult{Output: string(boundedRaw(raw)), Metadata: map[string]any{
		"kind": KindResource, "uri": uri, "server_id": server.ID, "untrusted_output": true}}, nil
}

// executePrompt renders one prompt template. What comes back is the server's
// words, not Hermetrix's, so it is labelled untrusted like any other remote
// content: a prompt is data the model reads, never instructions it must obey.
func (s *Service) executePrompt(ctx context.Context, server Server, entry capabilities.Entry,
	arguments json.RawMessage) (capabilities.CallResult, error) {
	name, _ := entry.Metadata["prompt_name"].(string)
	if name == "" {
		return capabilities.CallResult{}, &Error{Kind: ErrorProtocol, Operation: "prompts/get", ServerID: server.ID,
			Message: "capability does not carry a prompt name"}
	}
	var currentRevision string
	err := s.store.DB.QueryRowContext(ctx, `SELECT revision FROM mcp_prompts WHERE server_id=? AND name=?`,
		server.ID, name).Scan(&currentRevision)
	if err != nil {
		return capabilities.CallResult{}, classify("resolve prompt", server.ID, err)
	}
	if currentRevision != entry.Revision {
		return capabilities.CallResult{}, &Error{Kind: ErrorRevision, Operation: "prompts/get", ServerID: server.ID,
			Message: "MCP prompt revision changed after it was described"}
	}
	values := map[string]any{}
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &values); err != nil {
			return capabilities.CallResult{}, &Error{Kind: ErrorProtocol, Operation: "prompts/get", ServerID: server.ID,
				Message: "prompt arguments must be a JSON object"}
		}
	}
	credential, err := s.serverCredential(server)
	if err != nil {
		return capabilities.CallResult{}, err
	}
	raw, err := s.client.GetPrompt(ctx, server, credential, name, values)
	if err != nil {
		return capabilities.CallResult{}, redactError(err, credential)
	}
	return capabilities.CallResult{Output: string(boundedRaw(raw)), Metadata: map[string]any{
		"kind": KindPrompt, "prompt_name": name, "server_id": server.ID, "untrusted_output": true}}, nil
}

// boundedRaw caps what a remote server can put into the transcript in one
// answer. A resource is arbitrary content and its size is the server's choice,
// not ours.
func boundedRaw(raw json.RawMessage) json.RawMessage {
	const ceiling = 256 << 10
	if len(raw) <= ceiling {
		return raw
	}
	encoded, err := json.Marshal(map[string]any{
		"truncated": true, "bytes": len(raw), "ceiling": ceiling,
		"preview": string(raw[:8<<10]),
	})
	if err != nil {
		return json.RawMessage(`{"truncated":true}`)
	}
	return encoded
}

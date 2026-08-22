package mcp

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"hermetrix-harness/internal/capabilities"
	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/store"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	defaultTimeoutMS = 15_000
	maxSchemaBytes   = 256 << 10
	maxToolOutput    = 2 << 20
)

var envNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,126}$`)

type Service struct {
	store       *store.Store
	catalog     *capabilities.Catalog
	client      *Client
	validatorMu sync.RWMutex
	validators  map[string]*jsonschema.Schema
}

func NewService(dataStore *store.Store, catalog *capabilities.Catalog, client *Client) *Service {
	if catalog == nil {
		catalog = capabilities.NewCatalog()
	}
	if client == nil {
		client = NewClient(nil)
	}
	service := &Service{store: dataStore, catalog: catalog, client: client, validators: map[string]*jsonschema.Schema{}}
	catalog.SetExecutor(capabilities.SourceMCP, service)
	return service
}

func (s *Service) Save(ctx context.Context, input SaveInput) (Server, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Endpoint = strings.TrimSpace(input.Endpoint)
	input.APIKeyEnv = strings.TrimSpace(input.APIKeyEnv)
	if input.TransportKind == "" {
		input.TransportKind = TransportStreamableHTTP
	}
	if input.ProtocolMode == "" {
		input.ProtocolMode = ProtocolAuto
	}
	if input.RequestTimeoutMS == 0 {
		input.RequestTimeoutMS = defaultTimeoutMS
	}
	if err := validateServerInput(input); err != nil {
		return Server{}, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := time.Now().UTC()
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Server{}, err
	}
	defer tx.Rollback()
	if input.ID == "" {
		input.ID = identity.New("mcp")
		_, err = tx.ExecContext(ctx, `INSERT INTO mcp_servers
      (id,name,transport_kind,endpoint,api_key_env,protocol_mode,trust_annotations,enabled,request_timeout_ms,status,created_at,updated_at)
      VALUES(?,?,?,?,?,?,?,?,?,'not_discovered',?,?)`, input.ID, input.Name, input.TransportKind, input.Endpoint,
			input.APIKeyEnv, input.ProtocolMode, boolInt(input.TrustAnnotations), boolInt(enabled), input.RequestTimeoutMS,
			formatTime(now), formatTime(now))
	} else {
		var exists int
		if scanErr := tx.QueryRowContext(ctx, `SELECT 1 FROM mcp_servers WHERE id=?`, input.ID).Scan(&exists); scanErr != nil {
			return Server{}, scanErr
		}
		_, err = tx.ExecContext(ctx, `UPDATE mcp_servers SET name=?,transport_kind=?,endpoint=?,api_key_env=?,protocol_mode=?,
      trust_annotations=?,enabled=?,request_timeout_ms=?,status='not_discovered',last_error='',last_protocol='',tool_count=0,
      last_discovered_at=NULL,updated_at=? WHERE id=?`, input.Name, input.TransportKind, input.Endpoint, input.APIKeyEnv,
			input.ProtocolMode, boolInt(input.TrustAnnotations), boolInt(enabled), input.RequestTimeoutMS, formatTime(now), input.ID)
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM mcp_tools WHERE server_id=?`, input.ID)
		}
	}
	if err != nil {
		return Server{}, fmt.Errorf("save MCP server: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Server{}, err
	}
	s.clearValidators()
	if err := s.reloadServerCatalog(ctx, input.ID); err != nil {
		return Server{}, err
	}
	return s.Get(ctx, input.ID)
}

func (s *Service) List(ctx context.Context) ([]Server, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,name,transport_kind,endpoint,api_key_env,protocol_mode,
    trust_annotations,enabled,request_timeout_ms,status,last_error,last_protocol,tool_count,last_discovered_at,created_at,updated_at
    FROM mcp_servers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Server{}
	for rows.Next() {
		item, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) Get(ctx context.Context, id string) (Server, error) {
	row := s.store.DB.QueryRowContext(ctx, `SELECT id,name,transport_kind,endpoint,api_key_env,protocol_mode,
    trust_annotations,enabled,request_timeout_ms,status,last_error,last_protocol,tool_count,last_discovered_at,created_at,updated_at
    FROM mcp_servers WHERE id=?`, id)
	return scanServer(row)
}

func (s *Service) ReloadCatalog(ctx context.Context) error {
	servers, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, server := range servers {
		if err := s.reloadServerCatalog(ctx, server.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Discover(ctx context.Context, serverID string) (DiscoveryResult, error) {
	server, err := s.Get(ctx, serverID)
	if err != nil {
		return DiscoveryResult{}, err
	}
	if !server.Enabled {
		return DiscoveryResult{}, &Error{Kind: ErrorNotReady, Operation: "discover", ServerID: server.ID, Message: "MCP server is disabled"}
	}
	credential, err := serverCredential(server)
	if err != nil {
		_ = s.recordDiscoveryFailure(context.WithoutCancel(ctx), server.ID, err)
		_ = s.reloadServerCatalog(context.WithoutCancel(ctx), server.ID)
		return DiscoveryResult{}, err
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, time.Duration(server.RequestTimeoutMS)*time.Millisecond)
	defer cancel()
	remoteTools, protocol, err := s.client.ListTools(discoveryCtx, server, credential)
	if err != nil {
		err = redactError(err, credential)
		_ = s.recordDiscoveryFailure(context.WithoutCancel(ctx), server.ID, err)
		_ = s.reloadServerCatalog(context.WithoutCancel(ctx), server.ID)
		return DiscoveryResult{}, err
	}
	accepted := make([]storedTool, 0, len(remoteTools))
	rejected := 0
	for _, remote := range remoteTools {
		tool, validateErr := prepareTool(server, protocol, remote)
		if validateErr != nil {
			rejected++
			continue
		}
		accepted = append(accepted, tool)
	}
	sort.Slice(accepted, func(i, j int) bool { return accepted[i].RemoteName < accepted[j].RemoteName })
	now := time.Now().UTC()
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return DiscoveryResult{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_tools WHERE server_id=?`, server.ID); err != nil {
		return DiscoveryResult{}, err
	}
	for _, tool := range accepted {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_tools
      (server_id,remote_name,title,description,input_schema_json,output_schema_json,annotations_json,revision,effect,requires_approval,discovered_at)
      VALUES(?,?,?,?,?,?,?,?,?,?,?)`, server.ID, tool.RemoteName, tool.Title, tool.Description, string(tool.InputSchema),
			string(tool.OutputSchema), string(tool.Annotations), tool.Revision, tool.Effect, boolInt(tool.RequiresApproval), formatTime(now)); err != nil {
			return DiscoveryResult{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mcp_servers SET status='ready',last_error='',last_protocol=?,tool_count=?,
    last_discovered_at=?,updated_at=? WHERE id=?`, protocol, len(accepted), formatTime(now), formatTime(now), server.ID); err != nil {
		return DiscoveryResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return DiscoveryResult{}, err
	}
	s.clearValidators()
	if err := s.reloadServerCatalog(ctx, server.ID); err != nil {
		return DiscoveryResult{}, err
	}
	return DiscoveryResult{ServerID: server.ID, Protocol: protocol, Tools: len(accepted), Rejected: rejected,
		Revision: snapshotRevision(accepted)}, nil
}

func (s *Service) ExecuteCapability(ctx context.Context, entry capabilities.Entry, arguments json.RawMessage) (capabilities.CallResult, error) {
	if entry.Source != capabilities.SourceMCP {
		return capabilities.CallResult{}, &Error{Kind: ErrorPolicy, Operation: "tools/call", Message: "capability is not owned by the MCP executor"}
	}
	server, err := s.Get(ctx, entry.SourceRef)
	if err != nil {
		return capabilities.CallResult{}, classify("load server", entry.SourceRef, err)
	}
	if !server.Enabled || server.Status != "ready" {
		return capabilities.CallResult{}, &Error{Kind: ErrorNotReady, Operation: "tools/call", ServerID: server.ID, Message: "MCP server is not ready"}
	}
	var currentRevision, inputSchema, outputSchema string
	err = s.store.DB.QueryRowContext(ctx, `SELECT revision,input_schema_json,output_schema_json FROM mcp_tools WHERE server_id=? AND remote_name=?`,
		server.ID, entry.Name).Scan(&currentRevision, &inputSchema, &outputSchema)
	if err != nil {
		return capabilities.CallResult{}, classify("resolve tool", server.ID, err)
	}
	if currentRevision != entry.Revision {
		return capabilities.CallResult{}, &Error{Kind: ErrorRevision, Operation: "tools/call", ServerID: server.ID,
			Message: "MCP tool revision changed after it was described"}
	}
	credential, err := serverCredential(server)
	if err != nil {
		return capabilities.CallResult{}, err
	}
	inputValidator, err := s.validator("input:"+entry.Revision, json.RawMessage(inputSchema))
	if err != nil {
		return capabilities.CallResult{}, &Error{Kind: ErrorProtocol, Operation: "compile input schema", ServerID: server.ID, Message: err.Error()}
	}
	instance, err := decodeSchemaInstance(arguments)
	if err != nil {
		return capabilities.CallResult{}, &Error{Kind: ErrorProtocol, Operation: "validate arguments", ServerID: server.ID, Message: err.Error()}
	}
	if err := inputValidator.Validate(instance); err != nil {
		return capabilities.CallResult{}, &Error{Kind: ErrorProtocol, Operation: "validate arguments", ServerID: server.ID,
			Message: "arguments do not match described input schema: " + err.Error()}
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(server.RequestTimeoutMS)*time.Millisecond)
	defer cancel()
	response, err := s.client.CallTool(callCtx, server, credential, server.LastProtocol, entry.Name, arguments, json.RawMessage(inputSchema))
	if err != nil {
		return capabilities.CallResult{}, redactError(err, credential)
	}
	redactedResult := redactJSON(response.Result, credential)
	if outputSchema != "" {
		outputValidator, validateErr := s.validator("output:"+entry.Revision, json.RawMessage(outputSchema))
		if validateErr == nil {
			validateErr = validateStructuredOutput(outputValidator, redactedResult)
		}
		if validateErr != nil {
			return capabilities.CallResult{}, &Error{Kind: ErrorProtocol, Operation: "validate tool result", ServerID: server.ID,
				Message: validateErr.Error()}
		}
	}
	if len(redactedResult) > maxToolOutput {
		return capabilities.CallResult{}, &Error{Kind: ErrorProtocol, Operation: "tools/call", ServerID: server.ID,
			Message: "MCP tool result exceeds 2 MiB harness limit"}
	}
	return capabilities.CallResult{Output: string(redactedResult), Metadata: map[string]any{
		"source": "mcp", "server_id": server.ID, "server_name": server.Name, "remote_tool": entry.Name,
		"protocol": response.Protocol, "untrusted_output": true, "automatic_retry": false,
	}}, nil
}

type storedTool struct {
	RemoteName       string
	Title            string
	Description      string
	InputSchema      json.RawMessage
	OutputSchema     json.RawMessage
	Annotations      json.RawMessage
	Revision         string
	Effect           string
	RequiresApproval bool
}

func prepareTool(server Server, protocol string, remote RemoteTool) (storedTool, error) {
	remote.Name = strings.TrimSpace(remote.Name)
	if remote.Name == "" || len(remote.Name) > 256 || !utf8.ValidString(remote.Name) {
		return storedTool{}, fmt.Errorf("invalid MCP tool name")
	}
	remote.Title = boundedText(remote.Title, 256)
	remote.Description = boundedText(remote.Description, 4096)
	if len(remote.InputSchema) == 0 || len(remote.InputSchema) > maxSchemaBytes || !json.Valid(remote.InputSchema) {
		return storedTool{}, fmt.Errorf("invalid or oversized MCP input schema")
	}
	if _, err := compileJSONSchema(remote.InputSchema); err != nil {
		return storedTool{}, fmt.Errorf("invalid MCP input schema: %w", err)
	}
	var root map[string]any
	if err := json.Unmarshal(remote.InputSchema, &root); err != nil {
		return storedTool{}, err
	}
	if protocol == ProtocolCurrent {
		if _, err := validateAndCollectHeaderBindings(remote.InputSchema); err != nil {
			return storedTool{}, err
		}
	}
	if len(remote.OutputSchema) > maxSchemaBytes || (len(remote.OutputSchema) > 0 && !json.Valid(remote.OutputSchema)) {
		return storedTool{}, fmt.Errorf("invalid or oversized MCP output schema")
	}
	if len(remote.OutputSchema) > 0 {
		if _, err := compileJSONSchema(remote.OutputSchema); err != nil {
			return storedTool{}, fmt.Errorf("invalid MCP output schema: %w", err)
		}
	}
	if len(remote.Annotations) > 64<<10 || (len(remote.Annotations) > 0 && !json.Valid(remote.Annotations)) {
		return storedTool{}, fmt.Errorf("invalid MCP annotations")
	}
	effect, approval := toolEffect(server.TrustAnnotations, remote.Annotations)
	revision, err := toolRevision(server, protocol, remote)
	if err != nil {
		return storedTool{}, err
	}
	return storedTool{RemoteName: remote.Name, Title: strings.TrimSpace(remote.Title), Description: strings.TrimSpace(remote.Description),
		InputSchema: canonicalJSON(remote.InputSchema), OutputSchema: canonicalJSON(remote.OutputSchema), Annotations: canonicalJSON(remote.Annotations),
		Revision: revision, Effect: effect, RequiresApproval: approval}, nil
}

func toolEffect(trusted bool, raw json.RawMessage) (string, bool) {
	if !trusted {
		return "unknown", true
	}
	var annotations ToolAnnotations
	if len(raw) == 0 || json.Unmarshal(raw, &annotations) != nil || annotations.ReadOnlyHint == nil {
		return "unknown", true
	}
	if *annotations.ReadOnlyHint {
		return "read", false
	}
	if annotations.DestructiveHint == nil || *annotations.DestructiveHint {
		return "destructive", true
	}
	return "external_mutation", true
}

func toolRevision(server Server, protocol string, tool RemoteTool) (string, error) {
	payload := struct {
		ServerID         string          `json:"server_id"`
		Endpoint         string          `json:"endpoint"`
		APIKeyEnv        string          `json:"api_key_env"`
		Protocol         string          `json:"protocol"`
		TrustAnnotations bool            `json:"trust_annotations"`
		Name             string          `json:"name"`
		Title            string          `json:"title"`
		Description      string          `json:"description"`
		InputSchema      json.RawMessage `json:"input_schema"`
		OutputSchema     json.RawMessage `json:"output_schema"`
		Annotations      json.RawMessage `json:"annotations"`
	}{server.ID, server.Endpoint, server.APIKeyEnv, protocol, server.TrustAnnotations, tool.Name, tool.Title, tool.Description,
		canonicalJSON(tool.InputSchema), canonicalJSON(tool.OutputSchema), canonicalJSON(tool.Annotations)}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "mcp-tool-" + hex.EncodeToString(sum[:16]), nil
}

func canonicalJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return append(json.RawMessage(nil), raw...)
	}
	encoded, _ := json.Marshal(value)
	return encoded
}

func (s *Service) reloadServerCatalog(ctx context.Context, serverID string) error {
	server, err := s.Get(ctx, serverID)
	if err != nil {
		return err
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT remote_name,title,description,input_schema_json,output_schema_json,
    annotations_json,revision,effect,requires_approval FROM mcp_tools WHERE server_id=? ORDER BY remote_name`, serverID)
	if err != nil {
		return err
	}
	defer rows.Close()
	entries := []capabilities.Entry{}
	readiness := serverReadiness(server)
	for rows.Next() {
		var name, title, description, inputSchema, outputSchema, annotations, revision, effect string
		var approval int
		if err := rows.Scan(&name, &title, &description, &inputSchema, &outputSchema, &annotations, &revision, &effect, &approval); err != nil {
			return err
		}
		entries = append(entries, capabilities.Entry{ID: capabilityID(server.ID, name), Name: name, Title: title,
			Description: description, Source: capabilities.SourceMCP, SourceRef: server.ID, Revision: revision, Effect: effect,
			Readiness: readiness, RequiresApproval: approval != 0, InputSchema: json.RawMessage(inputSchema),
			OutputSchema: json.RawMessage(outputSchema), Annotations: json.RawMessage(annotations), Metadata: map[string]any{
				"server_name": server.Name, "protocol": server.LastProtocol, "annotations_trusted": server.TrustAnnotations,
				"untrusted_output": true,
			}})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return s.catalog.ReplaceSourceRef(capabilities.SourceMCP, server.ID, entries)
}

func serverReadiness(server Server) string {
	if !server.Enabled {
		return capabilities.ReadinessOff
	}
	if !server.CredentialReady {
		return capabilities.ReadinessLocked
	}
	if server.Status != "ready" {
		return capabilities.ReadinessStale
	}
	return capabilities.ReadinessReady
}

func capabilityID(serverID, remoteName string) string {
	return "mcp:" + serverID + ":" + base64.RawURLEncoding.EncodeToString([]byte(remoteName))
}

func snapshotRevision(tools []storedTool) string {
	revisions := make([]string, 0, len(tools))
	for _, tool := range tools {
		revisions = append(revisions, tool.Revision)
	}
	sort.Strings(revisions)
	sum := sha256.Sum256([]byte(strings.Join(revisions, "\n")))
	return "mcp-catalog-" + hex.EncodeToString(sum[:12])
}

func (s *Service) recordDiscoveryFailure(ctx context.Context, serverID string, failure error) error {
	message := failure.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	_, err := s.store.DB.ExecContext(ctx, `UPDATE mcp_servers SET status='error',last_error=?,updated_at=? WHERE id=?`,
		message, formatTime(time.Now().UTC()), serverID)
	return err
}

func (s *Service) validator(key string, raw json.RawMessage) (*jsonschema.Schema, error) {
	s.validatorMu.RLock()
	compiled := s.validators[key]
	s.validatorMu.RUnlock()
	if compiled != nil {
		return compiled, nil
	}
	compiled, err := compileJSONSchema(raw)
	if err != nil {
		return nil, err
	}
	s.validatorMu.Lock()
	if existing := s.validators[key]; existing != nil {
		compiled = existing
	} else {
		s.validators[key] = compiled
	}
	s.validatorMu.Unlock()
	return compiled, nil
}

func (s *Service) clearValidators() {
	s.validatorMu.Lock()
	s.validators = map[string]*jsonschema.Schema{}
	s.validatorMu.Unlock()
}

func boundedText(value string, maxBytes int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return strings.TrimSpace(value[:end]) + "…"
}

func redactError(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	replace := func(value string) string { return strings.ReplaceAll(value, secret, "[REDACTED]") }
	var typed *Error
	if errors.As(err, &typed) {
		clone := *typed
		clone.Message = replace(clone.Message)
		clone.Cause = nil
		return &clone
	}
	return &Error{Kind: ErrorTransport, Message: replace(err.Error())}
}

func redactJSON(raw json.RawMessage, secret string) json.RawMessage {
	if secret == "" || len(raw) == 0 {
		return append(json.RawMessage(nil), raw...)
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return json.RawMessage(strings.ReplaceAll(string(raw), secret, "[REDACTED]"))
	}
	var walk func(any) any
	walk = func(item any) any {
		switch typed := item.(type) {
		case string:
			return strings.ReplaceAll(typed, secret, "[REDACTED]")
		case []any:
			for index := range typed {
				typed[index] = walk(typed[index])
			}
		case map[string]any:
			for key, nested := range typed {
				typed[key] = walk(nested)
			}
		}
		return item
	}
	encoded, err := json.Marshal(walk(value))
	if err != nil {
		return json.RawMessage(`{"content":[{"type":"text","text":"[REDACTED INVALID RESULT]"}],"isError":true}`)
	}
	return encoded
}

func validateServerInput(input SaveInput) error {
	if input.Name == "" || len(input.Name) > 80 {
		return fmt.Errorf("MCP server name is required and must be at most 80 characters")
	}
	if input.TransportKind != TransportStreamableHTTP {
		return fmt.Errorf("unsupported MCP transport %q", input.TransportKind)
	}
	parsed, err := url.Parse(input.Endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("MCP endpoint must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return fmt.Errorf("MCP endpoint must use https; http is allowed only on loopback")
	}
	if input.APIKeyEnv != "" && !envNamePattern.MatchString(input.APIKeyEnv) {
		return fmt.Errorf("MCP API key environment variable must use uppercase letters, digits, and underscores")
	}
	if input.ProtocolMode != ProtocolAuto && input.ProtocolMode != ProtocolCurrent && input.ProtocolMode != ProtocolLegacy {
		return fmt.Errorf("MCP protocol mode must be auto, %s, or %s", ProtocolCurrent, ProtocolLegacy)
	}
	if input.RequestTimeoutMS < 1000 || input.RequestTimeoutMS > 120_000 {
		return fmt.Errorf("MCP request timeout must be between 1000 and 120000 milliseconds")
	}
	return nil
}

func serverCredential(server Server) (string, error) {
	if server.APIKeyEnv == "" {
		return "", nil
	}
	value, ok := os.LookupEnv(server.APIKeyEnv)
	if !ok || strings.TrimSpace(value) == "" {
		return "", &Error{Kind: ErrorNotReady, Operation: "credential", ServerID: server.ID,
			Message: "credential environment variable " + server.APIKeyEnv + " is not set"}
	}
	return value, nil
}

type scanner interface{ Scan(...any) error }

func scanServer(row scanner) (Server, error) {
	var item Server
	var trusted, enabled int
	var lastDiscovered sql.NullString
	var created, updated string
	if err := row.Scan(&item.ID, &item.Name, &item.TransportKind, &item.Endpoint, &item.APIKeyEnv, &item.ProtocolMode,
		&trusted, &enabled, &item.RequestTimeoutMS, &item.Status, &item.LastError, &item.LastProtocol, &item.ToolCount,
		&lastDiscovered, &created, &updated); err != nil {
		return Server{}, err
	}
	item.TrustAnnotations = trusted != 0
	item.Enabled = enabled != 0
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if lastDiscovered.Valid {
		value, _ := time.Parse(time.RFC3339Nano, lastDiscovered.String)
		item.LastDiscoveredAt = &value
	}
	if item.APIKeyEnv == "" {
		item.CredentialReady = true
	} else if value, ok := os.LookupEnv(item.APIKeyEnv); ok && strings.TrimSpace(value) != "" {
		item.CredentialReady = true
	}
	return item, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

var _ capabilities.Executor = (*Service)(nil)

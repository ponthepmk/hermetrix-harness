package mcp

import (
	"encoding/json"
	"time"
)

const (
	TransportStreamableHTTP = "streamable-http"
	ProtocolAuto            = "auto"
	ProtocolCurrent         = "2026-07-28"
	ProtocolLegacy          = "2025-11-25"
)

type Server struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	TransportKind    string     `json:"transport_kind"`
	Endpoint         string     `json:"endpoint"`
	APIKeyEnv        string     `json:"api_key_env,omitempty"`
	CredentialReady  bool       `json:"credential_ready"`
	ProtocolMode     string     `json:"protocol_mode"`
	TrustAnnotations bool       `json:"trust_annotations"`
	Enabled          bool       `json:"enabled"`
	RequestTimeoutMS int        `json:"request_timeout_ms"`
	Status           string     `json:"status"`
	LastError        string     `json:"last_error,omitempty"`
	LastProtocol     string     `json:"last_protocol,omitempty"`
	ToolCount        int        `json:"tool_count"`
	LastDiscoveredAt *time.Time `json:"last_discovered_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type SaveInput struct {
	ID               string `json:"id,omitempty"`
	Name             string `json:"name"`
	TransportKind    string `json:"transport_kind,omitempty"`
	Endpoint         string `json:"endpoint"`
	APIKeyEnv        string `json:"api_key_env,omitempty"`
	ProtocolMode     string `json:"protocol_mode,omitempty"`
	TrustAnnotations bool   `json:"trust_annotations"`
	Enabled          *bool  `json:"enabled,omitempty"`
	RequestTimeoutMS int    `json:"request_timeout_ms,omitempty"`
}

type DiscoveryResult struct {
	ServerID string `json:"server_id"`
	Protocol string `json:"protocol"`
	Tools    int    `json:"tools"`
	Rejected int    `json:"rejected"`
	Revision string `json:"catalog_revision"`
}

type RemoteTool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	Annotations  json.RawMessage `json:"annotations,omitempty"`
}

type ToolAnnotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool `json:"openWorldHint,omitempty"`
}

type callResponse struct {
	Protocol string
	Result   json.RawMessage
}

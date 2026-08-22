package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
)

const (
	ErrorConfiguration = "configuration"
	ErrorNotReady      = "not_ready"
	ErrorTransport     = "transport"
	ErrorTimeout       = "timeout"
	ErrorCancelled     = "cancelled"
	ErrorProtocol      = "protocol"
	ErrorRemote        = "remote"
	ErrorPolicy        = "policy"
	ErrorRevision      = "revision_conflict"
)

type Error struct {
	Kind       string `json:"kind"`
	Operation  string `json:"operation,omitempty"`
	ServerID   string `json:"server_id,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	RPCCode    int    `json:"rpc_code,omitempty"`
	Message    string `json:"message"`
	Cause      error  `json:"-"`
}

func (e *Error) Error() string {
	prefix := "MCP"
	if e.Kind != "" {
		prefix += " " + e.Kind
	}
	if e.Operation != "" {
		prefix += " during " + e.Operation
	}
	return prefix + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func classify(operation, serverID string, err error) error {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) {
		if typed.Operation == "" {
			typed.Operation = operation
		}
		if typed.ServerID == "" {
			typed.ServerID = serverID
		}
		return typed
	}
	kind := ErrorTransport
	if errors.Is(err, context.DeadlineExceeded) {
		kind = ErrorTimeout
	} else if errors.Is(err, context.Canceled) {
		kind = ErrorCancelled
	} else {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			kind = ErrorTimeout
		}
	}
	return &Error{Kind: kind, Operation: operation, ServerID: serverID, Message: fmt.Sprint(err), Cause: err}
}

package piidentity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	PiCommit       = "d68bdd3921d4555f7218a280ee62c9b5e6b78de0"
	PiSchema       = 9
	NodeID   int64 = 3
	NodeKey        = "windows-pc-main"
	AgentID  int64 = 2
	AgentKey       = "hermetrix-bonsai"
)

// Caller is deliberately narrower than the MCP client: no arbitrary tool can
// be selected by a model, configuration, or capability advertisement.
type Caller interface {
	Call(context.Context, string, json.RawMessage) (json.RawMessage, error)
}

var ErrTransient = errors.New("transient Pi transport failure")

func Probe(ctx context.Context, caller Caller) (Receipt, error) {
	for retry := 0; retry <= 1; retry++ {
		receipt, err := probeOnce(ctx, caller)
		if err == nil {
			receipt.RetryCount = retry
			return receipt, nil
		}
		if !errors.Is(err, ErrTransient) || retry == 1 || ctx.Err() != nil {
			return Receipt{}, err
		}
	}
	return Receipt{}, errors.New("unreachable retry state")
}

// ProbeFresh runs the reconnect proof without another retry allowance. A full
// CLI invocation therefore has at most one transient retry in total.
func ProbeFresh(ctx context.Context, caller Caller) (Receipt, error) {
	return probeOnce(ctx, caller)
}

func probeOnce(ctx context.Context, caller Caller) (Receipt, error) {
	var identity Identity
	if err := read(ctx, caller, "whoami", json.RawMessage(`{}`), &identity); err != nil {
		return Receipt{}, err
	}
	if !identity.Authenticated || identity.AuthMethod != "scoped_agent" || identity.NodeKey != NodeKey ||
		identity.AgentKey != AgentKey || identity.CredentialID <= 0 || len(identity.Scopes) != 1 || identity.Scopes[0] != "read" {
		return Receipt{}, errors.New("Pi identity, credential, or effective scope mismatch")
	}
	var node Node
	if err := read(ctx, caller, "get_node", json.RawMessage(`{"node_key":"windows-pc-main"}`), &node); err != nil {
		return Receipt{}, err
	}
	if node.ID != NodeID || node.NodeKey != NodeKey || node.Status != "active" {
		return Receipt{}, errors.New("Pi node mismatch or disabled")
	}
	var agent Agent
	if err := read(ctx, caller, "get_agent", json.RawMessage(`{"agent_key":"hermetrix-bonsai"}`), &agent); err != nil {
		return Receipt{}, err
	}
	if agent.ID != AgentID || agent.AgentKey != AgentKey || agent.AgentType != "managed" || agent.NodeKey != node.NodeKey ||
		agent.Status != "active" || agent.NodeStatus != "active" {
		return Receipt{}, errors.New("Pi agent mismatch, disabled, or bound to another node")
	}
	var capabilities Capabilities
	if err := read(ctx, caller, "list_capabilities", json.RawMessage(`{}`), &capabilities); err != nil {
		return Receipt{}, err
	}
	if capabilities.AgentKey != AgentKey || capabilities.Capabilities == nil {
		return Receipt{}, errors.New("Pi capability identity mismatch")
	}
	return Receipt{PiCommit: PiCommit, PiSchema: PiSchema, RealPiContacted: true, Node: node, Agent: agent,
		EffectiveScopes: []string{"read"}, CredentialID: identity.CredentialID,
		CapabilitiesCount: len(capabilities.Capabilities), CapabilitiesInformational: true,
		FreshWhoamiCount: 1, WorkspaceIdentical: true}, nil
}

func read(ctx context.Context, caller Caller, name string, args json.RawMessage, target any) error {
	raw, err := caller.Call(ctx, name, args)
	if err != nil {
		return fmt.Errorf("Pi %s: %w", name, err)
	}
	var rejected struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &rejected); err != nil {
		return fmt.Errorf("Pi %s malformed response: %w", name, err)
	}
	if rejected.Error != "" {
		return fmt.Errorf("Pi %s rejected request", name)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("Pi %s malformed fields: %w", name, err)
	}
	return nil
}

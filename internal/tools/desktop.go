package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hermetrix-harness/internal/providers"
	"strings"
)

func desktopNeedsApproval(raw string) (bool, error) {
	var args struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return false, err
	}
	switch args.Action {
	case "list", "observe":
		return false, nil
	case "view", "click", "invoke", "set_value":
		return true, nil
	default:
		return false, fmt.Errorf("unsupported desktop action")
	}
}
func (r *Registry) planDesktopApproval(_ context.Context, call providers.ToolCall, definition Definition) (ApprovalPlan, error) {
	needed, err := desktopNeedsApproval(call.Arguments)
	if err != nil || !needed {
		return ApprovalPlan{}, fmt.Errorf("desktop mutation approval requires view, click, invoke or set_value")
	}
	if len(call.Arguments) > 8192 {
		return ApprovalPlan{}, fmt.Errorf("desktop arguments exceed 8 KiB")
	}
	var args struct {
		Handle          int64  `json:"handle"`
		ProcessID       int    `json:"process_id"`
		ObservationHash string `json:"observation_hash"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return ApprovalPlan{}, err
	}
	if args.Handle == 0 || args.ProcessID < 1 || strings.TrimSpace(args.ObservationHash) == "" {
		return ApprovalPlan{}, fmt.Errorf("desktop action requires a freshly observed window")
	}
	sum := sha256.Sum256([]byte(call.Arguments))
	return ApprovalPlan{ToolCallID: call.ID, Name: call.Name, Revision: definition.Revision, Effect: definition.Effect, ArgumentsHash: hex.EncodeToString(sum[:]), Summary: fmt.Sprintf("Control reviewed Windows window %d · PID %d once", args.Handle, args.ProcessID), Preview: call.Arguments}, nil
}

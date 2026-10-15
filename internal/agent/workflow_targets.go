package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"

	"hermetrix-harness/internal/providers"
	toolruntime "hermetrix-harness/internal/tools"
)

func workflowOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("workflow target requires an HTTP(S) origin without credentials")
	}
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return parsed.Scheme + "://" + host, nil
}

func (s *Service) bindWorkflowTargets(ctx context.Context, consent *WorkspaceWriteApproval, input *WorkspaceWriteApprovalInput) error {
	if len(input.BrowserOrigins) > 8 || len(input.DesktopWindows) > 4 {
		return fmt.Errorf("choose at most eight web origins and four exact windows")
	}
	seen := map[string]bool{}
	for _, raw := range input.BrowserOrigins {
		origin, err := workflowOrigin(raw)
		if err != nil {
			return err
		}
		parsed, _ := url.Parse(strings.TrimSpace(raw))
		if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("enter the web origin only; permission covers that origin's pages")
		}
		if !seen[origin] {
			consent.BrowserOrigins = append(consent.BrowserOrigins, origin)
			seen[origin] = true
		}
	}
	for _, window := range input.DesktopWindows {
		if window.Handle == 0 || window.ProcessID < 1 || s.desktop == nil {
			return fmt.Errorf("choose an available exact desktop window")
		}
		observation, err := s.desktop.ObserveDesktopWindow(ctx, window.Handle, window.ProcessID)
		if err != nil || observation.Window.Handle != window.Handle || observation.Window.ProcessID != window.ProcessID {
			return fmt.Errorf("desktop window changed before authorization; select it again")
		}
		consent.DesktopWindows = append(consent.DesktopWindows, window)
	}
	if len(consent.BrowserOrigins) > 0 || len(consent.DesktopWindows) > 0 {
		consent.Revision = "project-workflow-v1"
	}
	return nil
}

func workflowConsentMatches(session Session) bool {
	consent := session.Contract.WorkspaceWriteApproval
	return consent != nil && consent.Revision == "project-workflow-v1" && session.Contract.WorkMode != "audit" &&
		session.ProjectID != "" && consent.ProjectID == session.ProjectID && consent.OwnerID == session.OwnerPrincipalID &&
		consent.Reason != "" && session.ContractRevision == sessionContractRevision(session.Contract)
}

// Decide only from frozen owner-selected targets and a fresh project-owned tab.
// Page text, model arguments and claimed app names cannot extend the targets.
func (s *Service) workflowTargetPermitted(ctx context.Context, session Session, binding StepBinding, call providers.ToolCall) (bool, error) {
	if !workflowConsentMatches(session) {
		return false, nil
	}
	definition, ok := boundDefinition(binding, call.Name)
	if !ok {
		return false, nil
	}
	consent := session.Contract.WorkspaceWriteApproval
	if call.Name == "desktop" && definition.Revision == "v1" {
		args, err := decodeDesktopArgs(call.Arguments)
		if err != nil {
			return false, err
		}
		// Image sharing still requires verified vision and its explicit review.
		if args.Action != "click" && args.Action != "invoke" && args.Action != "set_value" {
			return false, nil
		}
		for _, window := range consent.DesktopWindows {
			if args.Handle == window.Handle && args.ProcessID == window.ProcessID {
				return true, nil
			}
		}
		return false, nil
	}
	if call.Name != "browser" || definition.Revision != "v6" || len(consent.BrowserOrigins) == 0 {
		return false, nil
	}
	var args browserArgs
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return false, err
	}
	allowed := func(raw string) bool {
		origin, err := workflowOrigin(raw)
		if err != nil {
			return false
		}
		for _, target := range consent.BrowserOrigins {
			if origin == target {
				return true
			}
		}
		return false
	}
	switch args.Action {
	case "open":
		return allowed(args.URL) && toolruntime.BrowserNeedsApproval(args.Action, args.URL), nil
	case "navigate", "click", "type", "key":
		if s.browser == nil || args.TabID == "" {
			return false, nil
		}
		page, err := s.browser.ActOnPage(ctx, args.TabID, BrowserActRequest{ProjectID: session.ProjectID, Action: "read", Actor: "agent:" + session.ID})
		if err != nil {
			return false, err
		}
		if !allowed(page.URL) {
			return false, nil
		}
		if args.Action == "navigate" && !allowed(args.URL) {
			return false, nil
		}
		if args.Action == "navigate" && !toolruntime.BrowserNeedsApproval(args.Action, args.URL) {
			return false, nil
		}
		return true, nil
	default:
		return false, nil
	}
}

func (s *Service) executeAuthorizedWorkflowEffect(ctx context.Context, session Session, provider providers.Profile, turnID string,
	binding StepBinding, call providers.ToolCall, definition toolruntime.Definition, emit func(StreamEvent) error) (toolruntime.Receipt, error) {
	failed := toolruntime.Receipt{ToolCallID: call.ID, Name: call.Name, Revision: definition.Revision, Effect: definition.Effect, Status: "failed"}
	consent := session.Contract.WorkspaceWriteApproval
	root, err := s.workspaceWriteRoot(ctx, session.ProjectID, session.OwnerPrincipalID)
	if err != nil || !workflowConsentMatches(session) || root != consent.Root {
		failed.Error = "workflow authorization no longer matches the project folder"
		return failed, s.persistToolResult(ctx, session, provider, turnID, binding, failed, emit)
	}
	plan, err := s.tools.PlanApproval(ctx, call)
	if err == nil && call.Name == "browser" {
		plan, err = s.enrichBrowserApproval(ctx, session, call, plan)
	}
	if err == nil && call.Name == "desktop" {
		plan, err = s.enrichDesktopApproval(ctx, session, call, plan)
	}
	if err != nil {
		failed.Error = err.Error()
		return failed, s.persistToolResult(ctx, session, provider, turnID, binding, failed, emit)
	}
	if plan.Metadata == nil {
		plan.Metadata = map[string]any{}
	}
	plan.Metadata["authorization"] = consent.Revision
	approval, _, err := s.createToolApproval(ctx, session, provider, turnID, binding, call, plan)
	if err != nil {
		return failed, err
	}
	approval, event, err := s.claimApprovalDecision(ctx, approval.ID, ApprovalDecisionInput{Decision: "approve", Actor: "owner:" + consent.OwnerID, Reason: "Session workflow authorization: " + consent.Reason})
	if err != nil {
		return failed, err
	}
	if emit != nil {
		err = emit(StreamEvent{Type: "approval_decision", TurnID: turnID, Event: &event, Approval: &approval})
	}
	if err == nil {
		err = s.checkTurnControl(ctx, session.ID, turnID)
	}
	if err == nil {
		var latestRoot string
		latestRoot, err = s.workspaceWriteRoot(ctx, session.ProjectID, session.OwnerPrincipalID)
		if err == nil && latestRoot != root {
			err = fmt.Errorf("authorized project folder changed before dispatch")
		}
	}
	if err == nil {
		var permitted bool
		permitted, err = s.workflowTargetPermitted(ctx, session, binding, call)
		if err == nil && !permitted {
			err = fmt.Errorf("workflow target changed before dispatch; inspect again")
		}
	}
	dispatched := false
	if err != nil {
		failed.Error = err.Error()
		if call.Name == "desktop" && s.desktop != nil {
			_, _ = s.desktop.CancelDesktopAction(context.WithoutCancel(ctx), metadataString(plan.Metadata, "desktop_action_id"), metadataString(plan.Metadata, "desktop_subject_hash"))
		}
	} else if call.Name == "browser" {
		dispatched = true
		failed = s.executeBrowserTool(ctx, session, call, definition, true)
	} else {
		dispatched = true
		failed = s.executeDesktopTool(ctx, call, definition, &approval)
	}
	if dispatched && ctx.Err() != nil && failed.Status != "succeeded" {
		failed.Status = "uncertain"
	}
	result, finished, persistErr := s.persistApprovalReceipt(context.WithoutCancel(ctx), approval, "approve", failed)
	if persistErr != nil {
		return failed, persistErr
	}
	if emit != nil {
		if emitErr := emit(StreamEvent{Type: "tool_result", TurnID: turnID, Event: &result, Approval: &finished}); emitErr != nil {
			return failed, emitErr
		}
	}
	return failed, err
}

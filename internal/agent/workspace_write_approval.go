package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"hermetrix-harness/internal/providers"
	toolruntime "hermetrix-harness/internal/tools"
)

func (s *Service) workspaceWriteRoot(ctx context.Context, projectID, ownerID string) (string, error) {
	var root string
	err := s.store.DB.QueryRowContext(ctx, `SELECT COALESCE(root_path,'') FROM projects WHERE id=? AND state='active' AND owner_principal_id=?`, projectID, ownerID).Scan(&root)
	if err != nil || strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("automatic file writes require an active owned project with a code folder")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// Each file still has its own persisted exact grant, optimistic hash check and
// terminal receipt. Consent only replaces the repeated human pause; it grants
// no extra browser/desktop/MCP authority and never changes inference quarantine.
// Selected browser/window authority is handled separately by workflow_targets.go.
func (s *Service) executeAuthorizedWorkspaceWrite(ctx context.Context, session Session, provider providers.Profile,
	turnID string, binding StepBinding, call providers.ToolCall, emit func(StreamEvent) error) (toolruntime.Receipt, error) {
	failed := toolruntime.Receipt{ToolCallID: call.ID, Name: call.Name, Revision: "v1", Effect: "write", Status: "failed"}
	consent := session.Contract.WorkspaceWriteApproval
	definition, present := boundDefinition(binding, call.Name)
	if consent == nil || (consent.Revision != "project-file-writes-v1" && consent.Revision != "project-workflow-v1") || session.Contract.WorkMode == "audit" ||
		call.Name != "workspace.write_file" || !present || definition.Revision != "v1" || definition.Effect != "write" || !definition.RequiresApproval ||
		consent.ProjectID != session.ProjectID || consent.OwnerID != session.OwnerPrincipalID || consent.Reason == "" ||
		session.ContractRevision != sessionContractRevision(session.Contract) {
		failed.Error = "file-write authorization does not match the frozen session"
		return failed, s.persistToolResult(ctx, session, provider, turnID, binding, failed, emit)
	}
	root, err := s.workspaceWriteRoot(ctx, session.ProjectID, session.OwnerPrincipalID)
	if err != nil || root != consent.Root {
		failed.Error = "project folder changed after file-write authorization; start a new session for the selected folder"
		return failed, s.persistToolResult(ctx, session, provider, turnID, binding, failed, emit)
	}
	scoped, err := s.tools.For(root)
	if err != nil {
		failed.Error = err.Error()
		return failed, s.persistToolResult(ctx, session, provider, turnID, binding, failed, emit)
	}
	plan, err := scoped.PlanApproval(ctx, call)
	if err != nil {
		failed.Error = err.Error()
		return failed, s.persistToolResult(ctx, session, provider, turnID, binding, failed, emit)
	}
	plan.Metadata["authorization"] = consent.Revision
	approval, _, err := s.createToolApproval(ctx, session, provider, turnID, binding, call, plan)
	if err != nil {
		return failed, err
	}
	approval, event, err := s.claimApprovalDecision(ctx, approval.ID, ApprovalDecisionInput{
		Decision: "approve", Actor: "owner:" + consent.OwnerID,
		Reason: "Session project-file authorization: " + consent.Reason})
	if err != nil {
		return failed, err
	}
	if emit != nil {
		if err := emit(StreamEvent{Type: "approval_decision", TurnID: turnID, Event: &event, Approval: &approval}); err != nil {
			failed.Status, failed.Error = "failed", "authorization recorded but file write was not dispatched"
			_, _, persistErr := s.persistApprovalReceipt(context.WithoutCancel(ctx), approval, "approve", failed)
			if persistErr != nil {
				return failed, persistErr
			}
			return failed, err
		}
	}
	// Recheck Stop and the approved root immediately before dispatch.
	if err = s.checkTurnControl(ctx, session.ID, turnID); err == nil {
		var latestRoot string
		latestRoot, err = s.workspaceWriteRoot(ctx, session.ProjectID, session.OwnerPrincipalID)
		if err == nil && latestRoot != root {
			err = fmt.Errorf("authorized project folder changed before dispatch")
		}
	}
	if err != nil {
		failed.Error = err.Error()
	} else {
		started := time.Now()
		failed = scoped.ExecuteApproved(ctx, call, toolruntime.ApprovalGrant{ToolCallID: approval.ToolCallID,
			Name: approval.ToolName, Revision: approval.ToolRevision, Effect: approval.Effect, ArgumentsHash: approval.ArgumentsHash})
		failed.DurationMS = time.Since(started).Milliseconds()
		if ctx.Err() != nil && failed.Status != "succeeded" {
			failed.Status, failed.Error = "uncertain", "Stopped during a file write; inspect the file before continuing."
		}
	}
	result, finished, err := s.persistApprovalReceipt(context.WithoutCancel(ctx), approval, "approve", failed)
	if err == nil && emit != nil {
		err = emit(StreamEvent{Type: "tool_result", TurnID: turnID, Event: &result, Approval: &finished})
	}
	return failed, err
}

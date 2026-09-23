package discordbridge

import (
	"context"
	"fmt"
	"strings"

	"hermetrix-harness/internal/agent"
	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
)

// Binding is trusted local configuration, never a set of Discord command
// options. The transport authenticates the Discord user and resolves this
// binding before calling the backend.
type Binding struct {
	OwnerPrincipalID string
	ProjectID        string
	ProviderID       string
	ContextProfile   string
}

// ApprovalSummary exposes the persisted exact-call preview, not an authority
// grant. The transport must remember which complete preview was delivered to
// which authenticated user before accepting a remote decision.
type ApprovalSummary struct {
	ID            string
	SessionID     string
	ToolName      string
	Summary       string
	Preview       string
	ArgumentsHash string
}

// Reply contains only the answer or status intended for the remote user. Raw
// provider reasoning, context reports and tool argument JSON stay in the app.
type Reply struct {
	Text     string
	Approval *ApprovalSummary
}

// Backend keeps the Discord protocol independent of the agent implementation.
// A session ID must come from the transport's persisted binding, not user input.
type Backend interface {
	Validate(context.Context, Binding) error
	New(context.Context, Binding, string) (string, error)
	Ask(context.Context, Binding, string, string) (Reply, error)
	Status(context.Context, Binding, string) (Reply, error)
	GetApproval(context.Context, Binding, string, string) (ApprovalSummary, error)
	Decide(context.Context, Binding, string, string, string, string) (Reply, error)
}

// AgentAdapter uses the same immutable sessions, admission rules, turn lease,
// and exact-call approvals as the local application. It never invents a
// qualification override, remote-provider egress grant or approval actor.
type AgentAdapter struct {
	agent     *agent.Service
	providers *providers.Service
	product   *product.Service
}

func NewAgentAdapter(agentService *agent.Service, providerService *providers.Service, productService *product.Service) *AgentAdapter {
	return &AgentAdapter{agent: agentService, providers: providerService, product: productService}
}

var _ Backend = (*AgentAdapter)(nil)

// Validate performs read-only scope checks. CreateSession remains responsible
// for the exact qualification and answer-budget checks when a new task opens.
func (a *AgentAdapter) Validate(ctx context.Context, binding Binding) error {
	_, err := a.boundContext(ctx, binding)
	return err
}

func (a *AgentAdapter) boundContext(ctx context.Context, binding Binding) (context.Context, error) {
	if a == nil || a.agent == nil || a.providers == nil || a.product == nil {
		return nil, fmt.Errorf("Discord agent backend is unavailable")
	}
	if strings.TrimSpace(binding.OwnerPrincipalID) == "" || strings.TrimSpace(binding.ProjectID) == "" ||
		strings.TrimSpace(binding.ProviderID) == "" || strings.TrimSpace(binding.ContextProfile) == "" {
		return nil, fmt.Errorf("Discord requires a configured local owner, project, provider and context profile")
	}
	// A missing principal otherwise falls back to the installation owner in
	// store.OwnerPrincipalID. Remote ingress must never use that fallback.
	ctx = identity.WithPrincipal(ctx, binding.OwnerPrincipalID)
	project, err := a.product.GetProject(ctx, binding.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("Discord project is unavailable to its configured owner: %w", err)
	}
	if project.State != "active" || project.OwnerPrincipalID != binding.OwnerPrincipalID {
		return nil, fmt.Errorf("Discord requires an active project owned by its configured local principal")
	}
	provider, err := a.providers.Get(ctx, binding.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("load Discord provider: %w", err)
	}
	if !provider.Enabled || !providers.IsLocalProfile(provider) {
		return nil, fmt.Errorf("Discord requires an enabled local provider; select one in Models")
	}
	profile, ok := ctxcompiler.ProfileByName(binding.ContextProfile)
	if !ok {
		return nil, fmt.Errorf("unknown Discord context profile %q", binding.ContextProfile)
	}
	if profile.Total > provider.ContextWindow {
		return nil, fmt.Errorf("Discord context profile exceeds the selected provider's declared context window")
	}
	return ctx, nil
}

func (a *AgentAdapter) boundSession(ctx context.Context, binding Binding, sessionID string) (context.Context, agent.Session, error) {
	ctx, err := a.boundContext(ctx, binding)
	if err != nil {
		return nil, agent.Session{}, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, agent.Session{}, fmt.Errorf("no Discord task is bound; start a new task first")
	}
	session, err := a.agent.GetSession(ctx, sessionID)
	if err != nil {
		return nil, agent.Session{}, fmt.Errorf("load Discord task: %w", err)
	}
	if session.OwnerPrincipalID != binding.OwnerPrincipalID || session.ProjectID != binding.ProjectID ||
		session.ProviderID != binding.ProviderID || session.ContextProfile != binding.ContextProfile || session.EgressPolicy != "local_only" {
		return nil, agent.Session{}, fmt.Errorf("task does not match the configured Discord owner, project and local model; start a new task")
	}
	return ctx, session, nil
}

func (a *AgentAdapter) New(ctx context.Context, binding Binding, title string) (string, error) {
	ctx, err := a.boundContext(ctx, binding)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(title) == "" {
		title = "Discord task"
	}
	session, err := a.agent.CreateSession(ctx, agent.CreateSessionInput{Title: title, ProjectID: binding.ProjectID,
		ProviderID: binding.ProviderID, ContextProfile: binding.ContextProfile, EgressPolicy: "local_only"})
	if err != nil {
		return "", fmt.Errorf("open Discord task: %w", err)
	}
	return session.ID, nil
}

func (a *AgentAdapter) Ask(ctx context.Context, binding Binding, sessionID, text string) (Reply, error) {
	ctx, _, err := a.boundSession(ctx, binding, sessionID)
	if err != nil {
		return Reply{}, err
	}
	result, err := a.agent.RunTurn(ctx, sessionID, agent.TurnInput{Content: text}, nil)
	if err != nil {
		return Reply{}, err
	}
	return replyFromTurn(result), nil
}

func (a *AgentAdapter) Status(ctx context.Context, binding Binding, sessionID string) (Reply, error) {
	ctx, session, err := a.boundSession(ctx, binding, sessionID)
	if err != nil {
		return Reply{}, err
	}
	reply := Reply{Text: fmt.Sprintf("Task: %s\nSession: %s\nState: %s", session.Title, session.ID, session.State)}
	if session.ActiveTurnID != "" {
		reply.Text += "\nActive turn: " + session.ActiveTurnID
	}
	// Read a bounded tail, not an entire conversation and all attachment data
	// for every status command. GetSession above is the ownership boundary for
	// these lower-level read APIs.
	page, err := a.agent.ListEventsPage(ctx, sessionID, 0, int(^uint(0)>>1), 100)
	if err != nil {
		return Reply{}, err
	}
	for i := len(page.Items) - 1; i >= 0; i-- {
		event := page.Items[i]
		if event.Role == "assistant" && event.EventKind == "message" && strings.TrimSpace(event.Content) != "" {
			reply.Text += "\n\nLatest answer:\n" + event.Content
			break
		}
	}
	if session.State == "awaiting_approval" {
		approvals, err := a.agent.ListApprovals(ctx, sessionID)
		if err != nil {
			return Reply{}, err
		}
		for i := len(approvals) - 1; i >= 0; i-- {
			if approvals[i].State == "pending" {
				summary := approvalSummary(approvals[i])
				reply.Approval = &summary
				break
			}
		}
	}
	return reply, nil
}

func (a *AgentAdapter) GetApproval(ctx context.Context, binding Binding, sessionID, approvalID string) (ApprovalSummary, error) {
	ctx, session, err := a.boundSession(ctx, binding, sessionID)
	if err != nil {
		return ApprovalSummary{}, err
	}
	approval, err := a.pendingApproval(ctx, session, approvalID)
	if err != nil {
		return ApprovalSummary{}, err
	}
	return approvalSummary(approval), nil
}

func (a *AgentAdapter) pendingApproval(ctx context.Context, session agent.Session, approvalID string) (agent.ToolApproval, error) {
	approval, err := a.agent.GetApproval(ctx, approvalID)
	if err != nil {
		return agent.ToolApproval{}, fmt.Errorf("load Discord approval: %w", err)
	}
	// GetApproval itself is not owner-filtered. Check the already-authorized
	// session before returning a preview or entering DecideApproval.
	if approval.SessionID != session.ID || approval.State != "pending" || session.State != "awaiting_approval" ||
		approval.TurnID != session.ActiveTurnID || approval.ArgumentsHash == "" {
		return agent.ToolApproval{}, fmt.Errorf("approval is not pending for this Discord task; refresh status")
	}
	return approval, nil
}

func (a *AgentAdapter) Decide(ctx context.Context, binding Binding, sessionID, approvalID, decision, actor string) (Reply, error) {
	ctx, session, err := a.boundSession(ctx, binding, sessionID)
	if err != nil {
		return Reply{}, err
	}
	if _, err = a.pendingApproval(ctx, session, approvalID); err != nil {
		return Reply{}, err
	}
	result, err := a.agent.DecideApproval(ctx, approvalID, agent.ApprovalDecisionInput{
		Actor: actor, Decision: decision, Reason: "Explicit Discord decision after reviewing the persisted exact-call preview",
	}, nil)
	if err != nil {
		return Reply{}, err
	}
	return replyFromTurn(result), nil
}

func approvalSummary(approval agent.ToolApproval) ApprovalSummary {
	return ApprovalSummary{ID: approval.ID, SessionID: approval.SessionID, ToolName: approval.ToolName,
		Summary: approval.Summary, Preview: approval.Preview, ArgumentsHash: approval.ArgumentsHash}
}

func replyFromTurn(result agent.TurnResult) Reply {
	reply := Reply{Text: result.AssistantEvent.Content}
	if result.Approval != nil {
		summary := approvalSummary(*result.Approval)
		reply.Approval = &summary
	}
	if strings.TrimSpace(reply.Text) == "" {
		if reply.Approval != nil {
			reply.Text = "This task is waiting for your approval. Review the exact tool request below."
		} else {
			reply.Text = "Turn finished: " + result.FinishReason
		}
	}
	return reply
}

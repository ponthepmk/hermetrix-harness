package agent

import (
	"time"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/providers"
	toolruntime "hermetrix-harness/internal/tools"
)

type Session struct {
	ID                 string          `json:"id"`
	Title              string          `json:"title"`
	ProviderID         string          `json:"provider_id"`
	ProviderName       string          `json:"provider_name,omitempty"`
	Model              string          `json:"model,omitempty"`
	ProjectID          string          `json:"project_id,omitempty"`
	ContextProfile     string          `json:"context_profile"`
	State              string          `json:"state"`
	ActiveTurnID       string          `json:"active_turn_id,omitempty"`
	Contract           SessionContract `json:"contract"`
	ContractRevision   string          `json:"contract_revision"`
	CacheEpoch         int             `json:"cache_epoch"`
	QualificationRunID string          `json:"qualification_run_id,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type CreateSessionInput struct {
	Title                 string                      `json:"title"`
	ProviderID            string                      `json:"provider_id"`
	ProjectID             string                      `json:"project_id,omitempty"`
	ContextProfile        string                      `json:"context_profile"`
	QualificationOverride *QualificationOverrideInput `json:"qualification_override,omitempty"`
}

type QualificationOverrideInput struct {
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
}

type QualificationBinding struct {
	Mode             string     `json:"mode"`
	RunID            string     `json:"run_id,omitempty"`
	ProviderRevision string     `json:"provider_revision"`
	ContextProfile   string     `json:"context_profile"`
	Actor            string     `json:"actor,omitempty"`
	Reason           string     `json:"reason,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
}

type SessionSkillBinding struct {
	SkillID       string `json:"skill_id"`
	VersionID     string `json:"version_id"`
	CanonicalName string `json:"canonical_name"`
	Summary       string `json:"summary"`
	Pinned        bool   `json:"pinned"`
}

type SessionContract struct {
	Revision           string                   `json:"revision"`
	ProviderRevision   string                   `json:"provider_revision"`
	ProviderID         string                   `json:"provider_id"`
	Model              string                   `json:"model"`
	ContextProfile     string                   `json:"context_profile"`
	ProjectID          string                   `json:"project_id,omitempty"`
	PolicyRevision     string                   `json:"policy_revision"`
	CapabilityRevision string                   `json:"capability_revision"`
	ToolBindings       []toolruntime.Definition `json:"tool_bindings"`
	SkillCatalog       []SessionSkillBinding    `json:"skill_catalog"`
	SelectedSkills     []SessionSkillBinding    `json:"selected_skills"`
	SkillsInitialized  bool                     `json:"skills_initialized"`
	Qualification      QualificationBinding     `json:"qualification"`
	CacheEpoch         int                      `json:"cache_epoch"`
	TaskBudget         TaskBudget               `json:"task_budget"`
	CreatedAt          time.Time                `json:"created_at"`
}

type TaskBudget struct {
	MaxModelSteps       int `json:"max_model_steps"`
	MaxToolCalls        int `json:"max_tool_calls"`
	MaxWallTimeSeconds  int `json:"max_wall_time_seconds"`
	MaxCumulativeTokens int `json:"max_cumulative_tokens"`
}

type Event struct {
	ID         string         `json:"id"`
	SessionID  string         `json:"session_id"`
	TurnID     string         `json:"turn_id"`
	Sequence   int            `json:"sequence"`
	EventKind  string         `json:"event_kind"`
	Role       string         `json:"role,omitempty"`
	Content    string         `json:"content,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	ProviderID string         `json:"provider_id,omitempty"`
	Model      string         `json:"model,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

type StepBinding struct {
	ID                      string                   `json:"id"`
	SessionID               string                   `json:"session_id"`
	TurnID                  string                   `json:"turn_id"`
	StepNumber              int                      `json:"step_number"`
	ProviderID              string                   `json:"provider_id"`
	Model                   string                   `json:"model"`
	ContextSnapshotID       string                   `json:"context_snapshot_id"`
	CapabilityRevision      string                   `json:"capability_revision"`
	PolicyRevision          string                   `json:"policy_revision"`
	SessionContractRevision string                   `json:"session_contract_revision"`
	CacheEpoch              int                      `json:"cache_epoch"`
	ToolBindings            []toolruntime.Definition `json:"tool_bindings"`
	CreatedAt               time.Time                `json:"created_at"`
}

type TurnInput struct {
	Content string `json:"content"`
}

type TurnResult struct {
	TurnID         string             `json:"turn_id"`
	AssistantEvent Event              `json:"assistant_event"`
	Binding        StepBinding        `json:"binding"`
	ContextReport  ctxcompiler.Report `json:"context_report"`
	Usage          providers.Usage    `json:"usage"`
	FinishReason   string             `json:"finish_reason"`
	Approval       *ToolApproval      `json:"approval,omitempty"`
}

type ToolApproval struct {
	ID             string         `json:"id"`
	SessionID      string         `json:"session_id"`
	TurnID         string         `json:"turn_id"`
	StepBindingID  string         `json:"step_binding_id"`
	StepNumber     int            `json:"step_number"`
	ToolCallID     string         `json:"tool_call_id"`
	ToolName       string         `json:"tool_name"`
	ToolRevision   string         `json:"tool_revision"`
	Effect         string         `json:"effect"`
	ArgumentsHash  string         `json:"arguments_hash"`
	Summary        string         `json:"summary"`
	Preview        string         `json:"preview"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	State          string         `json:"state"`
	RequestedAt    time.Time      `json:"requested_at"`
	DecidedAt      *time.Time     `json:"decided_at,omitempty"`
	DecidedBy      string         `json:"decided_by,omitempty"`
	DecisionReason string         `json:"decision_reason,omitempty"`
	ReceiptEventID string         `json:"receipt_event_id,omitempty"`
	ArgumentsJSON  string         `json:"-"`
}

type ApprovalDecisionInput struct {
	Actor    string `json:"actor"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type StreamEvent struct {
	Type     string              `json:"type"`
	TurnID   string              `json:"turn_id,omitempty"`
	Event    *Event              `json:"event,omitempty"`
	Binding  *StepBinding        `json:"binding,omitempty"`
	Delta    *providers.Delta    `json:"delta,omitempty"`
	Result   *TurnResult         `json:"result,omitempty"`
	Approval *ToolApproval       `json:"approval,omitempty"`
	Report   *ctxcompiler.Report `json:"context_report,omitempty"`
	Error    string              `json:"error,omitempty"`
}

type SessionDetail struct {
	Session   Session        `json:"session"`
	Events    []Event        `json:"events"`
	Approvals []ToolApproval `json:"approvals"`
}

// SkillRetrievalStats answers the question ADR-7 left open: when a session had
// a Skill that matched the work, did the model actually reach for it?
//
// Moving Skill bodies behind skill_search traded determinism for reach. The
// trade only pays if models call the tool. Smaller models are the ones most
// likely not to, and they are the ones Hermetrix targets, so the rate is
// reported per model rather than as one number.
type SkillRetrievalStats struct {
	Model string `json:"model"`
	// Turns counts every committed user turn for this model.
	Turns int `json:"turns"`
	// TurnsWithRelevantSkill counts turns where the session's frozen catalog
	// contained at least one Skill the deterministic scorer matched to the
	// turn's goal. This is the denominator: a turn with no matching Skill is
	// not evidence of anything when the model does not search.
	TurnsWithRelevantSkill int `json:"turns_with_relevant_skill"`
	// TurnsModelRequested counts those turns where the model called
	// skill_search or skill_view itself.
	TurnsModelRequested int `json:"turns_model_requested"`
	// TurnsPreloaded counts those turns already carrying a Skill body from the
	// contract floor. The floor is why a model that never searches is no worse
	// off than before ADR-7.
	TurnsPreloaded int `json:"turns_preloaded"`
	// NoSkillRequestedRate is 1 - requested/relevant. ADR-7 treats a rate above
	// 0.5 for a supported model tier as pull having failed for that tier, which
	// calls for widening the floor rather than pushing harder in the prompt.
	NoSkillRequestedRate float64 `json:"no_skill_requested_rate"`
	// Verdict is "insufficient_evidence" under the minimum sample, otherwise
	// "pull_working" or "pull_failing".
	Verdict string `json:"verdict"`
}

// SkillRetrievalMinimumTurns is the sample below which the rate is reported but
// carries no verdict. A single unlucky turn must not condemn a model tier.
const SkillRetrievalMinimumTurns = 20

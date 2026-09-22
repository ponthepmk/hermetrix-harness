package agentplatform

import (
	"encoding/json"
	"time"
)

const (
	ContractVersion        = "agent-platform/v1"
	TaskAssignmentRevision = 2
	RunUpdateRevision      = 3
	ACKRevision            = 1
)

type Signature struct {
	Alg   string `json:"alg"`
	KeyID string `json:"key_id"`
	Value string `json:"value"`
}

type ManagedEnvelope struct {
	IdempotencyKey  string          `json:"idempotency_key"`
	MessageType     string          `json:"message_type"`
	ContractVersion string          `json:"contract_version"`
	SchemaRevision  int             `json:"schema_revision"`
	SenderNodeID    string          `json:"sender_node_id"`
	Payload         json.RawMessage `json:"payload"`
	PayloadDigest   string          `json:"payload_digest"`
	Signature       Signature       `json:"signature"`
}

type AccessScope struct {
	Access       string   `json:"access"`
	Capabilities []string `json:"capabilities,omitempty"`
	RepositoryID string   `json:"repository_id"`
	WorktreeID   *string  `json:"worktree_id,omitempty"`
	PathPrefixes []string `json:"path_prefixes,omitempty"`
	GrantedBy    string   `json:"granted_by,omitempty"`
	GrantedAt    string   `json:"granted_at,omitempty"`
	ExpiresAt    *string  `json:"expires_at,omitempty"`
}

type ClaimIntegrity struct {
	Alg   string `json:"alg"`
	Value string `json:"value"`
}

type AssignmentAuthorizationClaim struct {
	AssignmentID        string         `json:"assignment_id"`
	AssignmentRevision  int            `json:"assignment_revision"`
	NodeID              string         `json:"node_id"`
	AgentID             string         `json:"agent_id"`
	PlatformRunID       string         `json:"platform_run_id"`
	RepositoryID        string         `json:"repository_id"`
	WorktreeID          *string        `json:"worktree_id,omitempty"`
	AccessScope         AccessScope    `json:"access_scope"`
	AuthorityScopeID    *string        `json:"authority_scope_id,omitempty"`
	AuthorityGeneration *int           `json:"authority_generation,omitempty"`
	IssuedAt            string         `json:"issued_at"`
	ExpiresAt           string         `json:"expires_at"`
	Integrity           ClaimIntegrity `json:"integrity"`
}

type AcceptanceCriterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type TaskAssignment struct {
	ContractVersion     string                       `json:"contract_version"`
	SchemaRevision      int                          `json:"schema_revision"`
	AssignmentID        string                       `json:"assignment_id"`
	AssignmentRevision  int                          `json:"assignment_revision"`
	PlatformSessionID   *string                      `json:"platform_session_id,omitempty"`
	PlatformRunID       string                       `json:"platform_run_id"`
	AgentID             string                       `json:"agent_id"`
	NodeID              string                       `json:"node_id"`
	RepositoryID        string                       `json:"repository_id"`
	WorktreeID          *string                      `json:"worktree_id,omitempty"`
	WorkspaceID         *string                      `json:"workspace_id,omitempty"`
	ProjectID           *string                      `json:"project_id,omitempty"`
	ServiceID           *string                      `json:"service_id,omitempty"`
	TaskID              string                       `json:"task_id"`
	Title               string                       `json:"title"`
	OriginalRequest     string                       `json:"original_request"`
	AcceptanceCriteria  []AcceptanceCriterion        `json:"acceptance_criteria"`
	Goal                string                       `json:"goal"`
	Constraints         []string                     `json:"constraints,omitempty"`
	Complexity          string                       `json:"complexity,omitempty"`
	Risk                string                       `json:"risk,omitempty"`
	KnowledgeContextRef *string                      `json:"knowledge_context_ref,omitempty"`
	AccessScope         AccessScope                  `json:"access_scope"`
	AuthorityScopeID    *string                      `json:"authority_scope_id,omitempty"`
	AuthorityGeneration *int                         `json:"authority_generation,omitempty"`
	AuthorizationClaim  AssignmentAuthorizationClaim `json:"authorization_claim"`
	IssuedAt            string                       `json:"issued_at"`
}

type EvidenceRef struct {
	EvidenceID    string         `json:"evidence_id"`
	Type          string         `json:"type"`
	OriginNodeID  string         `json:"origin_node_id"`
	RunID         *string        `json:"run_id,omitempty"`
	ContentDigest string         `json:"content_digest"`
	CreatedAt     string         `json:"created_at"`
	Locator       *string        `json:"locator,omitempty"`
	Provenance    map[string]any `json:"provenance,omitempty"`
	Retrieval     map[string]any `json:"retrieval,omitempty"`
}

type ProgressFacts struct {
	Phase          string   `json:"phase"`
	CompletedSteps int      `json:"completed_steps"`
	TotalSteps     int      `json:"total_steps"`
	CurrentStepID  *string  `json:"current_step_id"`
	AttemptCount   int      `json:"attempt_count"`
	FailedChecks   int      `json:"failed_checks"`
	BlockedReasons []string `json:"blocked_reasons"`
	PlanRevision   *int     `json:"plan_revision,omitempty"`
}

type RunUpdate struct {
	PlatformRunID       string        `json:"platform_run_id"`
	HarnessRunRef       *string       `json:"harness_run_ref,omitempty"`
	Sequence            int64         `json:"sequence"`
	AuthorityScopeID    *string       `json:"authority_scope_id,omitempty"`
	AuthorityGeneration *int          `json:"authority_generation,omitempty"`
	ExecutionStatus     string        `json:"execution_status"`
	Progress            ProgressFacts `json:"progress"`
	ProjectionRef       EvidenceRef   `json:"projection_ref"`
	CreatedAt           string        `json:"created_at"`
}

type ACK struct {
	PlatformRunID string `json:"platform_run_id"`
	AckedSequence int64  `json:"acked_seq"`
}

type DeliveryResult struct {
	Attempted int
	Acked     int64
	Pending   int
	Exhausted int
}

type AssignmentReceipt struct {
	PlatformID         string
	AssignmentID       string
	AssignmentRevision int
	PlatformTaskID     string
	PlatformRunID      string
	HarnessTaskID      string
	PayloadDigest      string
	Access             string
	State              string
	CreatedAt          time.Time
}

type Observation struct {
	DecisionActionID  string
	DecisionBackend   string
	DecisionLatencyMS int64
	EnvelopeBytes     []byte
	RunUpdate         RunUpdate
	ProjectionRef     EvidenceRef
}

type Trust struct {
	PlatformID string
	NodeID     string
	AgentID    string
	KeyID      string
	PublicKey  []byte
	PrivateKey []byte
	Now        func() time.Time
}

type InvocationCounters struct {
	DecisionSelection int64 `json:"decision_selection"`
	PlanEffect        int64 `json:"plan_effect"`
	DispatchEffect    int64 `json:"dispatch_effect"`
	BeginRun          int64 `json:"begin_run"`
	BeginStepAttempt  int64 `json:"begin_step_attempt"`
	Command           int64 `json:"command"`
	Provider          int64 `json:"provider"`
	Tool              int64 `json:"tool"`
	WorkspaceWrite    int64 `json:"workspace_write"`
	Planner           int64 `json:"planner"`
	AgentTurn         int64 `json:"agent_turn"`
}

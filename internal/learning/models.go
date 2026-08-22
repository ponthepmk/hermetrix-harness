package learning

import "time"

const (
	StateQueued    = "queued"
	StateRunning   = "running"
	StateCompleted = "completed"
	StateFailed    = "failed"
	StateCanceled  = "canceled"
)

type SuggestedSkill struct {
	CanonicalName string `json:"canonical_name"`
	ScopeKind     string `json:"scope_kind"`
	ScopeRef      string `json:"scope_ref"`
	Owner         string `json:"owner"`
	ChangeKind    string `json:"change_kind"`
	TargetSkillID string `json:"target_skill_id,omitempty"`
	BaseVersionID string `json:"base_version_id,omitempty"`
	Reason        string `json:"reason"`
	Markdown      string `json:"markdown"`
}

// Digest is deliberately structured and bounded. A review worker receives
// receipts and redacted facts rather than an entire warm transcript.
type Digest struct {
	GoalAndConstraints string          `json:"goal_and_constraints"`
	Outcome            string          `json:"outcome"`
	Decisions          []string        `json:"decisions"`
	ToolReceipts       []string        `json:"tool_receipts"`
	SkillActivations   []string        `json:"skill_activations"`
	UserCorrections    []string        `json:"user_corrections"`
	Artifacts          []string        `json:"artifacts"`
	Redactions         []string        `json:"redactions"`
	SuggestedSkill     *SuggestedSkill `json:"suggested_skill,omitempty"`
}

type EnqueueInput struct {
	SessionID   string `json:"session_id"`
	TurnID      string `json:"turn_id,omitempty"`
	JobID       string `json:"job_id"`
	MilestoneID string `json:"milestone_id"`
	TriggerKind string `json:"trigger_kind"`
	Digest      Digest `json:"digest"`
}

type Decision struct {
	Kind            string   `json:"kind"`
	Reason          string   `json:"reason"`
	ExpectedBenefit string   `json:"expected_benefit,omitempty"`
	Risks           []string `json:"risks,omitempty"`
}

type Job struct {
	ID               string     `json:"id"`
	IdempotencyKey   string     `json:"idempotency_key"`
	State            string     `json:"state"`
	TriggerKind      string     `json:"trigger_kind"`
	SessionID        string     `json:"session_id"`
	JobID            string     `json:"job_id"`
	Digest           Digest     `json:"digest"`
	ReviewerRevision string     `json:"reviewer_revision"`
	Decision         Decision   `json:"decision"`
	CandidateID      string     `json:"candidate_id,omitempty"`
	Attempts         int        `json:"attempts"`
	Error            string     `json:"error,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

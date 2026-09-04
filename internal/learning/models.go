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
	GoalAndConstraints string   `json:"goal_and_constraints"`
	Outcome            string   `json:"outcome"`
	Decisions          []string `json:"decisions"`
	ToolReceipts       []string `json:"tool_receipts"`
	// VerifiedBy cites the receipts that measured the outcome rather than
	// asserting it—a command that actually completed and exited zero. A turn
	// whose Outcome is "success" with an empty VerifiedBy succeeded because the
	// model said so, which is a weaker claim. References, rather than a boolean,
	// keep every verification claim traceable to evidence.
	VerifiedBy       []string        `json:"verified_by"`
	SkillActivations []string        `json:"skill_activations"`
	UserCorrections  []string        `json:"user_corrections"`
	Artifacts        []string        `json:"artifacts"`
	Redactions       []string        `json:"redactions"`
	SuggestedSkill   *SuggestedSkill `json:"suggested_skill,omitempty"`
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
	// SuggestedSkill is what the reviewer proposes writing down. Without it a
	// reviewer could decide "create" and have nowhere to put the procedure, so
	// the runner read the *digest's* suggestion instead and any reviewer that
	// wanted to propose something new was structurally unable to.
	//
	// It is a proposal and nothing more: the runner turns it into a candidate
	// that still passes every check and still needs a human to promote.
	SuggestedSkill *SuggestedSkill `json:"suggested_skill,omitempty"`
	// Unusable marks a decision the reviewer did not actually make: the model
	// returned nothing parseable, or a proposal missing the parts that make it
	// a proposal. Those fail closed to no_change, which is right for authority
	// and wrong for measurement -- a reviewer that could not answer looks
	// identical to one that considered the evidence and declined, and the two
	// call for completely different fixes.
	Unusable bool `json:"unusable,omitempty"`
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

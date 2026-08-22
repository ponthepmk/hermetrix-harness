package skills

import "time"

const (
	StateActive      = "active"
	StateStale       = "stale"
	StateArchived    = "archived"
	StateQuarantined = "quarantined"

	CandidateNeedsReview = "needs_review"
	CandidateQuarantined = "quarantined"
	CandidatePromoted    = "promoted"
	CandidateRejected    = "rejected"
)

type Skill struct {
	ID               string     `json:"id"`
	CanonicalName    string     `json:"canonical_name"`
	ScopeKind        string     `json:"scope_kind"`
	ScopeRef         string     `json:"scope_ref"`
	Origin           string     `json:"origin"`
	Owner            string     `json:"owner"`
	State            string     `json:"state"`
	CurrentVersionID string     `json:"current_version_id"`
	Enabled          bool       `json:"enabled"`
	Pinned           bool       `json:"pinned"`
	Protected        bool       `json:"protected"`
	AbsorbedIntoID   string     `json:"absorbed_into_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	Summary          string     `json:"summary"`
	SelectedCount    int        `json:"selected_count"`
	InjectedCount    int        `json:"injected_count"`
	SuccessCount     int        `json:"success_count"`
	FailureCount     int        `json:"failure_count"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty"`
}

type Version struct {
	ID              string    `json:"id"`
	SkillID         string    `json:"skill_id"`
	ParentVersionID string    `json:"parent_version_id,omitempty"`
	ContentHash     string    `json:"content_hash"`
	PackageBlobRef  string    `json:"package_blob_ref"`
	Manifest        Manifest  `json:"manifest"`
	AuthorActor     string    `json:"author_actor"`
	ChangeMessage   string    `json:"change_message"`
	CreatedAt       time.Time `json:"created_at"`
	Markdown        string    `json:"markdown,omitempty"`
	SupportingFiles []string  `json:"supporting_files,omitempty"`
}

type Candidate struct {
	ID               string    `json:"id"`
	CanonicalName    string    `json:"canonical_name"`
	ScopeKind        string    `json:"scope_kind"`
	ScopeRef         string    `json:"scope_ref"`
	Origin           string    `json:"origin"`
	Owner            string    `json:"owner"`
	ChangeKind       string    `json:"change_kind"`
	TargetSkillID    string    `json:"target_skill_id,omitempty"`
	BaseVersionID    string    `json:"base_version_id,omitempty"`
	CandidateBlobRef string    `json:"candidate_blob_ref"`
	CandidateHash    string    `json:"candidate_hash"`
	CreatedBy        string    `json:"created_by"`
	TriggerKind      string    `json:"trigger_kind"`
	Reason           string    `json:"reason"`
	EvidenceRefs     []string  `json:"evidence_refs"`
	State            string    `json:"state"`
	Checks           CheckSet  `json:"checks"`
	Revision         int       `json:"revision"`
	ReviewedBy       string    `json:"reviewed_by,omitempty"`
	ReviewReason     string    `json:"review_reason,omitempty"`
	SourceReviewID   string    `json:"source_review_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Markdown         string    `json:"markdown,omitempty"`
}

type CheckFinding struct {
	Level   string `json:"level"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type CheckSet struct {
	Passed          bool           `json:"passed"`
	LintPassed      bool           `json:"lint_passed"`
	SecurityPassed  bool           `json:"security_passed"`
	ReplayRequired  bool           `json:"replay_required"`
	ReplayPassed    bool           `json:"replay_passed"`
	TokenEstimate   int            `json:"token_estimate"`
	CapabilityHints []string       `json:"capability_hints"`
	Findings        []CheckFinding `json:"findings"`
	CheckerVersion  string         `json:"checker_version"`
}

type Archive struct {
	ID                  string     `json:"id"`
	SkillID             string     `json:"skill_id"`
	SkillName           string     `json:"skill_name"`
	ArchivedVersionID   string     `json:"archived_version_id"`
	PackageBlobRef      string     `json:"package_blob_ref"`
	PreviousState       string     `json:"previous_state"`
	PreviousEnabled     bool       `json:"previous_enabled"`
	PreviousPinned      bool       `json:"previous_pinned"`
	Reason              string     `json:"reason"`
	ActorKind           string     `json:"actor_kind"`
	AbsorbedIntoID      string     `json:"absorbed_into_id,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	RestoredCandidateID string     `json:"restored_candidate_id,omitempty"`
	RestoredAt          *time.Time `json:"restored_at,omitempty"`
}

type ActivationInput struct {
	SessionID         string   `json:"session_id"`
	TurnID            string   `json:"turn_id"`
	JobID             string   `json:"job_id"`
	SkillID           string   `json:"skill_id"`
	VersionID         string   `json:"version_id"`
	SelectionSource   string   `json:"selection_source"`
	SelectionReason   string   `json:"selection_reason"`
	MetadataExposed   bool     `json:"metadata_exposed"`
	BodyInjected      bool     `json:"body_injected"`
	RelevantToolCalls []string `json:"relevant_tool_calls"`
	Outcome           string   `json:"outcome"`
	OutcomeSource     string   `json:"outcome_source"`
	AttributionKind   string   `json:"attribution_kind"`
	AttributionScore  *float64 `json:"attribution_score,omitempty"`
}

type CreateCandidateInput struct {
	CanonicalName  string   `json:"canonical_name"`
	ScopeKind      string   `json:"scope_kind"`
	ScopeRef       string   `json:"scope_ref"`
	Origin         string   `json:"origin"`
	Owner          string   `json:"owner"`
	ChangeKind     string   `json:"change_kind"`
	TargetSkillID  string   `json:"target_skill_id"`
	BaseVersionID  string   `json:"base_version_id"`
	CreatedBy      string   `json:"created_by"`
	TriggerKind    string   `json:"trigger_kind"`
	Reason         string   `json:"reason"`
	EvidenceRefs   []string `json:"evidence_refs"`
	Markdown       string   `json:"markdown"`
	Files          []File   `json:"files"`
	SourceReviewID string   `json:"-"`
}

type UpdateCandidateInput struct {
	Markdown         string `json:"markdown"`
	Actor            string `json:"actor"`
	ExpectedRevision int    `json:"expected_revision"`
}

type UpdateSkillControlsInput struct {
	Actor             string `json:"actor"`
	ExpectedVersionID string `json:"expected_version_id"`
	Enabled           *bool  `json:"enabled,omitempty"`
	Pinned            *bool  `json:"pinned,omitempty"`
}

type Relation struct {
	ID              string         `json:"id"`
	LeftSkillID     string         `json:"left_skill_id"`
	LeftVersionID   string         `json:"left_version_id"`
	LeftName        string         `json:"left_name"`
	RightSkillID    string         `json:"right_skill_id"`
	RightVersionID  string         `json:"right_version_id"`
	RightName       string         `json:"right_name"`
	Kind            string         `json:"relation_kind"`
	Score           float64        `json:"score"`
	Evidence        map[string]any `json:"evidence"`
	AnalyzerKind    string         `json:"analyzer_kind"`
	AnalyzerVersion string         `json:"analyzer_version"`
	Status          string         `json:"status"`
	CreatedAt       time.Time      `json:"created_at"`
}

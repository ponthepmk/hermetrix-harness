// Package taskengine persists the authoritative requirement, plan, execution
// and validation state for work that may outlive one model turn or process.
package taskengine

import "time"

const (
	StateDraft           = "draft"
	StateReady           = "ready"
	StateRunning         = "running"
	StateWaitingForInput = "waiting_for_input"
	StatePaused          = "paused"
	StateVerifying       = "verifying"
	StateCompleted       = "completed"
	StateFailed          = "failed"
	StateCancelled       = "cancelled"

	StepPending   = "pending"
	StepRunning   = "running"
	StepCompleted = "completed"
	StepBlocked   = "blocked"
	StepFailed    = "failed"
	StepSkipped   = "skipped"

	ValidationPass    = "pass"
	ValidationFail    = "fail"
	ValidationBlocked = "blocked"
	ValidationUnknown = "unknown"
)

type Criterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// GoalContract records why the active criteria apply and which human decision
// is required after objective evidence has passed. It belongs to a requirement
// revision, so changing it invalidates the active plan and prior acceptance.
type GoalContract struct {
	Outcome               string              `json:"outcome,omitempty"`
	Scope                 string              `json:"scope,omitempty"`
	WritablePaths         []string            `json:"writable_paths,omitempty"`
	Assumptions           []string            `json:"assumptions,omitempty"`
	AffectedJourneys      []string            `json:"affected_journeys,omitempty"`
	ExperiencePreview     string              `json:"experience_preview,omitempty"`
	Clarifications        []GoalClarification `json:"clarifications,omitempty"`
	Verification          string              `json:"verification,omitempty"`
	ReviewerPolicy        string              `json:"reviewer_policy"`
	RequirePlanReview     bool                `json:"require_plan_review,omitempty"`
	RequireUserAcceptance bool                `json:"require_user_acceptance,omitempty"`
}

type GoalClarification struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type RequirementRevision struct {
	ID           string       `json:"id"`
	TaskID       string       `json:"task_id"`
	Revision     int          `json:"revision"`
	Constraints  []string     `json:"constraints"`
	Unknowns     []string     `json:"unknowns"`
	Criteria     []Criterion  `json:"criteria"`
	Contract     GoalContract `json:"contract"`
	SupersedesID string       `json:"supersedes_id,omitempty"`
	Actor        string       `json:"actor"`
	CreatedAt    time.Time    `json:"created_at"`
}

type StepSpec struct {
	OutputPaths    []string `json:"output_paths,omitempty"`
	Key            string   `json:"key"`
	Title          string   `json:"title"`
	Instructions   string   `json:"instructions"`
	RequirementIDs []string `json:"requirement_ids,omitempty"`
	Dependencies   []string `json:"dependencies,omitempty"`
	Checks         []string `json:"checks,omitempty"`
	EffectScope    []string `json:"effect_scope,omitempty"`
}

type Step struct {
	ID           string `json:"id"`
	TaskID       string `json:"task_id"`
	PlanRevision int    `json:"plan_revision"`
	StepSpec
	State     string    `json:"state"`
	Revision  int       `json:"revision"`
	SortOrder int       `json:"sort_order"`
	LastError string    `json:"last_error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PlanRevision struct {
	ID                  string    `json:"id"`
	TaskID              string    `json:"task_id"`
	Revision            int       `json:"revision"`
	RequirementRevision int       `json:"requirement_revision"`
	Reason              string    `json:"reason"`
	Actor               string    `json:"actor"`
	Steps               []Step    `json:"steps"`
	ReviewStatus        string    `json:"review_status"`
	ReviewReason        string    `json:"review_reason,omitempty"`
	SubjectHash         string    `json:"subject_hash"`
	CreatedAt           time.Time `json:"created_at"`
}

type Task struct {
	ID                        string              `json:"id"`
	ProjectID                 string              `json:"project_id,omitempty"`
	Title                     string              `json:"title"`
	Objective                 string              `json:"objective"`
	OriginalRequest           string              `json:"original_request"`
	State                     string              `json:"state"`
	ActiveRequirementRevision int                 `json:"active_requirement_revision"`
	ActivePlanRevision        int                 `json:"active_plan_revision"`
	Revision                  int                 `json:"revision"`
	PauseReason               string              `json:"pause_reason,omitempty"`
	Requirement               RequirementRevision `json:"requirement"`
	Plan                      PlanRevision        `json:"plan"`
	CreatedAt                 time.Time           `json:"created_at"`
	UpdatedAt                 time.Time           `json:"updated_at"`
	OwnerPrincipalID          string              `json:"owner_principal_id"`
	Visibility                string              `json:"visibility"`
	ExportPolicy              string              `json:"export_policy"`
	SharingRevision           int                 `json:"sharing_revision"`
	EgressPolicy              string              `json:"egress_policy"`
}

type Checkpoint struct {
	ID                  string    `json:"id"`
	TaskID              string    `json:"task_id"`
	PlanRevision        int       `json:"plan_revision"`
	TaskRevision        int       `json:"task_revision"`
	CompletedSteps      []string  `json:"completed_steps"`
	PendingSteps        []string  `json:"pending_steps"`
	EvidenceRefs        []string  `json:"evidence_refs"`
	UnresolvedEffects   []string  `json:"unresolved_effects"`
	NextAction          string    `json:"next_action"`
	ResumePrerequisites []string  `json:"resume_prerequisites"`
	Reason              string    `json:"reason,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

type Validation struct {
	ID              string    `json:"id"`
	TaskID          string    `json:"task_id"`
	StepID          string    `json:"step_id,omitempty"`
	RequirementID   string    `json:"requirement_id,omitempty"`
	CheckID         string    `json:"check_id"`
	SubjectRevision string    `json:"subject_revision"`
	Status          string    `json:"status"`
	Expected        string    `json:"expected,omitempty"`
	Actual          string    `json:"actual,omitempty"`
	EvidenceRefs    []string  `json:"evidence_refs"`
	Severity        string    `json:"severity,omitempty"`
	Confidence      float64   `json:"confidence"`
	CreatedAt       time.Time `json:"created_at"`
}

type CreateTaskInput struct {
	ProjectID            string              `json:"project_id,omitempty"`
	Title                string              `json:"title"`
	Objective            string              `json:"objective"`
	OriginalRequest      string              `json:"original_request"`
	Constraints          []string            `json:"constraints,omitempty"`
	Unknowns             []string            `json:"unknowns,omitempty"`
	Criteria             []Criterion         `json:"criteria"`
	Contract             GoalContract        `json:"contract,omitempty"`
	Actor                string              `json:"actor"`
	EgressPolicy         string              `json:"egress_policy,omitempty"`
	RemoteEgressApproval *TaskEgressApproval `json:"remote_egress_approval,omitempty"`
}

type TaskEgressApproval struct {
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
}

type CreatePlanInput struct {
	TaskID               string     `json:"task_id"`
	ExpectedTaskRevision int        `json:"expected_task_revision"`
	RequirementRevision  int        `json:"requirement_revision"`
	Reason               string     `json:"reason"`
	Actor                string     `json:"actor"`
	Steps                []StepSpec `json:"steps"`
}

type ReviseRequirementsInput struct {
	TaskID               string        `json:"task_id"`
	ExpectedTaskRevision int           `json:"expected_task_revision"`
	Constraints          []string      `json:"constraints,omitempty"`
	Unknowns             []string      `json:"unknowns,omitempty"`
	Criteria             []Criterion   `json:"criteria"`
	Contract             *GoalContract `json:"contract,omitempty"`
	Actor                string        `json:"actor"`
}

type CheckpointInput struct {
	TaskID               string   `json:"task_id"`
	ExpectedTaskRevision int      `json:"expected_task_revision"`
	EvidenceRefs         []string `json:"evidence_refs,omitempty"`
	UnresolvedEffects    []string `json:"unresolved_effects,omitempty"`
	NextAction           string   `json:"next_action"`
	ResumePrerequisites  []string `json:"resume_prerequisites,omitempty"`
	Reason               string   `json:"reason,omitempty"`
}

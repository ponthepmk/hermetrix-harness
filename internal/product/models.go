package product

import "time"

type Project struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RootPath string `json:"root_path"`
	State    string `json:"state"`
	// Pinned and LastOpenedAt exist so the picker can order itself from data
	// rather than guessing which project someone wants.
	Pinned       bool       `json:"pinned"`
	LastOpenedAt *time.Time `json:"last_opened_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type ProjectInput struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	RootPath string `json:"root_path"`
}

type FileEntry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Directory  bool      `json:"directory"`
	Symlink    bool      `json:"symlink"`
	Bytes      int64     `json:"bytes"`
	ModifiedAt time.Time `json:"modified_at"`
}

type FileDocument struct {
	Path       string    `json:"path"`
	Content    string    `json:"content"`
	SHA256     string    `json:"sha256"`
	Bytes      int       `json:"bytes"`
	Mode       string    `json:"mode"`
	ModifiedAt time.Time `json:"modified_at"`
}

type WriteFileInput struct {
	Path           string `json:"path"`
	Content        string `json:"content"`
	ExpectedSHA256 string `json:"expected_sha256"`
	Actor          string `json:"actor"`
}

type WriteFileResult struct {
	Document        FileDocument `json:"document"`
	BeforeSHA256    string       `json:"before_sha256,omitempty"`
	Diff            string       `json:"diff"`
	ReceiptArtifact Artifact     `json:"receipt_artifact"`
}

type TerminalSession struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"project_id"`
	Shell       string     `json:"shell"`
	WorkingDir  string     `json:"working_dir"`
	State       string     `json:"state"`
	Cursor      int64      `json:"cursor"`
	ExitCode    *int       `json:"exit_code,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type StartTerminalInput struct {
	ProjectID  string `json:"project_id"`
	Shell      string `json:"shell"`
	WorkingDir string `json:"working_dir"`
	Actor      string `json:"actor"`
	Columns    uint16 `json:"columns"`
	Rows       uint16 `json:"rows"`
}

type TerminalOutput struct {
	ID        string `json:"id"`
	Output    string `json:"output"`
	Cursor    int64  `json:"cursor"`
	Truncated bool   `json:"truncated"`
	State     string `json:"state"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Error     string `json:"error,omitempty"`
}

type BrowserLink struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

type BrowserElement struct {
	Ref         int    `json:"ref"`
	Tag         string `json:"tag"`
	Role        string `json:"role,omitempty"`
	Text        string `json:"text,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Selector    string `json:"selector"`
}

type BrowserTab struct {
	ID                   string           `json:"id"`
	ProjectID            string           `json:"project_id,omitempty"`
	URL                  string           `json:"url"`
	Title                string           `json:"title"`
	State                string           `json:"state"`
	AllowPrivate         bool             `json:"allow_private"`
	TextSnapshot         string           `json:"text_snapshot,omitempty"`
	Links                []BrowserLink    `json:"links"`
	Elements             []BrowserElement `json:"elements"`
	ScreenshotArtifactID string           `json:"screenshot_artifact_id,omitempty"`
	Error                string           `json:"error,omitempty"`
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
}

type OpenBrowserTabInput struct {
	ProjectID    string `json:"project_id,omitempty"`
	URL          string `json:"url"`
	AllowPrivate bool   `json:"allow_private"`
	Actor        string `json:"actor"`
}

type BrowserActionInput struct {
	Action string `json:"action"`
	URL    string `json:"url,omitempty"`
	Ref    int    `json:"ref,omitempty"`
	Text   string `json:"text,omitempty"`
	Actor  string `json:"actor"`
}

type Artifact struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`
	MIMEType  string         `json:"mime_type"`
	BlobRef   string         `json:"blob_ref"`
	ByteSize  int            `json:"byte_size"`
	Checksum  string         `json:"checksum"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type ArtifactInput struct {
	ProjectID string         `json:"project_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`
	MIMEType  string         `json:"mime_type"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type DeliverableSlide struct {
	Title   string   `json:"title"`
	Bullets []string `json:"bullets"`
}

type DeliverableInput struct {
	ProjectID  string             `json:"project_id,omitempty"`
	SessionID  string             `json:"session_id,omitempty"`
	Format     string             `json:"format"`
	Title      string             `json:"title"`
	Actor      string             `json:"actor"`
	Paragraphs []string           `json:"paragraphs,omitempty"`
	Rows       [][]string         `json:"rows,omitempty"`
	Slides     []DeliverableSlide `json:"slides,omitempty"`
}

type TeamMember struct {
	ID           string    `json:"id"`
	TeamID       string    `json:"team_id"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	Instructions string    `json:"instructions"`
	IsLead       bool      `json:"is_lead"`
	SortOrder    int       `json:"sort_order"`
	CreatedAt    time.Time `json:"created_at"`
}

type AgentTeam struct {
	ID           string       `json:"id"`
	ProjectID    string       `json:"project_id,omitempty"`
	Name         string       `json:"name"`
	Instructions string       `json:"instructions"`
	State        string       `json:"state"`
	Revision     int          `json:"revision"`
	Members      []TeamMember `json:"members"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type TeamMemberInput struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	Instructions string `json:"instructions"`
	IsLead       bool   `json:"is_lead"`
}

type AgentTeamInput struct {
	ID               string            `json:"id,omitempty"`
	ExpectedRevision int               `json:"expected_revision,omitempty"`
	ProjectID        string            `json:"project_id,omitempty"`
	Name             string            `json:"name"`
	Instructions     string            `json:"instructions"`
	Actor            string            `json:"actor"`
	Members          []TeamMemberInput `json:"members"`
}

type TeamTask struct {
	ID                 string     `json:"id"`
	RunID              string     `json:"run_id"`
	MemberID           string     `json:"member_id"`
	MemberName         string     `json:"member_name"`
	MemberRole         string     `json:"member_role"`
	MemberInstructions string     `json:"member_instructions,omitempty"`
	Title              string     `json:"title"`
	Prompt             string     `json:"prompt"`
	DependsOn          []string   `json:"depends_on"`
	State              string     `json:"state"`
	SessionID          string     `json:"session_id,omitempty"`
	ApprovalID         string     `json:"approval_id,omitempty"`
	ApprovalSummary    string     `json:"approval_summary,omitempty"`
	ApprovalPreview    string     `json:"approval_preview,omitempty"`
	ApprovalEffect     string     `json:"approval_effect,omitempty"`
	Result             string     `json:"result,omitempty"`
	Error              string     `json:"error,omitempty"`
	PromptTokens       int        `json:"prompt_tokens"`
	CompletionTokens   int        `json:"completion_tokens"`
	CreatedAt          time.Time  `json:"created_at"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
}

type TeamRun struct {
	ID                  string     `json:"id"`
	TeamID              string     `json:"team_id"`
	TeamName            string     `json:"team_name"`
	TeamInstructions    string     `json:"team_instructions,omitempty"`
	ProjectID           string     `json:"project_id,omitempty"`
	Objective           string     `json:"objective"`
	ProviderID          string     `json:"provider_id"`
	ContextProfile      string     `json:"context_profile"`
	State               string     `json:"state"`
	MaxParallel         int        `json:"max_parallel"`
	Actor               string     `json:"actor"`
	QualificationReason string     `json:"qualification_reason,omitempty"`
	Error               string     `json:"error,omitempty"`
	PromptTokens        int        `json:"prompt_tokens"`
	CompletionTokens    int        `json:"completion_tokens"`
	Tasks               []TeamTask `json:"tasks"`
	CreatedAt           time.Time  `json:"created_at"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
}

type TeamTaskInput struct {
	ID        string   `json:"id,omitempty"`
	MemberID  string   `json:"member_id"`
	Title     string   `json:"title"`
	Prompt    string   `json:"prompt"`
	DependsOn []string `json:"depends_on,omitempty"`
}

type StartTeamRunInput struct {
	TeamID              string          `json:"team_id"`
	ProjectID           string          `json:"project_id,omitempty"`
	Objective           string          `json:"objective"`
	ProviderID          string          `json:"provider_id"`
	ContextProfile      string          `json:"context_profile"`
	Actor               string          `json:"actor"`
	MaxParallel         int             `json:"max_parallel"`
	QualificationReason string          `json:"qualification_reason,omitempty"`
	Tasks               []TeamTaskInput `json:"tasks,omitempty"`
}

type TeamApprovalDecisionInput struct {
	Actor    string `json:"actor"`
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

type Job struct {
	ID              string         `json:"id"`
	Kind            string         `json:"kind"`
	State           string         `json:"state"`
	Progress        float64        `json:"progress"`
	Payload         map[string]any `json:"payload"`
	Result          map[string]any `json:"result"`
	Error           string         `json:"error,omitempty"`
	CancelRequested bool           `json:"cancel_requested"`
	CreatedAt       time.Time      `json:"created_at"`
	StartedAt       *time.Time     `json:"started_at,omitempty"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
}

type CommandInput struct {
	ProjectID      string   `json:"project_id"`
	Actor          string   `json:"actor"`
	Executable     string   `json:"executable"`
	Arguments      []string `json:"arguments"`
	WorkingDir     string   `json:"working_dir"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type Setting struct {
	Key       string    `json:"key"`
	Value     any       `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Memory struct {
	ID         string    `json:"id"`
	ScopeKind  string    `json:"scope_kind"`
	ScopeRef   string    `json:"scope_ref"`
	MemoryKind string    `json:"memory_kind"`
	Content    string    `json:"content"`
	Source     string    `json:"source"`
	State      string    `json:"state"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type MemoryInput struct {
	ScopeKind  string `json:"scope_kind"`
	ScopeRef   string `json:"scope_ref"`
	MemoryKind string `json:"memory_kind"`
	Content    string `json:"content"`
	Source     string `json:"source"`
}

type UsageSummary struct {
	Sessions         int `json:"sessions"`
	ModelSteps       int `json:"model_steps"`
	ToolCalls        int `json:"tool_calls"`
	ToolSucceeded    int `json:"tool_succeeded"`
	ToolFailed       int `json:"tool_failed"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type BackupRun struct {
	ID              string         `json:"id"`
	Kind            string         `json:"kind"`
	State           string         `json:"state"`
	FormatVersion   int            `json:"format_version"`
	ManifestBlobRef string         `json:"manifest_blob_ref,omitempty"`
	Checksum        string         `json:"checksum,omitempty"`
	Counts          map[string]int `json:"counts"`
	Error           string         `json:"error,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
}

type ImportPreview struct {
	BackupRun
	SkillConflicts int `json:"skill_conflicts"`
	BlobCount      int `json:"blob_count"`
	BlobBytes      int `json:"blob_bytes"`
}

type ImportResult struct {
	BackupRun
	CandidateIDs []string `json:"candidate_ids"`
	Conflicts    int      `json:"conflicts"`
	// NotRestored names the tables the file carried that this import did not
	// read, with the row count each held. Export writes the whole database;
	// import reads Skills and turns them into candidates. Those are not
	// inverses, and a result that says only "imported" reads like they are.
	//
	// A user restoring what they were told is a backup should not have to
	// discover from an empty session list that their conversations were in the
	// file and were dropped on the floor.
	NotRestored map[string]int `json:"not_restored,omitempty"`
}

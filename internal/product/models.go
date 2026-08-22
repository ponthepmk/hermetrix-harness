package product

import "time"

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	RootPath  string    `json:"root_path"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
	Document       FileDocument `json:"document"`
	BeforeSHA256   string       `json:"before_sha256,omitempty"`
	Diff           string       `json:"diff"`
	ReceiptArtifact Artifact    `json:"receipt_artifact"`
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
}

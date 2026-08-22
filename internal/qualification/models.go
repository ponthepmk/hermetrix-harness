package qualification

import (
	"time"

	"hermetrix-harness/internal/localmodel"
)

type Input struct {
	ProviderID       string                   `json:"provider_id"`
	RuntimeProbe     *localmodel.ProbeRequest `json:"runtime_probe,omitempty"`
	RequestedProfile string                   `json:"requested_profile"`
}

type Check struct {
	Name        string         `json:"name"`
	State       string         `json:"state"`
	LatencyMS   int64          `json:"latency_ms,omitempty"`
	Evidence    map[string]any `json:"evidence,omitempty"`
	Remediation string         `json:"remediation,omitempty"`
}

type Results struct {
	Checks                   []Check `json:"checks"`
	Connectivity             bool    `json:"connectivity"`
	RuntimeAllocation        bool    `json:"runtime_allocation"`
	LongContextRecall        bool    `json:"long_context_recall"`
	UsageCalibration         bool    `json:"usage_calibration"`
	NativeToolCall           bool    `json:"native_tool_call"`
	SequentialToolCalls      bool    `json:"sequential_tool_calls"`
	MalformedRecovery        bool    `json:"malformed_recovery"`
	DeferredToolCall         bool    `json:"deferred_tool_call"`
	ThaiEnglishSchema        bool    `json:"thai_english_schema"`
	Cancellation             bool    `json:"cancellation"`
	ForegroundPreemption     bool    `json:"foreground_preemption"`
	TTFTMilliseconds         int64   `json:"ttft_milliseconds"`
	TotalLatencyMilliseconds int64   `json:"total_latency_milliseconds"`
	TokensPerSecond          float64 `json:"tokens_per_second"`
	UsageRatio               float64 `json:"usage_ratio"`
	RecallProbedTokens       int     `json:"recall_probed_tokens"`
}

type Run struct {
	ID               string     `json:"id"`
	ProviderID       string     `json:"provider_id"`
	ProviderName     string     `json:"provider_name"`
	RuntimeKind      string     `json:"runtime_kind,omitempty"`
	RuntimeEndpoint  string     `json:"runtime_endpoint,omitempty"`
	Model            string     `json:"model"`
	SuiteRevision    string     `json:"suite_revision"`
	ProviderRevision string     `json:"provider_revision"`
	State            string     `json:"state"`
	DeclaredContext  int        `json:"declared_context"`
	AllocatedContext int        `json:"allocated_context"`
	ContextTier      string     `json:"context_tier"`
	CapabilityGrade  string     `json:"capability_grade"`
	RequestedProfile string     `json:"requested_profile"`
	Eligible         bool       `json:"eligible"`
	RequiresDecision bool       `json:"requires_decision"`
	Results          Results    `json:"results"`
	Remediation      []string   `json:"remediation"`
	Error            string     `json:"error,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

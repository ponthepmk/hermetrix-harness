package providers

import (
	"context"
	"fmt"
	"time"
)

const (
	AdapterOpenAICompatible = "openai-compatible"
	AdapterAnthropicNative  = "anthropic-native"
	AdapterGeminiNative     = "gemini-native"
)

// Adapter is the transport boundary for one provider protocol. Profiles keep
// model/context/credential policy; adapters only translate the frozen common
// chat and tool contract to a wire protocol.
type Adapter interface {
	StreamChat(context.Context, Profile, string, ChatRequest, func(Delta) error) (Completion, error)
}

type Profile struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	AdapterKind     string `json:"adapter_kind"`
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	APIKeyEnv       string `json:"api_key_env,omitempty"`
	CredentialReady bool   `json:"credential_ready"`
	// CredentialStored says a token was saved through the control center, as
	// opposed to coming from the environment or not being needed. It never
	// carries the token itself.
	CredentialStored     bool   `json:"credential_stored"`
	ContextWindow        int    `json:"context_window"`
	ContextEvidence      string `json:"context_evidence"`
	MaxOutputTokens      int    `json:"max_output_tokens"`
	ResourceGroup        string `json:"resource_group,omitempty"`
	RuntimeFingerprintID string `json:"runtime_fingerprint_id,omitempty"`
	// ReasoningRatio is the share of completion tokens this model spends
	// thinking, measured from its own responses. Reasoning bills as completion,
	// so a model at 0.8 leaves a fifth of the output budget for the answer --
	// and at a small profile that can round down to nothing. Zero means unknown.
	ReasoningRatio  float64 `json:"reasoning_ratio"`
	ReasoningSample int     `json:"reasoning_sample"`
	// TokenMultiplier scales the heuristic token estimate for this model's
	// tokenizer. It is per profile because the quantity is a tokenizer: one
	// number shared across models describes none of them.
	TokenMultiplier float64 `json:"token_multiplier"`
	TokenSample     int     `json:"token_sample"`
	// NonASCIIRate is tokens per character for scripts the ASCII rules do not
	// cover. Learned rather than assumed: the value that fits one model and one
	// language is not a fact about tokenizers.
	NonASCIIRate   float64 `json:"nonascii_rate"`
	NonASCIISample int     `json:"nonascii_sample"`
	// MessageOverhead and RequestOverhead are the chat template's cost: what
	// each message is wrapped in, and what the request carries besides its
	// messages. Both are billed and neither is context. They are measured by
	// probe rather than learned from traffic -- two requests with known
	// message counts pin them exactly, while production traffic grows message
	// count, size and language mix together and cannot separate them.
	MessageOverhead    int       `json:"token_message_overhead"`
	RequestOverhead    int       `json:"token_request_overhead"`
	OverheadMeasuredAt string    `json:"token_overhead_measured_at,omitempty"`
	Enabled            bool      `json:"enabled"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	Revision           string    `json:"revision"`
}

type SaveInput struct {
	ID              string `json:"id,omitempty"`
	Name            string `json:"name"`
	AdapterKind     string `json:"adapter_kind"`
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	APIKeyEnv       string `json:"api_key_env,omitempty"`
	ContextWindow   int    `json:"context_window"`
	ContextEvidence string `json:"context_evidence,omitempty"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
	ResourceGroup   string `json:"resource_group,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty"`
}

type Message struct {
	Role       string            `json:"role"`
	Content    string            `json:"content,omitempty"`
	Parts      []ContentPart     `json:"parts,omitempty"`
	ToolCalls  []MessageToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

type ContentPart struct {
	Kind       string `json:"kind"`
	Text       string `json:"text,omitempty"`
	MediaType  string `json:"media_type,omitempty"`
	Data       []byte `json:"-"`
	ArtifactID string `json:"artifact_id,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
}

type MessageToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function ToolCallInvocation `json:"function"`
}

type ToolCallInvocation struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolDefinition struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ChatRequest struct {
	Messages    []Message        `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	// Reasoning is a role contract, translated only by adapters that have
	// positively identified support for a bounded reasoning transport.
	Reasoning *ReasoningControl `json:"-"`
}

type ReasoningControl struct {
	Mode     string
	TokenCap int
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ToolCall struct {
	Index     int    `json:"index"`
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Delta struct {
	Content   string     `json:"content,omitempty"`
	Reasoning string     `json:"reasoning,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type Completion struct {
	Content      string     `json:"content"`
	Reasoning    string     `json:"reasoning,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason"`
	Usage        Usage      `json:"usage"`
}

// IncompleteCompletionError carries bounded partial evidence without allowing
// callers to treat it as a completed assistant response or dispatch its tools.
type IncompleteCompletionError struct {
	Reason  string
	Partial Completion
}

func (e *IncompleteCompletionError) Error() string {
	return fmt.Sprintf("provider stream incomplete: %s", e.Reason)
}

type TestResult struct {
	ProviderID   string `json:"provider_id"`
	Model        string `json:"model"`
	LatencyMS    int64  `json:"latency_ms"`
	Sample       string `json:"sample"`
	FinishReason string `json:"finish_reason"`
	Usage        Usage  `json:"usage"`
}

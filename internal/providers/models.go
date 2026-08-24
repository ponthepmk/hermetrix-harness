package providers

import "time"

const AdapterOpenAICompatible = "openai-compatible"

type Profile struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	AdapterKind     string `json:"adapter_kind"`
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	APIKeyEnv       string `json:"api_key_env,omitempty"`
	CredentialReady bool   `json:"credential_ready"`
	ContextWindow   int    `json:"context_window"`
	ContextEvidence string `json:"context_evidence"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	// ReasoningRatio is the share of completion tokens this model spends
	// thinking, measured from its own responses. Reasoning bills as completion,
	// so a model at 0.8 leaves a fifth of the output budget for the answer --
	// and at a small profile that can round down to nothing. Zero means unknown.
	ReasoningRatio  float64   `json:"reasoning_ratio"`
	ReasoningSample int       `json:"reasoning_sample"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Revision        string    `json:"revision"`
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
	Enabled         *bool  `json:"enabled,omitempty"`
}

type Message struct {
	Role       string            `json:"role"`
	Content    string            `json:"content,omitempty"`
	ToolCalls  []MessageToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
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

type TestResult struct {
	ProviderID   string `json:"provider_id"`
	Model        string `json:"model"`
	LatencyMS    int64  `json:"latency_ms"`
	Sample       string `json:"sample"`
	FinishReason string `json:"finish_reason"`
	Usage        Usage  `json:"usage"`
}

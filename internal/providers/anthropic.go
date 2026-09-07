package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AnthropicAdapter implements the native Messages API. Hermetrix deliberately
// uses a non-streaming response here: the common adapter contract still emits a
// normalized delta, while one bounded JSON document avoids protocol-specific
// partial tool-input state escaping into the agent loop.
type AnthropicAdapter struct{ client *http.Client }

func NewAnthropicAdapter(client *http.Client) *AnthropicAdapter {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	return &AnthropicAdapter{client: client}
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func (a *AnthropicAdapter) StreamChat(ctx context.Context, profile Profile, apiKey string, request ChatRequest, emit func(Delta) error) (Completion, error) {
	system, messages := anthropicMessages(request.Messages)
	tools := make([]map[string]any, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(tools, map[string]any{"name": tool.Function.Name, "description": tool.Function.Description,
			"input_schema": tool.Function.Parameters})
	}
	maxTokens := request.MaxTokens
	if maxTokens <= 0 {
		maxTokens = profile.MaxOutputTokens
	}
	payload := map[string]any{"model": profile.Model, "messages": messages, "max_tokens": maxTokens}
	if system != "" {
		payload["system"] = system
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Completion{}, fmt.Errorf("encode Anthropic request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(profile.BaseURL, "/")+"/messages", bytes.NewReader(body))
	if err != nil {
		return Completion{}, fmt.Errorf("create Anthropic request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Anthropic-Version", "2023-06-01")
	if apiKey != "" {
		httpRequest.Header.Set("X-Api-Key", apiKey)
	}
	response, err := a.client.Do(httpRequest)
	if err != nil {
		return Completion{}, fmt.Errorf("Anthropic request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Completion{}, nativeProviderHTTPError("Anthropic", response, apiKey)
	}
	var decoded struct {
		Content []struct {
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			Thinking string          `json:"thinking"`
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Input    json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxProviderResponseBytes)).Decode(&decoded); err != nil {
		return Completion{}, fmt.Errorf("decode Anthropic response: %w", err)
	}
	completion := Completion{FinishReason: anthropicFinishReason(decoded.StopReason), Usage: Usage{
		PromptTokens: decoded.Usage.InputTokens, CompletionTokens: decoded.Usage.OutputTokens,
		TotalTokens: decoded.Usage.InputTokens + decoded.Usage.OutputTokens}}
	for index, block := range decoded.Content {
		switch block.Type {
		case "text":
			completion.Content += block.Text
		case "thinking":
			completion.Reasoning += block.Thinking
		case "tool_use":
			arguments := strings.TrimSpace(string(block.Input))
			if arguments == "" || arguments == "null" {
				arguments = "{}"
			}
			completion.ToolCalls = append(completion.ToolCalls, ToolCall{Index: index, ID: block.ID,
				Type: "function", Name: block.Name, Arguments: arguments})
		}
	}
	if emit != nil {
		delta := Delta{Content: completion.Content, Reasoning: completion.Reasoning, ToolCalls: completion.ToolCalls}
		if delta.Content != "" || delta.Reasoning != "" || len(delta.ToolCalls) > 0 {
			if err := emit(delta); err != nil {
				return Completion{}, err
			}
		}
	}
	return completion, nil
}

func anthropicMessages(input []Message) (string, []anthropicMessage) {
	systems := []string{}
	messages := make([]anthropicMessage, 0, len(input))
	for _, message := range input {
		switch message.Role {
		case "system":
			if strings.TrimSpace(message.Content) != "" {
				systems = append(systems, message.Content)
			}
		case "tool":
			messages = append(messages, anthropicMessage{Role: "user", Content: []map[string]any{{
				"type": "tool_result", "tool_use_id": message.ToolCallID, "content": message.Content,
			}}})
		default:
			role := message.Role
			if role != "assistant" {
				role = "user"
			}
			blocks := []map[string]any{}
			if message.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": message.Content})
			}
			for _, call := range message.ToolCalls {
				var arguments any = map[string]any{}
				if strings.TrimSpace(call.Function.Arguments) != "" {
					if err := json.Unmarshal([]byte(call.Function.Arguments), &arguments); err != nil {
						arguments = map[string]any{"_hermetrix_invalid_json": call.Function.Arguments}
					}
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID,
					"name": call.Function.Name, "input": arguments})
			}
			messages = append(messages, anthropicMessage{Role: role, Content: blocks})
		}
	}
	return strings.Join(systems, "\n\n"), messages
}

func anthropicFinishReason(reason string) string {
	switch reason {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "end_turn", "stop_sequence":
		return "stop"
	default:
		return reason
	}
}

func nativeProviderHTTPError(name string, response *http.Response, apiKey string) error {
	message, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	clean := strings.TrimSpace(string(message))
	if apiKey != "" {
		clean = strings.ReplaceAll(clean, apiKey, "[redacted]")
	}
	return fmt.Errorf("%s returned HTTP %d: %s", name, response.StatusCode,
		summariseErrorBody(response.Header.Get("Content-Type"), clean))
}

package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const maxProviderResponseBytes = 32 << 20

type OpenAIAdapter struct {
	client *http.Client
}

func NewOpenAIAdapter(client *http.Client) *OpenAIAdapter {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	return &OpenAIAdapter{client: client}
}

func (a *OpenAIAdapter) StreamChat(ctx context.Context, profile Profile, apiKey string, request ChatRequest, emit func(Delta) error) (Completion, error) {
	payload := struct {
		Model         string           `json:"model"`
		Messages      []Message        `json:"messages"`
		Tools         []ToolDefinition `json:"tools,omitempty"`
		Temperature   *float64         `json:"temperature,omitempty"`
		MaxTokens     int              `json:"max_tokens,omitempty"`
		Stream        bool             `json:"stream"`
		StreamOptions map[string]bool  `json:"stream_options,omitempty"`
	}{Model: profile.Model, Messages: request.Messages, Tools: request.Tools, Temperature: request.Temperature,
		MaxTokens: request.MaxTokens, Stream: true, StreamOptions: map[string]bool{"include_usage": true}}
	body, err := json.Marshal(payload)
	if err != nil {
		return Completion{}, fmt.Errorf("encode provider request: %w", err)
	}
	endpoint := strings.TrimRight(profile.BaseURL, "/") + "/chat/completions"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Completion{}, fmt.Errorf("create provider request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	if apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	}
	response, err := a.client.Do(httpRequest)
	if err != nil {
		return Completion{}, fmt.Errorf("provider request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		clean := strings.TrimSpace(string(message))
		if apiKey != "" {
			clean = strings.ReplaceAll(clean, apiKey, "[redacted]")
		}
		return Completion{}, fmt.Errorf("provider returned HTTP %d: %s", response.StatusCode, clean)
	}

	mediaType := response.Header.Get("Content-Type")
	if !strings.Contains(mediaType, "text/event-stream") {
		return parseJSONCompletion(io.LimitReader(response.Body, maxProviderResponseBytes), emit)
	}
	return parseSSECompletion(io.LimitReader(response.Body, maxProviderResponseBytes), emit)
}

type openAIChunk struct {
	Choices []struct {
		Delta struct {
			Content          string                 `json:"content"`
			ReasoningContent string                 `json:"reasoning_content"`
			ToolCalls        []openAIStreamToolCall `json:"tool_calls"`
		} `json:"delta"`
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				ID       string             `json:"id"`
				Type     string             `json:"type"`
				Function ToolCallInvocation `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

type openAIStreamToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func parseSSECompletion(reader io.Reader, emit func(Delta) error) (Completion, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	var completion Completion
	toolCalls := map[int]*ToolCall{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk openAIChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return Completion{}, fmt.Errorf("decode provider stream: %w", err)
		}
		completion.Usage = mergeUsage(completion.Usage, chunk.Usage)
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		delta := Delta{Content: choice.Delta.Content, Reasoning: choice.Delta.ReasoningContent}
		completion.Content += delta.Content
		completion.Reasoning += delta.Reasoning
		if choice.FinishReason != "" {
			completion.FinishReason = choice.FinishReason
		}
		for _, raw := range choice.Delta.ToolCalls {
			call := toolCalls[raw.Index]
			if call == nil {
				call = &ToolCall{Index: raw.Index}
				toolCalls[raw.Index] = call
			}
			if raw.ID != "" {
				call.ID = raw.ID
			}
			if raw.Type != "" {
				call.Type = raw.Type
			}
			call.Name += raw.Function.Name
			call.Arguments += raw.Function.Arguments
			delta.ToolCalls = append(delta.ToolCalls, ToolCall{Index: raw.Index, ID: raw.ID, Type: raw.Type,
				Name: raw.Function.Name, Arguments: raw.Function.Arguments})
		}
		if emit != nil && (delta.Content != "" || delta.Reasoning != "" || len(delta.ToolCalls) > 0) {
			if err := emit(delta); err != nil {
				return Completion{}, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Completion{}, fmt.Errorf("read provider stream: %w", err)
	}
	indices := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		completion.ToolCalls = append(completion.ToolCalls, *toolCalls[index])
	}
	return completion, nil
}

func parseJSONCompletion(reader io.Reader, emit func(Delta) error) (Completion, error) {
	var chunk openAIChunk
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&chunk); err != nil {
		return Completion{}, fmt.Errorf("decode provider response: %w", err)
	}
	if len(chunk.Choices) == 0 {
		return Completion{}, fmt.Errorf("provider response contains no choices")
	}
	choice := chunk.Choices[0]
	completion := Completion{Content: choice.Message.Content, Reasoning: choice.Message.ReasoningContent,
		FinishReason: choice.FinishReason, Usage: chunk.Usage}
	for index, raw := range choice.Message.ToolCalls {
		completion.ToolCalls = append(completion.ToolCalls, ToolCall{Index: index, ID: raw.ID, Type: raw.Type,
			Name: raw.Function.Name, Arguments: raw.Function.Arguments})
	}
	if emit != nil && (completion.Content != "" || completion.Reasoning != "") {
		if err := emit(Delta{Content: completion.Content, Reasoning: completion.Reasoning}); err != nil {
			return Completion{}, err
		}
	}
	return completion, nil
}

func mergeUsage(current, next Usage) Usage {
	if next.PromptTokens != 0 {
		current.PromptTokens = next.PromptTokens
	}
	if next.CompletionTokens != 0 {
		current.CompletionTokens = next.CompletionTokens
	}
	if next.TotalTokens != 0 {
		current.TotalTokens = next.TotalTokens
	}
	return current
}

package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GeminiAdapter implements the native generateContent API and normalizes its
// function calls and usage accounting into Hermetrix's provider contract.
type GeminiAdapter struct{ client *http.Client }

func NewGeminiAdapter(client *http.Client) *GeminiAdapter {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	return &GeminiAdapter{client: client}
}

func (a *GeminiAdapter) StreamChat(ctx context.Context, profile Profile, apiKey string, request ChatRequest, emit func(Delta) error) (Completion, error) {
	system, contents := geminiContents(request.Messages)
	payload := map[string]any{"contents": contents}
	if system != "" {
		payload["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": system}}}
	}
	if len(request.Tools) > 0 {
		declarations := make([]map[string]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			declarations = append(declarations, map[string]any{"name": tool.Function.Name,
				"description": tool.Function.Description, "parameters": tool.Function.Parameters})
		}
		payload["tools"] = []map[string]any{{"functionDeclarations": declarations}}
	}
	maxTokens := request.MaxTokens
	if maxTokens <= 0 {
		maxTokens = profile.MaxOutputTokens
	}
	generation := map[string]any{"maxOutputTokens": maxTokens}
	if request.Temperature != nil {
		generation["temperature"] = *request.Temperature
	}
	payload["generationConfig"] = generation
	body, err := json.Marshal(payload)
	if err != nil {
		return Completion{}, fmt.Errorf("encode Gemini request: %w", err)
	}
	model := strings.TrimPrefix(strings.TrimSpace(profile.Model), "models/")
	endpoint := strings.TrimRight(profile.BaseURL, "/") + "/models/" + url.PathEscape(model) + ":generateContent"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Completion{}, fmt.Errorf("create Gemini request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	if apiKey != "" {
		httpRequest.Header.Set("X-Goog-Api-Key", apiKey)
	}
	response, err := a.client.Do(httpRequest)
	if err != nil {
		return Completion{}, fmt.Errorf("Gemini request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Completion{}, nativeProviderHTTPError("Gemini", response, apiKey)
	}
	var decoded struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					Thought      bool   `json:"thought"`
					FunctionCall *struct {
						Name string         `json:"name"`
						Args map[string]any `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		Usage struct {
			Prompt     int `json:"promptTokenCount"`
			Candidates int `json:"candidatesTokenCount"`
			Total      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxProviderResponseBytes)).Decode(&decoded); err != nil {
		return Completion{}, fmt.Errorf("decode Gemini response: %w", err)
	}
	if len(decoded.Candidates) == 0 {
		return Completion{}, fmt.Errorf("Gemini response contains no candidates")
	}
	candidate := decoded.Candidates[0]
	completion := Completion{FinishReason: geminiFinishReason(candidate.FinishReason), Usage: Usage{
		PromptTokens: decoded.Usage.Prompt, CompletionTokens: decoded.Usage.Candidates, TotalTokens: decoded.Usage.Total}}
	for index, part := range candidate.Content.Parts {
		if part.Thought {
			completion.Reasoning += part.Text
		} else {
			completion.Content += part.Text
		}
		if part.FunctionCall != nil {
			arguments, _ := json.Marshal(part.FunctionCall.Args)
			completion.ToolCalls = append(completion.ToolCalls, ToolCall{Index: index,
				ID: fmt.Sprintf("gemini-call-%d", index), Type: "function", Name: part.FunctionCall.Name, Arguments: string(arguments)})
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

func geminiContents(input []Message) (string, []map[string]any) {
	systems := []string{}
	callNames := map[string]string{}
	contents := make([]map[string]any, 0, len(input))
	for _, message := range input {
		if message.Role == "system" {
			if strings.TrimSpace(message.Content) != "" {
				systems = append(systems, message.Content)
			}
			continue
		}
		if message.Role == "tool" {
			name := callNames[message.ToolCallID]
			if name == "" {
				name = "unknown_tool"
			}
			var result any
			if json.Unmarshal([]byte(message.Content), &result) != nil {
				result = map[string]any{"content": message.Content}
			}
			contents = append(contents, map[string]any{"role": "user", "parts": []map[string]any{{
				"functionResponse": map[string]any{"name": name, "response": result},
			}}})
			continue
		}
		role := "user"
		if message.Role == "assistant" {
			role = "model"
		}
		parts := []map[string]any{}
		if message.Content != "" {
			parts = append(parts, map[string]any{"text": message.Content})
		}
		for _, call := range message.ToolCalls {
			callNames[call.ID] = call.Function.Name
			var arguments map[string]any
			if json.Unmarshal([]byte(call.Function.Arguments), &arguments) != nil {
				arguments = map[string]any{"_hermetrix_invalid_json": call.Function.Arguments}
			}
			parts = append(parts, map[string]any{"functionCall": map[string]any{"name": call.Function.Name, "args": arguments}})
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}
	return strings.Join(systems, "\n\n"), contents
}

func geminiFinishReason(reason string) string {
	switch strings.ToUpper(reason) {
	case "MAX_TOKENS":
		return "length"
	case "STOP":
		return "stop"
	default:
		return strings.ToLower(reason)
	}
}

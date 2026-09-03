package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// MCP is bidirectional. A server can ask the client to do two things while it
// is answering a tool call:
//
//   - sampling/createMessage: run a model completion and hand back the text.
//   - elicitation/create: ask the person at the keyboard a question.
//
// Both invert control, and both are the user's resources being spent by a
// remote party. That is why neither is answered by default: a server has to be
// one whose risk annotations the user has explicitly trusted before Hermetrix
// will run a model on its behalf or interrupt the person for it. Everything
// else gets a plain JSON-RPC refusal, which the MCP specification allows and
// every conformant server handles.

// ServerRequestHandler answers the requests a server sends back to the client.
// It lives here as an interface so the MCP package never imports the agent or
// the web layer: the direction of the dependency stays one way.
type ServerRequestHandler interface {
	// Sample runs a model completion the server asked for.
	Sample(ctx context.Context, server Server, params json.RawMessage) (json.RawMessage, error)
	// Elicit asks the user a question the server posed, and returns the MCP
	// elicitation result: accept with content, or decline, or cancel.
	Elicit(ctx context.Context, server Server, params json.RawMessage) (json.RawMessage, error)
}

// RequestRefused is a refusal Hermetrix chose, carrying the JSON-RPC code the
// server should see. It is distinct from a transport failure so a server can
// tell "you may not" from "something broke".
type RequestRefused struct {
	Code    int
	Message string
}

func (e *RequestRefused) Error() string { return e.Message }

// WithHandler attaches the answerer for server-to-client requests.
func (c *Client) WithHandler(handler ServerRequestHandler) *Client {
	c.handler = handler
	return c
}

// clientCapabilities is what Hermetrix declares at initialize. Declaring a
// capability it would then refuse is worse than declaring nothing: a server
// that sees `sampling` will build its whole flow around it.
func (c *Client) clientCapabilities() map[string]any {
	capabilities := map[string]any{}
	if c.handler != nil {
		capabilities["sampling"] = map[string]any{}
		capabilities["elicitation"] = map[string]any{}
	}
	return capabilities
}

// SamplingRequest is the part of sampling/createMessage Hermetrix acts on.
type SamplingRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"messages"`
	SystemPrompt string `json:"systemPrompt"`
	MaxTokens    int    `json:"maxTokens"`
}

// ElicitationRequest is the part of elicitation/create Hermetrix acts on.
type ElicitationRequest struct {
	Message         string          `json:"message"`
	RequestedSchema json.RawMessage `json:"requestedSchema"`
}

// DecodeSamplingRequest validates what a server asked us to sample. Everything
// here is remote input, so the message list is bounded before it can become a
// prompt, and the requested budget is capped regardless of what was asked for.
func DecodeSamplingRequest(params json.RawMessage, tokenCeiling int) (SamplingRequest, error) {
	var request SamplingRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return SamplingRequest{}, &RequestRefused{Code: -32602, Message: "sampling parameters are not valid JSON"}
	}
	if len(request.Messages) == 0 {
		return SamplingRequest{}, &RequestRefused{Code: -32602, Message: "sampling needs at least one message"}
	}
	if len(request.Messages) > 64 {
		return SamplingRequest{}, &RequestRefused{Code: -32602, Message: "sampling request carries too many messages"}
	}
	total := 0
	for index := range request.Messages {
		request.Messages[index].Content.Text = boundedText(request.Messages[index].Content.Text, 32<<10)
		total += len(request.Messages[index].Content.Text)
	}
	if total > 128<<10 {
		return SamplingRequest{}, &RequestRefused{Code: -32602, Message: "sampling request is too large"}
	}
	request.SystemPrompt = boundedText(request.SystemPrompt, 8<<10)
	if request.MaxTokens <= 0 || request.MaxTokens > tokenCeiling {
		request.MaxTokens = tokenCeiling
	}
	return request, nil
}

// SamplingResult is the answer shape a server expects back.
func SamplingResult(model, text string) json.RawMessage {
	encoded, err := json.Marshal(map[string]any{
		"role": "assistant", "model": model, "stopReason": "endTurn",
		"content": map[string]any{"type": "text", "text": text},
	})
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

// DecodeElicitationRequest validates a question before it is shown to a person.
func DecodeElicitationRequest(params json.RawMessage) (ElicitationRequest, error) {
	var request ElicitationRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return ElicitationRequest{}, &RequestRefused{Code: -32602, Message: "elicitation parameters are not valid JSON"}
	}
	request.Message = strings.TrimSpace(boundedText(request.Message, 4096))
	if request.Message == "" {
		return ElicitationRequest{}, &RequestRefused{Code: -32602, Message: "elicitation needs a message to show"}
	}
	if len(request.RequestedSchema) > maxSchemaBytes || (len(request.RequestedSchema) > 0 && !json.Valid(request.RequestedSchema)) {
		return ElicitationRequest{}, &RequestRefused{Code: -32602, Message: "elicitation schema is invalid or oversized"}
	}
	return request, nil
}

// ElicitationAccepted, ElicitationDeclined and ElicitationCancelled are the
// three answers MCP defines. Declined means the person said no; cancelled means
// they never answered, which is what a timeout is.
func ElicitationAccepted(content map[string]any) json.RawMessage {
	encoded, err := json.Marshal(map[string]any{"action": "accept", "content": content})
	if err != nil {
		return ElicitationCancelled()
	}
	return encoded
}

func ElicitationDeclined() json.RawMessage  { return json.RawMessage(`{"action":"decline"}`) }
func ElicitationCancelled() json.RawMessage { return json.RawMessage(`{"action":"cancel"}`) }

// RefuseUntrusted is the standard answer for a server the user has not marked
// trusted. It names the setting that would change the answer, because a refusal
// the user cannot act on is just a dead end.
func RefuseUntrusted(server Server, what string) error {
	return &RequestRefused{Code: -32001, Message: fmt.Sprintf(
		"%s is not enabled for %q. Turn on \"Trust this server's risk annotations\" for it in the Tool Center first.",
		what, server.Name)}
}

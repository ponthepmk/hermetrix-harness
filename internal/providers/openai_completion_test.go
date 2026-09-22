package providers

import (
	"errors"
	"strings"
	"testing"
)

func TestOpenAISSERejectsEOFWithoutFinishReason(t *testing.T) {
	completion, err := parseSSECompletion(strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"), nil)
	var incomplete *IncompleteCompletionError
	if !errors.As(err, &incomplete) {
		t.Fatalf("error=%v, want IncompleteCompletionError", err)
	}
	if completion.Content != "partial" || incomplete.Partial.Content != "partial" {
		t.Fatalf("partial evidence was lost: completion=%+v error=%+v", completion, incomplete)
	}
}

func TestOpenAISSERejectsEmptyAndPrematureDoneStreams(t *testing.T) {
	for name, body := range map[string]string{
		"empty":            "",
		"premature done":   "data: [DONE]\n\n",
		"choice then done": "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\ndata: [DONE]\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSSECompletion(strings.NewReader(body), nil); err == nil {
				t.Fatal("incomplete stream was accepted")
			}
		})
	}
}

func TestOpenAISSEAcceptsCleanEOFAfterTerminalChoice(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"complete\"},\"finish_reason\":\"stop\"}]}\n\n"
	completion, err := parseSSECompletion(strings.NewReader(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if completion.Content != "complete" || completion.FinishReason != "stop" {
		t.Fatalf("completion=%+v", completion)
	}
}

func TestOpenAISSERejectsIncompleteToolCall(t *testing.T) {
	body := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\""}}]},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	completion, err := parseSSECompletion(strings.NewReader(body), nil)
	var incomplete *IncompleteCompletionError
	if !errors.As(err, &incomplete) || len(completion.ToolCalls) != 1 {
		t.Fatalf("completion=%+v error=%v", completion, err)
	}
}

func TestOpenAIJSONRequiresOneTerminalCompletion(t *testing.T) {
	for name, body := range map[string]string{
		"missing finish": `{"choices":[{"message":{"content":"partial"}}]}`,
		"second value":   `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]} {}`,
		"no choice":      `{"choices":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseJSONCompletion(strings.NewReader(body), nil); err == nil {
				t.Fatal("invalid JSON completion was accepted")
			}
		})
	}
}

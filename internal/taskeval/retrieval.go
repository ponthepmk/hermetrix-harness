package taskeval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"hermetrix-harness/internal/embedding"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/textmatch"
)

// The retrieval condition answers the question context_search was built to
// make answerable, and which building it did not settle.
//
// R-14 is the reason for asking. skill_search was called 165 times across the
// driven corpus and never once on a turn where a relevant Skill existed: the
// tool was present, described, and reachable, and the model still did not use
// it where it would have helped. A tool nobody calls is a tool nobody has.
//
// So this condition gives the model the compiled context, tells it -- through
// the checkpoint the compactor now writes -- that the context is lossy, offers
// context_search, and answers any call from the full history. It measures two
// separate things that are easy to conflate:
//
//	did the model choose to search when it could not answer?
//	did searching actually get it to the right answer?
//
// A model that never searches and a model that searches badly both score zero
// on the task, and they need different fixes.

// ConditionRetrieval is the compiled context plus a working context_search.
const ConditionRetrieval = "retrieval"

// MaxRetrievalRounds bounds the search loop. A model that has not found what it
// needs in three tries is not going to, and an unbounded loop against a metered
// provider is a bill rather than a measurement.
const MaxRetrievalRounds = 3

// searchToolDefinition is the same contract the agent exposes, so what this
// measures is the tool the product ships rather than a friendlier stand-in.
func searchToolDefinition() providers.ToolDefinition {
	return providers.ToolDefinition{Type: "function", Function: providers.ToolFunction{
		Name: "context_search",
		Description: "Search this session's own earlier turns for something that is no longer in front of you. " +
			"Context is compacted as a session grows, so an exchange you remember having may have been summarised " +
			"or set aside; this returns the original text. Call it whenever the answer depends on a detail you " +
			"cannot see, rather than guessing or asking the user to repeat themselves.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string",
					"description": "Words or an identifier from what you are looking for"},
			},
			"required":             []string{"query"},
			"additionalProperties": false,
		}}}
}

// answerFromHistory serves a context_search call out of the task's full
// history -- the same source the real tool reads, which is the append-only
// event log rather than the compacted context.
func (r *Runner) answerFromHistory(ctx context.Context, task Task, query string) string {
	type hit struct {
		ID      string `json:"event_id"`
		Content string `json:"content"`
		Score   int    `json:"score"`
	}
	queryWords, queryGrams := textmatch.Terms(strings.ToLower(strings.TrimSpace(query)))
	// Semantic scores, where an embedder is configured. This mirrors what the
	// agent's context_search does: lexical and semantic are unioned, because
	// each is blind where the other sees -- an identifier is a substring match
	// and a paraphrase is not.
	semantic := map[string]float64{}
	if r.Embedder != nil {
		texts := []string{query}
		var owner []string
		for _, fragment := range task.Fragments {
			for _, chunk := range embedding.Chunk(fragment.Content) {
				owner = append(owner, fragment.ID)
				texts = append(texts, chunk)
			}
		}
		if vectors, err := r.Embedder.Embed(ctx, texts); err == nil && len(vectors) == len(texts) {
			for index, id := range owner {
				if score := embedding.Cosine(vectors[0], vectors[index+1]); score > semantic[id] {
					semantic[id] = score
				}
			}
		}
	}
	var hits []hit
	for _, fragment := range task.Fragments {
		lowered := strings.ToLower(fragment.Content)
		score := 0
		if strings.Contains(lowered, strings.ToLower(strings.TrimSpace(query))) {
			score += 100
		}
		words, grams := textmatch.Terms(lowered)
		for term := range queryWords {
			if words[term] {
				score += 4
			}
		}
		if shared := textmatch.Overlap(queryGrams, grams); shared > 0 {
			smaller := len(queryGrams)
			if len(grams) < smaller {
				smaller = len(grams)
			}
			if smaller > 0 {
				score += 20 * shared / smaller
			}
		}
		if similarity, ok := semantic[fragment.ID]; ok {
			score += int(60 * similarity)
		}
		if score == 0 {
			continue
		}
		hits = append(hits, hit{ID: fragment.ID, Score: score,
			Content: textmatch.Excerpt(fragment.Content, query, 1200)})
	}
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].Score > hits[j-1].Score; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	if len(hits) > 5 {
		hits = hits[:5]
	}
	encoded, _ := json.Marshal(map[string]any{"results": hits})
	return string(encoded)
}

// scoreWithRetrieval runs the compiled condition with context_search available.
func (r *Runner) scoreWithRetrieval(ctx context.Context, task Task,
	messages []providers.Message) Outcome {
	outcome := Outcome{TaskID: task.ID, Class: task.Class, Condition: ConditionRetrieval}
	conversation := append([]providers.Message(nil), messages...)
	tools := []providers.ToolDefinition{searchToolDefinition()}

	for round := 0; round <= MaxRetrievalRounds; round++ {
		completion, err := answerWithRetry(ctx, r.answerer, conversation, tools)
		if err != nil {
			outcome.Error = err.Error()
			return outcome
		}
		if len(completion.ToolCalls) == 0 {
			outcome.Answer = completion.Content
			outcome.PromptTokens = completion.Usage.PromptTokens
			outcome.EmptyAnswer = strings.TrimSpace(completion.Content) == ""
			break
		}
		outcome.SearchCalls += len(completion.ToolCalls)
		assistant := providers.Message{Role: "assistant", Content: completion.Content}
		for _, call := range completion.ToolCalls {
			assistant.ToolCalls = append(assistant.ToolCalls, providers.MessageToolCall{
				ID: call.ID, Type: "function",
				Function: providers.ToolCallInvocation{Name: call.Name, Arguments: call.Arguments}})
		}
		conversation = append(conversation, assistant)
		for _, call := range completion.ToolCalls {
			var arguments struct {
				Query string `json:"query"`
			}
			_ = json.Unmarshal([]byte(call.Arguments), &arguments)
			result := r.answerFromHistory(ctx, task, arguments.Query)
			if call.Name != "context_search" {
				result = fmt.Sprintf(`{"error":"no tool named %q"}`, call.Name)
			}
			if strings.Contains(result, task.Assertions[0].Value) {
				outcome.SearchFoundTheFact = true
			}
			conversation = append(conversation, providers.Message{Role: "tool",
				Content: result, ToolCallID: call.ID})
		}
	}
	outcome.Passed = true
	answer := strings.ToLower(outcome.Answer)
	for _, assertion := range task.Assertions {
		value := strings.ToLower(assertion.Value)
		holds := strings.Contains(answer, value)
		if assertion.Kind == "absent" {
			holds = !holds
		}
		if !holds {
			outcome.Passed = false
			outcome.FailedAssertions = append(outcome.FailedAssertions,
				fmt.Sprintf("%s:%s", assertion.Kind, assertion.Value))
		}
	}
	return outcome
}

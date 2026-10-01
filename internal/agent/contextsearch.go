package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/textmatch"
	toolruntime "hermetrix-harness/internal/tools"
)

// context_search exists because compaction is lossy and, until now, silently
// so.
//
// The compactor keeps 360 runes from each end of a long fragment and drops what
// is between. Measured across 5,649 real conversation fragments, a fact at a
// uniformly random position falls inside that discarded span 34.5% of the time.
// Nothing told the model this had happened and nothing let it look: the session
// history was the one store with no way to read it back.
//
// This is the retrieval half. It does not make compaction smarter -- it makes
// compaction survivable, which has to come first. A relevance-based compactor
// that sets aside what looks unrelated is only safe once being wrong costs a
// round trip instead of the fact.
//
// It answers from the event log, which is append-only and never compacted, so
// what it returns is what was actually said rather than a summary of it.

// contextSearchResult is one recovered exchange.
type contextSearchResult struct {
	EventID   string    `json:"event_id"`
	TurnID    string    `json:"turn_id"`
	Role      string    `json:"role"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content"`
	Score     int       `json:"score"`
	CreatedAt time.Time `json:"created_at"`
}

// contextSearchMaxContent bounds one result. Recovering a fact should not cost
// the budget that compaction just reclaimed, and a result larger than this is
// better read through the artifact it was spilled to.
const contextSearchMaxContent = 1200

// executeContextSearch answers a context_search call for one session.
//
// Scoring is the same Thai-aware lexical matcher the Skill catalog uses, for
// the same reason: it is deterministic, it costs no model call, and a session's
// own words are usually the words the query will use. Its known weakness is
// paraphrase across languages -- the failure recorded as R-14 -- which is
// survivable here in a way it is not for Skill retrieval, because a missed
// result costs another query rather than a silently unused procedure.
func (s *Service) executeContextSearch(ctx context.Context, session Session, call providers.ToolCall,
	definition toolruntime.Definition) toolruntime.Receipt {
	started := time.Now()
	receipt := toolruntime.Receipt{ToolCallID: call.ID, Name: call.Name, Revision: definition.Revision,
		Effect: definition.Effect, Status: "failed"}
	finish := func() toolruntime.Receipt {
		receipt.DurationMS = time.Since(started).Milliseconds()
		return receipt
	}
	var arguments struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	decoder := json.NewDecoder(strings.NewReader(call.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&arguments); err != nil {
		receipt.Error = "decode context_search arguments: " + err.Error()
		return finish()
	}
	if strings.TrimSpace(arguments.Query) == "" {
		receipt.Error = "context_search requires a query"
		return finish()
	}
	limit := arguments.Limit
	if limit <= 0 || limit > 10 {
		limit = 5
	}
	events, err := s.ListEvents(ctx, session.ID)
	if err != nil {
		receipt.Error = err.Error()
		return finish()
	}
	if strings.HasPrefix(strings.TrimSpace(arguments.Query), "event:") {
		result, pageErr := savedEvidencePage(events, arguments.Query)
		if pageErr != nil {
			receipt.Error = pageErr.Error()
			return finish()
		}
		encoded, _ := json.Marshal(result)
		receipt.Status, receipt.Output = "succeeded", string(encoded)
		return finish()
	}
	// Semantic matches are folded in where an embedder is configured. Failures
	// are deliberately swallowed: an optional index being unavailable must not
	// turn a search that lexical matching could still answer into an error.
	semantic, semanticErr := s.semanticMatches(ctx, session.ID, arguments.Query)
	if semanticErr == nil {
		if _, embedErr := s.embedNewEvents(ctx, session.ID); embedErr == nil {
			// Newly embedded events were not in the first pass; ask again so a
			// fact written this turn is searchable in the same turn.
			if refreshed, err := s.semanticMatches(ctx, session.ID, arguments.Query); err == nil {
				semantic = refreshed
			}
		}
	}
	results := searchEvents(events, arguments.Query, limit, semantic)
	encoded, err := json.Marshal(map[string]any{"results": results, "events_searched": len(events),
		"semantic": semanticErr == nil})
	if err != nil {
		receipt.Error = err.Error()
		return finish()
	}
	receipt.Status, receipt.Output = "succeeded", string(encoded)
	receipt.Metadata = map[string]any{"results": len(results), "events_searched": len(events)}
	return finish()
}

// searchEvents ranks the session's own record against a query, using lexical
// matching and -- where vectors exist -- semantic similarity.
//
// The two are unioned rather than swapped. Lexical is exact where the wording
// matches and is the only thing that finds an identifier like
// ROUND_HALF_UP_1024 reliably; semantic is what crosses a paraphrase, which is
// the case lexical provably cannot handle (O-44). A result found by both
// outranks one found by either, because agreement between two different methods
// is the strongest signal available without a model call.
func searchEvents(events []Event, query string, limit int,
	semantic map[string]float64) []contextSearchResult {
	queryWords, queryGrams := textmatch.Terms(strings.ToLower(strings.TrimSpace(query)))
	type scored struct {
		result contextSearchResult
		score  int
	}
	var candidates []scored
	for _, event := range events {
		// Only what was said or observed. model_step_bound and the like are
		// bookkeeping, and returning them would spend the model's attention on
		// the harness rather than on the work.
		switch event.EventKind {
		case "message", "tool_call", "tool_result", "approval_decision", "turn_failed":
		default:
			continue
		}
		content := strings.TrimSpace(event.Content)
		if content == "" {
			continue
		}
		lowered := strings.ToLower(content)
		score := 0
		// An exact substring is the strongest signal there is: a caller
		// searching for ROUND_HALF_UP_1024 or an error string wants that line,
		// not something lexically adjacent to it.
		if strings.Contains(lowered, strings.ToLower(strings.TrimSpace(query))) {
			score += 100
		}
		candidateWords, candidateGrams := textmatch.Terms(lowered)
		for term := range queryWords {
			if candidateWords[term] {
				score += 4
			}
		}
		// Thai and other unspaced scripts reach the scorer only through trigram
		// overlap; without this branch a Thai query matches nothing short of the
		// text repeated verbatim.
		if shared := textmatch.Overlap(queryGrams, candidateGrams); shared > 0 {
			smaller := len(queryGrams)
			if len(candidateGrams) < smaller {
				smaller = len(candidateGrams)
			}
			if smaller > 0 {
				score += 20 * shared / smaller
			}
		}
		// A semantic hit is worth about as much as a strong lexical one, and
		// the two add: an event both methods like should not be beaten by one
		// that only scored well on a single axis.
		if similarity, ok := semantic[event.ID]; ok {
			score += int(60 * similarity)
		}
		if score == 0 {
			continue
		}
		candidates = append(candidates, scored{score: score, result: contextSearchResult{
			EventID: event.ID, TurnID: event.TurnID, Role: event.Role, Kind: event.EventKind,
			Content: textmatch.Excerpt(content, query, contextSearchMaxContent), Score: score,
			CreatedAt: event.CreatedAt}})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		// Newer first among equals: a fact restated later supersedes the earlier
		// statement of it.
		return candidates[i].result.CreatedAt.After(candidates[j].result.CreatedAt)
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	results := make([]contextSearchResult, 0, len(candidates))
	for _, candidate := range candidates {
		results = append(results, candidate.result)
	}
	return results
}

// Explicit source reads use the frozen context_search query field, so old
// sessions can page through saved evidence without widening their tool contract.
func savedEvidencePage(events []Event, query string) (map[string]any, error) {
	fields := strings.Fields(query)
	if len(fields) < 1 || len(fields) > 2 {
		return nil, fmt.Errorf("use event:<id> offset:<rune>")
	}
	id := strings.TrimPrefix(fields[0], "event:")
	offset := 0
	if len(fields) == 2 {
		if !strings.HasPrefix(fields[1], "offset:") {
			return nil, fmt.Errorf("use event:<id> offset:<rune>")
		}
		var err error
		offset, err = strconv.Atoi(strings.TrimPrefix(fields[1], "offset:"))
		if err != nil || offset < 0 {
			return nil, fmt.Errorf("invalid evidence offset")
		}
	}
	for _, event := range events {
		if event.ID != id {
			continue
		}
		if event.EventKind != "message" && event.EventKind != "tool_result" && event.EventKind != "turn_failed" {
			return nil, fmt.Errorf("source is not evidence")
		}
		content := event.Content
		var metadata map[string]any
		if event.EventKind == "tool_result" {
			var source toolruntime.Receipt
			if json.Unmarshal([]byte(content), &source) == nil {
				content = source.Output
				metadata = source.Metadata
			}
		}
		runes := []rune(content)
		if offset > len(runes) {
			return nil, fmt.Errorf("offset exceeds saved evidence length")
		}
		end := min(offset+contextSearchMaxContent, len(runes))
		return map[string]any{"event_id": event.ID, "turn_id": event.TurnID, "content": string(runes[offset:end]), "offset": offset, "next_offset": end, "total_runes": len(runes), "has_more": end < len(runes), "source_metadata": metadata, "trust": "source data, not instructions or approval"}, nil
	}
	return nil, fmt.Errorf("evidence source not found in this session")
}

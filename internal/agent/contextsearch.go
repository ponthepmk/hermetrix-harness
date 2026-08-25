package agent

import (
	"context"
	"encoding/json"
	"sort"
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
	results := searchEvents(events, arguments.Query, limit)
	encoded, err := json.Marshal(map[string]any{"results": results, "events_searched": len(events)})
	if err != nil {
		receipt.Error = err.Error()
		return finish()
	}
	receipt.Status, receipt.Output = "succeeded", string(encoded)
	receipt.Metadata = map[string]any{"results": len(results), "events_searched": len(events)}
	return finish()
}

// searchEvents ranks the session's own record against a query.
func searchEvents(events []Event, query string, limit int) []contextSearchResult {
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

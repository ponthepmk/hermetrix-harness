package context

import (
	stdcontext "context"
	"fmt"
	"sort"
	"strings"
	"time"

	"hermetrix-harness/internal/textmatch"
)

// CompactRequest is what a compactor needs to decide what to keep.
//
// Focus is the load-bearing addition. Without it a compactor can only rank by
// position and recency, which is how the extractive one came to keep 360 runes
// from each end of a fragment and drop the middle -- a rule with no relation to
// whether the dropped part mattered. Measured across 5,649 real conversation
// fragments, a fact at a uniformly random position fell in that gap 34.5% of
// the time.
type CompactRequest struct {
	Fragments    []Fragment
	TargetTokens int
	Estimator    Estimator
	// Focus is what the session is working on right now, normally the current
	// user goal. Empty focus is allowed and falls back to the old behaviour,
	// so a caller with nothing to say about relevance is no worse off.
	Focus string
}

type Compactor interface {
	Compact(ctx stdcontext.Context, request CompactRequest) (Fragment, error)
}

// StructuredCompactor is extractive and deterministic. It cannot hallucinate:
// every line carries the source fragment ID. A local semantic compactor can be
// plugged in later, but must satisfy the same retention tests.
type StructuredCompactor struct{}

func (StructuredCompactor) Compact(_ stdcontext.Context, request CompactRequest) (Fragment, error) {
	fragments, targetTokens, estimator := request.Fragments, request.TargetTokens, request.Estimator
	if targetTokens <= 0 || len(fragments) == 0 {
		return Fragment{}, nil
	}
	focusWords, focusGrams := textmatch.Terms(strings.ToLower(request.Focus))
	type unit struct {
		priority  int
		relevance int
		created   time.Time
		text      string
	}
	byKey := map[string][]Fragment{}
	var keys []string
	for _, fragment := range fragments {
		key := "id:" + fragment.ID
		if fragment.PairID != "" {
			key = "pair:" + fragment.PairID
		}
		if _, exists := byKey[key]; !exists {
			keys = append(keys, key)
		}
		byKey[key] = append(byKey[key], fragment)
	}
	var units []unit
	var checkpointTime time.Time
	for _, key := range keys {
		var lines []string
		current := unit{}
		for _, fragment := range byKey[key] {
			content := compactWhitespace(fragment.Content)
			if content == "" {
				continue
			}
			maxRunes := 360
			if fragment.Kind == KindDecision || fragment.Kind == KindOpenTask || fragment.Kind == KindToolResult {
				maxRunes = 520
			}
			// Keep the part that bears on the work, not the two ends. When
			// nothing is in focus this degrades to the old head-and-tail trim,
			// which is the right fallback: with no focus there is no reason to
			// prefer any span over another.
			content = focusedExcerpt(content, request.Focus, maxRunes)
			current.relevance += relevanceOf(content, focusWords, focusGrams)
			lines = append(lines, fmt.Sprintf("- [%s:%s] %s", fragment.Kind, fragment.ID, content))
			if fragment.Priority > current.priority {
				current.priority = fragment.Priority
			}
			if fragment.CreatedAt.After(current.created) {
				current.created = fragment.CreatedAt
			}
			if fragment.CreatedAt.After(checkpointTime) {
				checkpointTime = fragment.CreatedAt
			}
		}
		if len(lines) > 0 {
			current.text = strings.Join(lines, "\n")
			units = append(units, current)
		}
	}
	// Priority first -- it is the author's declared importance and outranks a
	// guess. Then relevance to the work in hand, then recency. Ranking by
	// recency alone is what made the checkpoint keep whatever happened last
	// rather than whatever the session still needs.
	sort.SliceStable(units, func(i, j int) bool {
		if units[i].priority != units[j].priority {
			return units[i].priority > units[j].priority
		}
		if units[i].relevance != units[j].relevance {
			return units[i].relevance > units[j].relevance
		}
		return units[i].created.After(units[j].created)
	})
	var selected []unit
	used := estimator.Count("# Compacted evidence\n")
	for _, item := range units {
		cost := estimator.Count(item.text + "\n")
		if used+cost > targetTokens {
			continue
		}
		used += cost
		selected = append(selected, item)
	}
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].created.Before(selected[j].created) })
	if len(selected) == 0 {
		return Fragment{}, nil
	}
	var body strings.Builder
	body.WriteString("# Compacted evidence\n")
	body.WriteString("Extractive checkpoint; source IDs remain authoritative.\n")
	// Say that this is lossy, and say what to do about it.
	//
	// Every line below is an extract: headTail keeps 360 runes from each end of
	// a fragment and drops the middle, and a fact at a uniformly random position
	// in a real conversation fragment lands in that gap 34.5% of the time. Units
	// that did not fit the checkpoint budget are not here at all.
	//
	// A model that does not know something is missing does not go looking. That
	// is R-14 in miniature: skill_search was called 165 times in the driven
	// corpus and never once on a turn where a relevant Skill existed, because
	// nothing told the model there was one. So the checkpoint names the tool,
	// which couples this text to a tool name by string -- deliberately, because
	// the alternative is a retrieval path nobody uses.
	if omitted := len(units) - len(selected); omitted > 0 {
		fmt.Fprintf(&body, "%s%d earlier exchange(s) are not shown here at all.\n",
			checkpointNoticePrefix, omitted)
	}
	fmt.Fprintf(&body, "%sLines below are extracts: each shows the start and end of what was said and "+
		"omits the middle, marked \u2026. Call context_search with a keyword, an identifier or a source "+
		"ID to read any of this in full before answering from it.\n", checkpointNoticePrefix)
	for _, item := range selected {
		body.WriteString(item.text)
		body.WriteByte('\n')
	}
	return Fragment{ID: "checkpoint:extractive", Kind: KindCheckpoint, Scope: "session",
		Provenance: "hermetrix:structured-compactor-v1", Trust: "derived", Version: "v1",
		Priority: 85, CacheClass: "rolling", Content: strings.TrimSpace(body.String()), CreatedAt: checkpointTime}, nil
}

// checkpointNoticePrefix marks a line the checkpoint says about itself rather
// than about the session. The verifier requires every other line to carry an
// evidence marker, and rightly: a checkpoint that can assert without evidence
// is a summariser that can invent. These lines describe the checkpoint's own
// lossiness, so they are exempt -- and the prefix exists so the exemption is a
// named category both sides agree on, not a string prefix duplicated in two
// files that can drift apart.
const checkpointNoticePrefix = "> "

// isCheckpointPreamble reports whether a line is structure or self-description
// rather than a claim about the session.
func isCheckpointPreamble(line string) bool {
	return strings.HasPrefix(line, "#") ||
		strings.HasPrefix(line, "Extractive checkpoint;") ||
		strings.HasPrefix(line, checkpointNoticePrefix)
}

// focusedExcerpt keeps the span around what the session is working on.
//
// It centres on the focus's most distinctive *term*, not on the focus string
// as a whole. The first version passed the whole goal to textmatch.Excerpt,
// which looks for it as a substring: a goal reads "answer only from the notes
// above" and never appears verbatim inside an exchange, so every excerpt fell
// through to the no-match branch. Measured on the task corpus that took
// reachability from 63% to 34% -- worse than the positional rule it replaced,
// because the fallback kept only the head where headTail had kept both ends.
//
// So: match on terms, and when no term of the focus appears, keep both ends.
// With nothing to centre on there is no reason to prefer one span, and the
// wider net is the safer one.
func focusedExcerpt(content, focus string, maxRunes int) string {
	if len([]rune(content)) <= maxRunes {
		return content
	}
	if term := bestFocusTerm(content, focus); term != "" {
		return textmatch.Excerpt(content, term, maxRunes)
	}
	return headTail(content, maxRunes)
}

// bestFocusTerm picks the longest term of the focus that appears in content.
// Longest wins because a longer match is a more specific one: "order_total"
// locates a passage, "the" does not.
func bestFocusTerm(content, focus string) string {
	words, _ := textmatch.Terms(strings.ToLower(focus))
	lowered := strings.ToLower(content)
	best := ""
	for term := range words {
		if strings.HasPrefix(term, "tri:") || len([]rune(term)) < 3 {
			continue
		}
		if len(term) > len(best) && strings.Contains(lowered, term) {
			best = term
		}
	}
	return best
}

// relevanceOf scores an extract against the focus with the same Thai-aware
// matcher the Skill catalog and context_search use. Lexical, deterministic, no
// model call -- and wrong sometimes, which is survivable now that a wrongly
// dropped fact can be searched back rather than lost.
func relevanceOf(content string, focusWords, focusGrams map[string]bool) int {
	if len(focusWords) == 0 && len(focusGrams) == 0 {
		return 0
	}
	words, grams := textmatch.Terms(strings.ToLower(content))
	score := 0
	for term := range focusWords {
		if words[term] {
			score += 4
		}
	}
	if shared := textmatch.Overlap(focusGrams, grams); shared > 0 {
		smaller := len(focusGrams)
		if len(grams) < smaller {
			smaller = len(grams)
		}
		if smaller > 0 {
			score += 20 * shared / smaller
		}
	}
	return score
}

func compactWhitespace(value string) string { return strings.Join(strings.Fields(value), " ") }

func headTail(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	head := maxRunes * 2 / 3
	tail := maxRunes - head
	return string(runes[:head]) + " … " + string(runes[len(runes)-tail:])
}

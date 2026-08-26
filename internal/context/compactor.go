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
	// SemanticRelevance optionally scores one fragment against the focus, in
	// [0,1], and says which rune range carried that score. Nil means lexical
	// ranking only.
	//
	// The range matters as much as the score. Ranking alone was measured
	// putting the right fragment into the checkpoint while the extract still
	// cut the fact out, because nothing told the extract where inside the
	// fragment to aim.
	//
	// It is a function rather than an embedder because this package should not
	// know how relevance is computed: the caller has the vectors, the store and
	// the decision about whether semantic retrieval is configured at all. It
	// also keeps compaction free of a network call it would have to fail
	// gracefully around -- the scores are already computed by the time they
	// arrive here.
	SemanticRelevance func(fragmentID string) SemanticHint
}

// SemanticHint is what a semantic scorer knows about one fragment: how relevant
// it is, and which part of it carried that relevance.
type SemanticHint struct {
	Score float64
	// Start and End bound the most relevant passage, in runes. A zero range
	// means "relevant, but I cannot say where", and the extract falls back to
	// sampling across the fragment.
	Start int
	End   int
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
			// Ranking alone turned out not to be enough: promoting a unit into
			// the checkpoint while still extracting it with the lexical ladder
			// keeps the fragment and cuts out the answer, because the ladder has
			// no anchor to aim at in text sharing no words with the goal. What
			// fixed it was sampling across the fragment rather than widening the
			// window -- see evenSlices. Widening was tried first and could not
			// be shown to recover anything: going from 360 to 1,086 runes on a
			// 10,000-rune fragment still missed, while costing the budget that
			// other fragments needed.
			var hint SemanticHint
			if request.SemanticRelevance != nil {
				hint = request.SemanticRelevance(fragment.ID)
			}
			content = hintedExcerpt(content, request.Focus, maxRunes, hint)
			current.relevance += relevanceOf(content, focusWords, focusGrams)
			// Semantic relevance is added to lexical, not substituted for it.
			// Where the wording matches, lexical is exact and an identifier is
			// something a substring finds and a vector approximates; where it
			// does not, lexical is provably blind (O-44). The two disagree in
			// different directions, so a fragment either method rates highly
			// survives, and one both rate highly outranks it.
			//
			// Ranking is where semantics are applied and extraction is not:
			// choosing the window would mean embedding several candidate spans
			// per fragment, which is tens of model calls inside a compile. The
			// window still comes from the lexical ladder, so a semantically
			// relevant fragment with no lexical anchor is kept whole-ish rather
			// than aimed precisely.
			current.relevance += int(semanticRelevanceWeight * hint.Score)
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

// hintedExcerpt prefers the passage a semantic scorer pointed at, and falls
// back to the lexical ladder when there is no hint.
//
// A hint is the only thing in this pipeline that can find a fact stated in
// words the goal does not use. Lexical matching cannot, by construction; and
// ranking without a position was measured keeping the right fragment while
// still discarding the answer inside it.
func hintedExcerpt(content, focus string, maxRunes int, hint SemanticHint) string {
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	if hint.End > hint.Start && hint.Start >= 0 && hint.Start < len(runes) {
		end := hint.End
		if end > len(runes) {
			end = len(runes)
		}
		// Anchor the window at the passage's start rather than its midpoint.
		// Centring was tried and measured wrong: a chunk is 500 runes and the
		// fact is often at its beginning, so a 360-rune window centred on the
		// chunk's middle opens after the fact and misses it. Starting where the
		// matching chunk starts is what guarantees the passage is included.
		start := hint.Start
		if start < 0 {
			start = 0
		}
		if end-start < maxRunes {
			// Room to spare: pull back a little so the sentence leading into
			// the passage comes with it.
			if lead := (maxRunes - (end - start)) / 2; start > lead {
				start -= lead
			} else {
				start = 0
			}
		}
		stop := start + maxRunes
		if stop > len(runes) {
			stop, start = len(runes), len(runes)-maxRunes
		}
		excerpt := string(runes[start:stop])
		if start > 0 {
			excerpt = "… " + excerpt
		}
		if stop < len(runes) {
			excerpt += " …"
		}
		return excerpt
	}
	return focusedExcerpt(content, focus, maxRunes)
}

// focusedExcerpt keeps the span of content that carries the most of the focus.
//
// It centres on the window with the densest coverage of the focus's terms
// rather than on any single term. Centring on the longest term seemed
// reasonable and measured badly: a goal's most distinctive word often appears
// somewhere other than the passage that answers it, so the window opened in the
// wrong place. Across the task corpus that left facts stated in the question's
// own words reachable only 62% of the time when they sat mid-message -- the
// case relevance ranking exists to rescue.
//
// When no term of the focus appears at all, both ends are kept. With nothing to
// centre on there is no reason to prefer one span, and the wider net is safer.
func focusedExcerpt(content, focus string, maxRunes int) string {
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	terms := focusTermsPresent(content, focus)
	if len(terms) == 0 {
		// No whole word of the focus appears. For Thai and other unspaced
		// scripts that is the normal case rather than the exception: Terms
		// splits on whitespace, so "หมายเลขแผนงานที่ตกลงกันไว้คืออะไร" is one
		// enormous word that matches nothing, and the trigram path is the only
		// way in. Falling back to both ends here left a fact stated in the
		// question's own words reachable just 62% of the time when it sat
		// mid-message -- in Thai, which is the language this system is used in.
		if window, ok := densestTrigramWindow(content, focus, maxRunes); ok {
			return window
		}
		return evenSlices(content, maxRunes)
	}
	lowered := strings.ToLower(content)
	// Every position a focus term starts at, in runes.
	var marks []int
	for _, term := range terms {
		for offset := 0; ; {
			index := strings.Index(lowered[offset:], term)
			if index < 0 {
				break
			}
			marks = append(marks, len([]rune(lowered[:offset+index])))
			offset += index + len(term)
		}
	}
	// The window centred on the mark that covers the most other marks.
	best, bestCount := marks[0], 0
	for _, mark := range marks {
		start, end := mark-maxRunes/2, mark+maxRunes/2
		count := 0
		for _, other := range marks {
			if other >= start && other <= end {
				count++
			}
		}
		if count > bestCount {
			best, bestCount = mark, count
		}
	}
	start := best - maxRunes/2
	if start < 0 {
		start = 0
	}
	end := start + maxRunes
	if end > len(runes) {
		end, start = len(runes), len(runes)-maxRunes
	}
	excerpt := string(runes[start:end])
	if start > 0 {
		excerpt = "… " + excerpt
	}
	if end < len(runes) {
		excerpt += " …"
	}
	return excerpt
}

// evenSlices samples a fragment at its start, middle and end.
//
// It replaces keeping only the two ends for the case where nothing in the text
// tells us where to look. headTail was built for a compactor that ranked by
// position, and it encodes the assumption that what matters is at an edge --
// which is exactly the assumption that lost a third of the facts it was asked
// about. When a fragment is known to be about the work but not where, sampling
// across it is the honest shape: three windows instead of two, same budget.
func evenSlices(content string, maxRunes int) string {
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	const slices = 3
	width := maxRunes / slices
	if width < 1 {
		return headTail(content, maxRunes)
	}
	var out strings.Builder
	for index := range slices {
		start := index * (len(runes) - width) / (slices - 1)
		if index > 0 {
			out.WriteString(" … ")
		}
		out.WriteString(string(runes[start : start+width]))
	}
	return out.String()
}

// focusTermsPresent returns the focus terms that actually appear in content,
// lowercased. Short terms and trigrams are skipped: "the" locates nothing, and
// a trigram match is what the relevance score is for, not what a window should
// be aimed at.
func focusTermsPresent(content, focus string) []string {
	words, _ := textmatch.Terms(strings.ToLower(focus))
	lowered := strings.ToLower(content)
	var present []string
	for term := range words {
		if strings.HasPrefix(term, "tri:") || len([]rune(term)) < 3 {
			continue
		}
		if strings.Contains(lowered, term) {
			present = append(present, term)
		}
	}
	sort.Strings(present)
	return present
}

// densestTrigramWindow finds the span of content sharing the most trigrams with
// the focus. It is how an unspaced script gets a focused excerpt at all.
//
// Windows are stepped rather than evaluated at every rune: a fact worth keeping
// is a sentence, not a character, and scoring 10,000 offsets to place a 500-rune
// window buys nothing but time.
// semanticRelevanceWeight scales a similarity in [0,1] onto the same range the
// lexical score uses, where a strong word match contributes 4 per term and a
// full trigram overlap 20. A confident semantic match is worth about as much as
// a good lexical one -- enough to save a fragment lexical ranking would drop,
// not enough to bury an exact match.
const semanticRelevanceWeight = 24

// focusedWindowFloor is the share of the focus's trigrams a window must carry
// before it is preferred over keeping both ends. See densestTrigramWindow.
const focusedWindowFloor = 0.30

func densestTrigramWindow(content, focus string, maxRunes int) (string, bool) {
	_, focusGrams := textmatch.Terms(strings.ToLower(focus))
	if len(focusGrams) == 0 {
		return "", false
	}
	runes := []rune(strings.ToLower(content))
	if len(runes) <= maxRunes {
		return content, true
	}
	step := maxRunes / 4
	if step < 1 {
		step = 1
	}
	starts := []int{}
	for start := 0; start+maxRunes <= len(runes); start += step {
		starts = append(starts, start)
	}
	// The stepped loop stops before the end, so the last window never gets
	// scanned. A fact in the final sentence of a long message was invisible to
	// this search until the tail was added explicitly -- which showed up as a
	// whole cell of the corpus going from fully reachable to not reachable at
	// all.
	if tail := len(runes) - maxRunes; tail > 0 && (len(starts) == 0 || starts[len(starts)-1] != tail) {
		starts = append(starts, tail)
	}
	best, bestShared := -1, 0
	for _, start := range starts {
		_, grams := textmatch.Terms(string(runes[start : start+maxRunes]))
		if shared := textmatch.Overlap(focusGrams, grams); shared > bestShared {
			best, bestShared = start, shared
		}
	}
	// Only override the positional default when relevance actually has
	// something to say. Measured on this corpus: a fact stated in the
	// question's own words puts 0.48 of the focus's trigrams inside the best
	// window, one stated in different words 0.15, and a window that never
	// covers the fact 0.11. Below the threshold the signal is indistinguishable
	// from background similarity between two pieces of Thai prose, and keeping
	// both ends is the safer bet.
	if best < 0 || float64(bestShared)/float64(len(focusGrams)) < focusedWindowFloor {
		return "", false
	}
	original := []rune(content)
	end := best + maxRunes
	if end > len(original) {
		end = len(original)
	}
	excerpt := string(original[best:end])
	if best > 0 {
		excerpt = "… " + excerpt
	}
	if end < len(original) {
		excerpt += " …"
	}
	return excerpt, true
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

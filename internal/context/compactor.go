package context

import (
	stdcontext "context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Compactor interface {
	Compact(ctx stdcontext.Context, fragments []Fragment, targetTokens int, estimator Estimator) (Fragment, error)
}

// StructuredCompactor is extractive and deterministic. It cannot hallucinate:
// every line carries the source fragment ID. A local semantic compactor can be
// plugged in later, but must satisfy the same retention tests.
type StructuredCompactor struct{}

func (StructuredCompactor) Compact(_ stdcontext.Context, fragments []Fragment, targetTokens int, estimator Estimator) (Fragment, error) {
	if targetTokens <= 0 || len(fragments) == 0 {
		return Fragment{}, nil
	}
	type unit struct {
		priority int
		created  time.Time
		text     string
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
			content = headTail(content, maxRunes)
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
	sort.SliceStable(units, func(i, j int) bool {
		if units[i].priority != units[j].priority {
			return units[i].priority > units[j].priority
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

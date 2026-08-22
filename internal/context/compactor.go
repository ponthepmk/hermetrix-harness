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
	for _, item := range selected {
		body.WriteString(item.text)
		body.WriteByte('\n')
	}
	return Fragment{ID: "checkpoint:extractive", Kind: KindCheckpoint, Scope: "session",
		Provenance: "hermetrix:structured-compactor-v1", Trust: "derived", Version: "v1",
		Priority: 85, CacheClass: "rolling", Content: strings.TrimSpace(body.String()), CreatedAt: checkpointTime}, nil
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

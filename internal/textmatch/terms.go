// Package textmatch turns text into the units deterministic retrieval compares.
//
// It exists because the codebase had two tokenizers with the same name and
// different behaviour. internal/skills/analyzer.go emitted character trigrams
// for non-ASCII scripts; internal/agent/service.go did not, and split on
// whitespace instead. Thai does not put spaces between words, so on the agent's
// path an entire Thai phrase became one term and matched only a byte-identical
// phrase. Measured against a Thai-language Skill:
//
//	"ปัดเศษสตางค์"                        0 hits
//	"การปัดเศษเงิน"                       0 hits
//	"ปัดเศษเงินไทยเป็นสตางค์แบบครึ่งขึ้น"  1 hit   (exactly the summary)
//
// The blind tokenizer was the one on the user-facing path: session skill
// preselection, skill_search, and the denominator of the skill-retrieval
// metric all use it. A Thai turn was therefore excluded from that denominator,
// so the metric reported no missed retrieval opportunities for the language the
// product is primarily written for.
package textmatch

import (
	"regexp"
	"strings"
	"unicode"
)

var wordPattern = regexp.MustCompile(`[a-z0-9][a-z0-9_-]*`)

// GramPrefix marks a character trigram so it can never collide with a word.
const GramPrefix = "tri:"

// Terms splits text into two kinds of unit, because they do not carry the same
// amount of information. An ASCII word is a whole morpheme and a single match
// is real evidence. A character trigram from an unspaced script is a fragment;
// one match is noise and only the proportion that matches means anything.
// Callers score them separately for that reason.
func Terms(text string) (words, grams map[string]bool) {
	normalized := normalize(text)
	words = map[string]bool{}
	for _, word := range wordPattern.FindAllString(normalized, -1) {
		add(words, word)
		// A canonical name is a compound: satang-rounding should be findable by
		// "rounding" alone. The joined form is kept too, so an exact hit on the
		// whole name still scores.
		if strings.ContainsAny(word, "-_") {
			for _, part := range strings.FieldsFunc(word, func(r rune) bool { return r == '-' || r == '_' }) {
				add(words, part)
			}
		}
	}
	grams = map[string]bool{}
	for _, field := range strings.Fields(normalized) {
		runes := []rune(field)
		if len(runes) < 3 || !containsNonASCII(field) {
			continue
		}
		for i := 0; i+3 <= len(runes); i++ {
			grams[GramPrefix+string(runes[i:i+3])] = true
		}
	}
	return words, grams
}

// Union is the single set the duplicate-analyzer compares. It keeps the
// analyzer's existing calibration by producing exactly what its own tokenizer
// produced.
func Union(text string) map[string]bool {
	words, grams := Terms(text)
	out := make(map[string]bool, len(words)+len(grams))
	for term := range words {
		out[term] = true
	}
	for term := range grams {
		out[term] = true
	}
	return out
}

// Overlap counts the terms two sets share.
func Overlap(left, right map[string]bool) int {
	if len(right) < len(left) {
		left, right = right, left
	}
	count := 0
	for term := range left {
		if right[term] {
			count++
		}
	}
	return count
}

func add(set map[string]bool, term string) {
	if len(term) > 2 {
		set[term] = true
	}
}

func normalize(text string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			out.WriteRune(r)
		} else {
			out.WriteByte(' ')
		}
	}
	return out.String()
}

func containsNonASCII(value string) bool {
	for _, r := range value {
		if r > 127 {
			return true
		}
	}
	return false
}

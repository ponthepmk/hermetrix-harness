package taskcoord

import (
	"errors"
	"sort"
	"strings"
	"unicode"
)

var proposalStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true, "this": true,
	"that": true, "into": true, "only": true, "then": true, "when": true, "must": true,
	"file": true, "files": true, "test": true, "tests": true, "func": true, "return": true,
	"package": true, "error": true, "string": true, "true": true, "false": true, "nil": true,
	"source": true, "current": true, "step": true, "implementation": true, "implement": true,
	"correct": true, "change": true, "changes": true, "fix": true,
}

func isUnicodeTermRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) || r == '_'
}

func proposalAnchorTerms(text string) ([]string, error) {
	lower := strings.ToLower(text)
	terms := make(map[string]bool)

	for _, part := range strings.FieldsFunc(lower, func(r rune) bool {
		return !isUnicodeTermRune(r)
	}) {
		if len([]rune(part)) >= 3 && !proposalStopwords[part] {
			terms[part] = true
		}
	}

	for _, anchor := range proposalQuotedAnchors(lower) {
		terms[anchor] = true
	}

	out := make([]string, 0, len(terms))
	for term := range terms {
		out = append(out, term)
	}
	if len(out) > 128 {
		return nil, errors.New("too many anchor terms; narrow the step")
	}
	sort.Strings(out)
	return out, nil
}

func proposalRegionScore(text string, terms []string) int {
	lower := strings.ToLower(text)
	tokens := make(map[string]bool)
	for _, part := range strings.FieldsFunc(lower, func(r rune) bool {
		return !isUnicodeTermRune(r)
	}) {
		tokens[part] = true
	}

	score := 0
	for _, term := range terms {
		if isIdentifierTerm(term) {
			if tokens[term] {
				score++
			}
		} else if strings.Contains(lower, term) {
			score += 50
		}
	}
	return score
}

func isIdentifierTerm(term string) bool {
	if term == "" {
		return false
	}
	for _, r := range term {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r) && r != '_' {
			return false
		}
	}
	return true
}

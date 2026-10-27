// Package projectbrain provides a small, deterministic fallback helper for
// Project Brain query shaping. It extracts Latin identifier tokens from mixed
// script user input so that non-English questions can still be routed to the
// existing lexical retriever without inventing translations or domain terms.
package projectbrain

import (
	"strings"
)

// fallbackQuery returns a bounded, deterministic query built from the original
// user input. It scans at most 4096 runes, extracts Latin tokens of the form
// [A-Za-z][A-Za-z0-9_]*, and prefers distinct identifier tokens containing an
// underscore (up to two). If no such tokens exist, it falls back to the existing
// focusedQuery over the extracted Latin words. The result is at most two tokens,
// at most 512 UTF-8 bytes, deduplicated case-insensitively, and empty when no
// useful Latin token remains.
func fallbackQuery(original string) string {
	// Bound the input to 4096 runes to avoid unbounded scanning.
	if len([]rune(original)) > 4096 {
		original = string([]rune(original)[:4096])
	}

	// Extract Latin tokens [A-Za-z][A-Za-z0-9_]* across mixed-script text.
	latinTokens := extractLatinTokens(original)

	// Prefer distinct identifier tokens containing underscore (up to two).
	underscoreTokens := make([]string, 0, 2)
	seen := make(map[string]bool)
	for _, token := range latinTokens {
		if !strings.Contains(token, "_") {
			continue
		}
		key := strings.ToLower(token)
		if seen[key] {
			continue
		}
		seen[key] = true
		underscoreTokens = append(underscoreTokens, token)
		if len(underscoreTokens) == 2 {
			break
		}
	}
	if len(underscoreTokens) > 0 {
		return boundedQuery(strings.Join(underscoreTokens, " "))
	}

	// Otherwise filter existing queryFiller and use up to two other tokens.
	filtered := make([]string, 0, 2)
	for _, token := range latinTokens {
		key := strings.ToLower(token)
		if queryFiller[key] || seen[key] {
			continue
		}
		seen[key] = true
		filtered = append(filtered, token)
		if len(filtered) == 2 {
			break
		}
	}
	if len(filtered) > 0 {
		return boundedQuery(strings.Join(filtered, " "))
	}

	return ""
}

// extractLatinTokens extracts all Latin tokens matching [A-Za-z][A-Za-z0-9_]*
// from the input string. It scans the entire input and returns tokens in order.
func extractLatinTokens(input string) []string {
	var tokens []string
	i := 0
	n := len(input)
	for i < n {
		c := input[i]
		if isLatinLetter(c) {
			start := i
			i++
			for i < n && (isLatinLetter(input[i]) || isLatinDigit(input[i]) || input[i] == '_') {
				i++
			}
			tokens = append(tokens, input[start:i])
		} else {
			i++
		}
	}
	return tokens
}

// isLatinLetter reports whether c is an ASCII Latin letter.
func isLatinLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isLatinDigit reports whether c is an ASCII digit.
func isLatinDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

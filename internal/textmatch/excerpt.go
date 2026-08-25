package textmatch

import "strings"

// Excerpt returns a bounded window of content centred on where query matches.
//
// The first version used the same head-and-tail trim the compactor uses, and a
// test caught what that means: searching a 10,000-rune message for a fact in
// its middle returned a hit whose matching text had been cut out. A search
// result that omits what was searched for is worse than no result -- it looks
// like an answer.
//
// With no match to centre on -- the hit came from term or trigram overlap
// rather than a substring -- the head is the right window, because that is
// where a message states what it is about.
func Excerpt(content, query string, max int) string {
	runes := []rune(content)
	if len(runes) <= max {
		return content
	}
	index := strings.Index(strings.ToLower(content), strings.ToLower(strings.TrimSpace(query)))
	if index < 0 {
		return boundedText(content, max)
	}
	// Convert the byte offset to a rune offset before windowing: Thai is three
	// bytes per character, so slicing runes by a byte index lands mid-word.
	start := len([]rune(content[:index])) - max/2
	if start < 0 {
		start = 0
	}
	end := start + max
	if end > len(runes) {
		end, start = len(runes), len(runes)-max
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

// boundedText trims to max runes, keeping the head. Used when there is nothing
// to centre on.
func boundedText(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return strings.TrimSpace(string(runes[:max])) + " \u2026"
}

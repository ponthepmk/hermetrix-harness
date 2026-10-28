package product

import (
	"strings"
	"unicode"
)

// manifestQueryPrefixes tokenizes full prose and returns unique valid
// relative path tokens, in query order, without parent prefixes.
func manifestQueryPrefixes(query string) []string {
	if query == "" {
		return nil
	}
	var out []string
	seen := make(map[string]struct{})
	for _, token := range strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r) &&
			r != '.' && r != '_' && r != '-' && r != ':' && r != '/' && r != '\\'
	}) {
		if strings.HasSuffix(token, "/") {
			token = strings.TrimSuffix(token, "/")
		}
		if strings.HasSuffix(token, ".") {
			token = strings.TrimSuffix(token, ".")
		}
		if !strings.ContainsRune(token, '/') {
			continue
		}
		if strings.HasPrefix(token, "/") || strings.ContainsRune(token, ':') ||
			strings.ContainsRune(token, '\\') {
			continue
		}
		valid := true
		for _, part := range strings.Split(token, "/") {
			if part == "" || part == "." || part == ".." {
				valid = false
				break
			}
			for _, r := range part {
				if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r) &&
					r != '.' && r != '_' && r != '-' {
					valid = false
					break
				}
			}
			if !valid {
				break
			}
		}
		if !valid {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

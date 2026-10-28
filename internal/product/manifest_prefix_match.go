package product

import "strings"

// manifestPrefixMatch returns the index of the longest prefix in prefixes
// that exactly matches a slash boundary of path, or -1 if none matches.
// Empty prefixes are ignored; ties choose the first prefix.
func manifestPrefixMatch(path string, prefixes []string) int {
	best := -1
	bestLen := 0
	for i, p := range prefixes {
		if p == "" {
			continue
		}
		if !strings.HasPrefix(path, p+"/") {
			continue
		}
		if len(p) > bestLen {
			best = i
			bestLen = len(p)
		}
	}
	return best
}

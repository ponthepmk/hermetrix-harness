package taskcoord

import "strings"

func proposalQuotedAnchors(text string) []string {
	text = strings.ToLower(text)
	var out []string
	for _, d := range []rune{96, 34} {
		parts := strings.Split(text, string(d))
		for i := 1; i+1 < len(parts); i += 2 {
			s := strings.TrimSpace(parts[i])
			if s == "" || len([]rune(s)) > 256 {
				continue
			}
			out = append(out, s)
		}
	}
	return out
}

package product

import (
	"context"
	"sort"
	"strings"
	"unicode"
)

type MemoryMatch struct {
	MemoryID     string   `json:"memory_id"`
	ScopeKind    string   `json:"scope_kind"`
	ScopeRef     string   `json:"scope_ref"`
	MemoryKind   string   `json:"memory_kind"`
	Snippet      string   `json:"snippet"`
	Score        int      `json:"score"`
	MatchedTerms []string `json:"matched_terms"`
}

// RetrieveMemories deterministically ranks explicit active user memories. It
// never calls a model and bounds both the result count and returned content.
func (s *Service) RetrieveMemories(ctx context.Context, projectID, query string, limit int) ([]MemoryMatch, error) {
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	all, err := s.ListMemories(ctx, "", "")
	if err != nil {
		return nil, err
	}
	terms := retrievalTerms(query)
	matches := make([]MemoryMatch, 0)
	for _, memory := range all {
		if memory.State != "active" || memory.Source != "user" ||
			(memory.ScopeKind == "project" && memory.ScopeRef != projectID) {
			continue
		}
		content := strings.ToLower(memory.MemoryKind + " " + memory.Content)
		matched := []string{}
		for _, term := range terms {
			if strings.Contains(content, term) {
				matched = append(matched, term)
			}
		}
		if len(matched) == 0 {
			continue
		}
		snippet := []rune(memory.Content)
		if len(snippet) > 1200 {
			snippet = append(snippet[:1200], '…')
		}
		matches = append(matches, MemoryMatch{MemoryID: memory.ID, ScopeKind: memory.ScopeKind, ScopeRef: memory.ScopeRef,
			MemoryKind: memory.MemoryKind, Snippet: string(snippet), Score: len(matched), MatchedTerms: matched})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].MemoryID < matches[j].MemoryID
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func retrievalTerms(value string) []string {
	seen := map[string]bool{}
	terms := []string{}
	for _, term := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		if len([]rune(term)) < 3 || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
		if len(terms) == 64 {
			break
		}
	}
	return terms
}

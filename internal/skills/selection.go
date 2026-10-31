package skills

import (
	"math"
	"sort"
	"strings"

	"hermetrix-harness/internal/textmatch"
)

// gramWeight is the most a trigram overlap can contribute, matched to the bonus
// an exact name hit earns.
//
// Trigrams are scored as a proportion of the shorter side rather than as a raw
// count. Measured on a long Thai goal about rounding satang, a terse Skill
// summary saying exactly that shared 7 trigrams while a long unrelated summary
// about sales-tax reports shared 17 -- so a raw count buries the right Skill
// under a wordy wrong one. Against the shorter side those become 40 and 14.
const gramWeight = 40

// Binding is the shared skill selection binding. It carries the same fields
// and JSON contract as the agent's session skill binding.
type Binding struct {
	SkillID         string `json:"skill_id"`
	VersionID       string `json:"version_id"`
	CanonicalName   string `json:"canonical_name"`
	Summary         string `json:"summary"`
	Pinned          bool   `json:"pinned"`
	SelectionReason string `json:"selection_reason,omitempty"`
}

// bindingKey returns the stable key for a binding, used to look up semantic
// scores. It is the SkillID and VersionID joined by a slash.
func bindingKey(b Binding) string {
	return b.SkillID + "/" + b.VersionID
}

// RankBindings scores the catalog against a goal, optionally adding what
// meaning-similarity contributed. The semantic bonus is summed with the lexical
// score rather than replacing it, because the two are blind to different
// things: a vector crosses a paraphrase and an exact canonical name is a
// substring a vector only approximates.
func RankBindings(goal string, catalog []Binding, semantic map[string]int) []Binding {
	type scored struct {
		binding Binding
		score   int
	}
	query := strings.ToLower(strings.TrimSpace(goal))
	queryWords, queryGrams := textmatch.Terms(query)
	var candidates []scored
	for _, binding := range catalog {
		score := 0
		reasons := []string{}
		if binding.Pinned {
			score += 100
			reasons = append(reasons, "pinned in this session")
		}
		name := strings.ToLower(strings.ReplaceAll(binding.CanonicalName, "-", " "))
		if query != "" && (strings.Contains(query, name) || strings.Contains(name, query)) {
			score += 40
			reasons = append(reasons, "name matches the goal")
		}
		candidateWords, candidateGrams := textmatch.Terms(name + " " + strings.ToLower(binding.Summary))
		wordMatched := false
		for term := range candidateWords {
			if queryWords[term] {
				score += 4
				wordMatched = true
			}
		}
		if wordMatched {
			reasons = append(reasons, "terms overlap with the goal")
		}
		// Thai and other unspaced scripts reach the scorer only through this
		// branch. Without it a Thai query matched nothing short of the summary
		// repeated verbatim.
		if shared := textmatch.Overlap(queryGrams, candidateGrams); shared > 0 {
			// The shorter side is whichever of goal and summary is more specific,
			// so this reads as "how much of the specific one is covered".
			smaller := len(queryGrams)
			if len(candidateGrams) < smaller {
				smaller = len(candidateGrams)
			}
			score += int(math.Round(gramWeight * float64(shared) / float64(smaller)))
			reasons = append(reasons, "text resembles the goal")
		}
		semanticScore := semantic[bindingKey(binding)]
		score += semanticScore
		if semanticScore > 0 {
			reasons = append(reasons, "meaning resembles the goal")
		}
		if score > 0 {
			binding.SelectionReason = strings.Join(reasons, "; ")
			candidates = append(candidates, scored{binding: binding, score: score})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].binding.CanonicalName < candidates[j].binding.CanonicalName
	})
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}
	selected := make([]Binding, 0, len(candidates))
	for _, candidate := range candidates {
		selected = append(selected, candidate.binding)
	}
	return selected
}

// AppliesToProject reports whether a skill applies to the given project.
func AppliesToProject(item Skill, projectID string) bool {
	switch item.ScopeKind {
	case "user":
		return item.ScopeRef == ""
	case "workspace":
		return projectID != "" && item.ScopeRef == projectID
	default:
		// This runtime has no durable agent-scope identity to bind these to.
		return false
	}
}

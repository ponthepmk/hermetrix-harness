package selfimprove

import "fmt"

type Outcome struct {
	CaseID           string `json:"case_id"`
	Trial            int    `json:"trial"`
	Condition        string `json:"condition"`
	SessionID        string `json:"session_id"`
	TurnID           string `json:"turn_id"`
	ProviderRevision string `json:"provider_revision"`
	CatalogHash      string `json:"catalog_hash"`
	Answer           string `json:"answer"`
	Tokens           int    `json:"tokens"`
	DurationMS       int64  `json:"duration_ms"`
	Score            Score  `json:"score"`
	Error            string `json:"error,omitempty"`
}

type Comparison struct {
	Verdict         string   `json:"verdict"`
	BaselinePassed  int      `json:"baseline_passed"`
	CandidatePassed int      `json:"candidate_passed"`
	Total           int      `json:"total"`
	BaselineTokens  int      `json:"baseline_tokens"`
	CandidateTokens int      `json:"candidate_tokens"`
	Improvements    int      `json:"improvements"`
	Regressions     []string `json:"regressions"`
	Reasons         []string `json:"reasons"`
	AutoPromoted    bool     `json:"auto_promoted"`
}

// Compare recomputes scores from answers and the host's frozen cases. No model
// score, changed case, missing pair, swapped win/loss or equal score can pass.
func Compare(outcomes []Outcome, repeats int) Comparison {
	r := Comparison{Verdict: "keep_current", Reasons: []string{}, Regressions: []string{}}
	if repeats < 2 || repeats > 3 {
		r.Reasons = append(r.Reasons, "two or three repeats required")
		return r
	}
	pairs := map[string]Outcome{}
	sessions, turns := map[string]bool{}, map[string]bool{}
	for _, o := range outcomes {
		key := fmt.Sprintf("%s/%d/%s", o.CaseID, o.Trial, o.Condition)
		if _, exists := pairs[key]; exists {
			r.Reasons = append(r.Reasons, "duplicate outcome "+key)
		}
		pairs[key] = o
		if sessions[o.SessionID] || turns[o.TurnID] {
			r.Reasons = append(r.Reasons, "conditions reused a session or turn")
		}
		sessions[o.SessionID], turns[o.TurnID] = true, true
	}
	if len(outcomes) != len(HeldoutCases())*repeats*2 {
		r.Reasons = append(r.Reasons, "incomplete or extra outcomes")
	}
	revision, catalog := "", ""
	for _, c := range HeldoutCases() {
		for trial := 1; trial <= repeats; trial++ {
			key := fmt.Sprintf("%s/%d/", c.ID, trial)
			base, bOK := pairs[key+"baseline"]
			cand, cOK := pairs[key+"candidate"]
			if !bOK || !cOK {
				r.Reasons = append(r.Reasons, "missing pair "+key)
				continue
			}
			for _, o := range []Outcome{base, cand} {
				if o.Error != "" || o.SessionID == "" || o.TurnID == "" || o.Tokens <= 0 || o.ProviderRevision == "" || o.CatalogHash == "" {
					r.Reasons = append(r.Reasons, "invalid run "+key+o.Condition)
				}
				if revision == "" {
					revision, catalog = o.ProviderRevision, o.CatalogHash
				}
				if revision != o.ProviderRevision || catalog != o.CatalogHash {
					r.Reasons = append(r.Reasons, "provider or Skill catalog changed")
				}
			}
			b, candidate := ScorePlan(c, base.Answer), ScorePlan(c, cand.Answer)
			r.BaselinePassed += b.Passed
			r.CandidatePassed += candidate.Passed
			r.Total += b.Total
			r.BaselineTokens += base.Tokens
			r.CandidateTokens += cand.Tokens
			if b.Error != "" || candidate.Error != "" {
				r.Reasons = append(r.Reasons, "invalid answer "+key)
			}
			for _, missing := range candidate.Missing {
				if !contains(b.Missing, missing) {
					r.Regressions = append(r.Regressions, key+missing)
				}
			}
			for _, missing := range b.Missing {
				if !contains(candidate.Missing, missing) {
					r.Improvements++
				}
			}
		}
	}
	if len(r.Regressions) > 0 {
		r.Reasons = append(r.Reasons, "one or more previously covered requirements regressed")
	}
	if r.CandidatePassed <= r.BaselinePassed {
		r.Reasons = append(r.Reasons, "no measured coverage improvement")
	}
	if r.CandidatePassed != r.Total {
		r.Reasons = append(r.Reasons, "candidate still omits required planning capabilities")
	}
	if r.CandidateTokens > r.BaselineTokens*3/2 {
		r.Reasons = append(r.Reasons, "candidate exceeds 1.5x baseline token budget")
	}
	if len(r.Reasons) == 0 {
		r.Verdict = "review_candidate"
	}
	return r
}

func GuardCore(r Comparison, before, after CoreReceipt) Comparison {
	if !before.Passed || !after.Passed || before.SourceHash == "" || before.TestHash == "" || before.SourceHash != after.SourceHash || before.TestHash != after.TestHash {
		r.Verdict = "keep_current"
		r.Reasons = append(r.Reasons, "core checks failed or protected implementation/tests changed")
	}
	return r
}

package selfimprove

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func fullAnswer(c Case) string {
	raw, _ := json.Marshal(Plan{Roles: c.Required, Invariants: c.Rules})
	return string(raw)
}

func pairedOutcomes() []Outcome {
	var outcomes []Outcome
	for _, c := range HeldoutCases() {
		for trial := 1; trial <= 2; trial++ {
			for _, condition := range []string{"baseline", "candidate"} {
				answer := fullAnswer(c)
				if condition == "baseline" {
					var p Plan
					_ = json.Unmarshal([]byte(answer), &p)
					p.Invariants = p.Invariants[1:]
					raw, _ := json.Marshal(p)
					answer = string(raw)
				}
				id := fmt.Sprintf("%s/%d/%s", c.ID, trial, condition)
				outcomes = append(outcomes, Outcome{CaseID: c.ID, Trial: trial, Condition: condition, SessionID: "session-" + id, TurnID: "turn-" + id, ProviderRevision: "provider-fixed", CatalogHash: "catalog-fixed", Answer: answer, Tokens: 100})
			}
		}
	}
	return outcomes
}

func TestComparisonRequiresRealGainAndKeepsPerRequirementRegressionsVisible(t *testing.T) {
	good := pairedOutcomes()
	r := Compare(good, 2)
	if r.Verdict != "review_candidate" || r.Improvements != 6 || r.AutoPromoted {
		t.Fatalf("improvement: %+v", r)
	}
	// Aggregate scores still improve, but a candidate drops a custody requirement
	// that the baseline covered. The overall gain must never hide that regression.
	var p Plan
	_ = json.Unmarshal([]byte(good[1].Answer), &p)
	p.Roles["rider"] = p.Roles["rider"][1:]
	raw, _ := json.Marshal(p)
	good[1].Answer = string(raw)
	r = Compare(good, 2)
	if r.Verdict != "keep_current" || len(r.Regressions) != 1 || r.CandidatePassed <= r.BaselinePassed {
		t.Fatalf("hidden regression escaped: %+v", r)
	}
}

func TestComparisonRejectsIncompleteChangedEqualAndExpensiveTrials(t *testing.T) {
	for _, name := range []string{"equal", "missing", "duplicate", "provider", "catalog", "error", "cost", "forged-score", "extra"} {
		t.Run(name, func(t *testing.T) {
			outcomes := pairedOutcomes()
			switch name {
			case "equal":
				for i := 0; i < len(outcomes); i += 2 {
					outcomes[i].Answer = outcomes[i+1].Answer
				}
			case "missing":
				outcomes = outcomes[:len(outcomes)-1]
			case "duplicate":
				outcomes[1] = outcomes[0]
			case "provider":
				outcomes[1].ProviderRevision = "changed"
			case "catalog":
				outcomes[1].CatalogHash = "changed"
			case "error":
				outcomes[1].Error = "network timeout"
			case "cost":
				outcomes[1].Tokens = 10000
			case "forged-score":
				outcomes[1].Answer = "{}"
				outcomes[1].Score = Score{Passed: 1000, Total: 1000}
			case "extra":
				outcomes = append(outcomes, outcomes[0])
			}
			if r := Compare(outcomes, 2); r.Verdict != "keep_current" || len(r.Reasons) == 0 || r.AutoPromoted {
				t.Fatalf("bad trial allowed: %+v", r)
			}
		})
	}
}

func TestPlanRejectsUnauthorizedActionsAndNonJSONClaims(t *testing.T) {
	c := HeldoutCases()[0]
	if s := ScorePlan(c, fullAnswer(c)); s.Passed != s.Total || s.Error != "" {
		t.Fatalf("full plan: %+v", s)
	}
	for _, raw := range []string{fullAnswer(c) + " {}", "```json\n" + fullAnswer(c) + "\n```", `{"roles":{"customer":["manage_users"]},"invariants":[]}`, `{"roles":{},"invariants":[],"passed":true}`} {
		if s := ScorePlan(c, raw); s.Error == "" || s.Passed != 0 {
			t.Fatalf("invalid plan accepted: %+v", s)
		}
	}
}

func TestCoreGuardCannotUseStaleOrMissingProtectedEvidence(t *testing.T) {
	c := CoreReceipt{Passed: true, SourceHash: "source", TestHash: "tests"}
	for _, name := range []string{"failed", "source", "tests", "missing"} {
		t.Run(name, func(t *testing.T) {
			after := c
			switch name {
			case "failed":
				after.Passed = false
			case "source":
				after.SourceHash = "different"
			case "tests":
				after.TestHash = "different"
			case "missing":
				after.TestHash = ""
			}
			r := GuardCore(Compare(pairedOutcomes(), 2), c, after)
			if r.Verdict != "keep_current" || !strings.Contains(strings.Join(r.Reasons, " "), "core checks") {
				t.Fatalf("core protection failed: %+v", r)
			}
		})
	}
}

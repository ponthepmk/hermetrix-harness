package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A CorpusCase is one unit of completed work plus a human judgement about what
// should have come of it. The judgement is the point: without it a reviewer's
// output can be counted but not scored, and every Phase 8 number is a rate over
// a denominator nobody defined.
type CorpusCase struct {
	ID          string `json:"id"`
	TriggerKind string `json:"trigger_kind"`
	// Provenance is "driven" for a digest taken from work actually performed and
	// "synthetic" for one written to reach a family that driving did not produce.
	// Results are reported split by it, because a synthetic case proves the
	// reviewer handles the author's imagination and should never quietly carry a
	// gate on its own.
	Provenance string `json:"provenance"`
	// SourceReviewID ties a driven case back to the committed review it came
	// from, so a number in a report can be walked back to real events.
	SourceReviewID string `json:"source_review_id,omitempty"`
	Digest         Digest `json:"digest"`
	Label          Label  `json:"label"`
}

// Label is the ground truth. ShouldPropose is the whole denominator of the
// semantic-reviewer gate, and Rationale exists so a disagreement during audit is
// about a stated reason rather than a remembered one.
type Label struct {
	ShouldPropose bool   `json:"should_propose"`
	Rationale     string `json:"rationale"`
	LabeledBy     string `json:"labeled_by"`
	LabeledAt     string `json:"labeled_at"`
	// Audited records a second reader agreeing or disagreeing with the label.
	// Auditing a sample is how the corpus stops measuring one person's taste.
	Audited   string `json:"audited_by,omitempty"`
	AuditAgee *bool  `json:"audit_agreed,omitempty"`
}

// CorpusResult is the semantic-reviewer gate, computed rather than asserted.
type CorpusResult struct {
	Provenance string `json:"provenance"`
	Cases      int    `json:"cases"`
	// ShouldPropose is the denominator: cases a human said contain a procedure.
	ShouldPropose int `json:"should_propose"`
	// Proposed counts those where the reviewer produced a candidate that passed
	// its checks. ProposalRate is Proposed over ShouldPropose -- the gate wants
	// at least 60%.
	Proposed     int     `json:"proposed"`
	ProposalRate float64 `json:"proposal_rate"`
	// FalseProposals are cases a human said contain nothing and the reviewer
	// proposed anyway. The gate allows at most 10%.
	FalseProposals    int     `json:"false_proposals"`
	FalseProposalRate float64 `json:"false_proposal_rate"`
	// InventedEvidence counts proposals citing evidence the digest never carried.
	// The gate allows none, with no tolerance, because a proposal that cites
	// what it was not given is not a weak proposal but a fabricated one.
	InventedEvidence int      `json:"invented_evidence"`
	InventedIDs      []string `json:"invented_ids,omitempty"`
	// ReviewerErrors counts cases where the reviewer did not answer at all --
	// no parseable decision, or a proposal missing the parts that make it one.
	// They stay in the denominator, because a reviewer that could not answer did
	// not propose, but they are reported separately: recall limited by
	// availability and recall limited by judgement look identical in a single
	// rate and call for completely different fixes.
	ReviewerErrors int      `json:"reviewer_errors"`
	Verdict        string   `json:"verdict"`
	Failures       []string `json:"failures,omitempty"`
	// Repeats is how many independent readings this result is drawn from, and
	// the rates above are the worst of them.
	//
	// The reviewer is not deterministic. Temperature is already zero, so this is
	// the model rather than a sampling setting: measured directly, three of
	// twelve cases changed answer across five readings, every one of them a case
	// the label says holds a procedure, while negatives never moved once. Three
	// scorings of the same hundred cases returned 31, 34 and 30 proposals --
	// recall from 0.55 to 0.62, straddling the 0.60 floor.
	//
	// A gate settled by one reading of that passes or fails on which day it ran.
	// This one takes the worst reading, so passing means the corpus cleared the
	// bar every time rather than once.
	Repeats int `json:"repeats,omitempty"`
	// UnstableCases counts cases whose answer was not unanimous. Reported rather
	// than judged: it says how much of the rate is the reviewer's judgement and
	// how much is its variance.
	UnstableCases     int        `json:"unstable_cases,omitempty"`
	UnstableIDs       []string   `json:"unstable_ids,omitempty"`
	ProposalRateRange [2]float64 `json:"proposal_rate_range,omitempty"`
}

const (
	// CorpusProposalFloor, CorpusFalseProposalCeiling and the zero tolerance on
	// invented evidence are the plan's gate, restated where it can be computed.
	CorpusProposalFloor         = 0.60
	CorpusFalseProposalCeiling  = 0.10
	CorpusMinimumCasesPerFamily = 25
)

// LoadCorpus reads every case in a directory. It fails on a case without a
// label rather than skipping it: an unlabelled case silently dropped would
// shrink the denominator and flatter the result.
func LoadCorpus(dir string) ([]CorpusCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read corpus: %w", err)
	}
	cases := make([]CorpusCase, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var item CorpusCase
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&item); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if strings.TrimSpace(item.ID) == "" {
			return nil, fmt.Errorf("%s: case has no id", entry.Name())
		}
		if strings.TrimSpace(item.Label.LabeledBy) == "" {
			return nil, fmt.Errorf("%s: case is unlabelled; an unlabelled case would shrink the denominator", item.ID)
		}
		if item.Provenance != "driven" && item.Provenance != "synthetic" {
			return nil, fmt.Errorf("%s: provenance is %q, want driven or synthetic", item.ID, item.Provenance)
		}
		cases = append(cases, item)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return cases, nil
}

// FamilyCoverage counts cases per trigger family, which is the part of the
// prerequisite a rate cannot show: a corpus of a hundred cases from one family
// says nothing about the other three.
func FamilyCoverage(cases []CorpusCase) map[string]int {
	coverage := map[string]int{}
	for _, item := range cases {
		coverage[item.TriggerKind]++
	}
	return coverage
}

// Progress reports a case as it is reviewed. Scoring a hundred cases spends a
// model call each and took thirty-five minutes against a real gateway; a
// command that long with no output is indistinguishable from one that hung.
type Progress func(done, total int, caseID string)

// ScoreCorpus runs the reviewer over every case and computes the gate, split by
// provenance and again over everything.
func ScoreCorpus(ctx context.Context, reviewer Reviewer, cases []CorpusCase) ([]CorpusResult, error) {
	return ScoreCorpusWithProgress(ctx, reviewer, cases, nil)
}

// ScoreCorpusWithProgress is ScoreCorpus with a callback per case. Results are
// cached across the provenance splits so a case is reviewed once, not three
// times: reviewing the same case for "driven" and again for "all" would triple
// the cost and could disagree with itself.
func ScoreCorpusWithProgress(ctx context.Context, reviewer Reviewer, cases []CorpusCase,
	progress Progress) ([]CorpusResult, error) {
	return ScoreCorpusRepeated(ctx, reviewer, cases, 1, progress)
}

// ScoreCorpusRepeated reads the corpus several times and judges on the worst
// reading. One reading cannot settle a threshold the reviewer's own variance
// straddles.
func ScoreCorpusRepeated(ctx context.Context, reviewer Reviewer, cases []CorpusCase, repeats int,
	progress Progress) ([]CorpusResult, error) {
	if repeats < 1 {
		repeats = 1
	}
	rounds := make([]map[string]Decision, 0, repeats)
	total, done := len(cases)*repeats, 0
	for round := 0; round < repeats; round++ {
		decisions := make(map[string]Decision, len(cases))
		for _, item := range cases {
			decision, err := reviewWithRetry(ctx, reviewer, item)
			if err != nil {
				return nil, fmt.Errorf("review %s: %w", item.ID, err)
			}
			decisions[item.ID] = decision
			done++
			if progress != nil {
				progress(done, total, item.ID)
			}
		}
		rounds = append(rounds, decisions)
	}
	return worstOf(cases, rounds)
}

// worstOf scores every reading and keeps the one that did least well, carrying
// the range and the cases that did not answer the same way twice.
func worstOf(cases []CorpusCase, rounds []map[string]Decision) ([]CorpusResult, error) {
	perRound := make([][]CorpusResult, 0, len(rounds))
	for _, decisions := range rounds {
		results, err := scoreDecided(cases, decisions)
		if err != nil {
			return nil, err
		}
		perRound = append(perRound, results)
	}
	unstable := unstableCases(cases, rounds)
	worst := make([]CorpusResult, len(perRound[0]))
	for index := range worst {
		chosen := perRound[0][index]
		low, high := chosen.ProposalRate, chosen.ProposalRate
		for _, results := range perRound[1:] {
			candidate := results[index]
			if candidate.ProposalRate < low {
				low = candidate.ProposalRate
			}
			if candidate.ProposalRate > high {
				high = candidate.ProposalRate
			}
			if worseThan(candidate, chosen) {
				chosen = candidate
			}
		}
		chosen.Repeats = len(rounds)
		chosen.ProposalRateRange = [2]float64{low, high}
		for _, id := range unstable {
			if belongsTo(cases, id, chosen.Provenance) {
				chosen.UnstableCases++
				chosen.UnstableIDs = append(chosen.UnstableIDs, id)
			}
		}
		worst[index] = chosen
	}
	return worst, nil
}

// worseThan orders two readings the way the gate does: inventing evidence is
// worse than any rate, then lower recall, then more false proposals.
func worseThan(candidate, current CorpusResult) bool {
	if candidate.InventedEvidence != current.InventedEvidence {
		return candidate.InventedEvidence > current.InventedEvidence
	}
	if candidate.ProposalRate != current.ProposalRate {
		return candidate.ProposalRate < current.ProposalRate
	}
	return candidate.FalseProposalRate > current.FalseProposalRate
}

func unstableCases(cases []CorpusCase, rounds []map[string]Decision) []string {
	if len(rounds) < 2 {
		return nil
	}
	var unstable []string
	for _, item := range cases {
		first := proposedIn(rounds[0], item.ID)
		for _, decisions := range rounds[1:] {
			if proposedIn(decisions, item.ID) != first {
				unstable = append(unstable, item.ID)
				break
			}
		}
	}
	sort.Strings(unstable)
	return unstable
}

func proposedIn(decisions map[string]Decision, id string) bool {
	decision := decisions[id]
	return decision.Kind == "create" && decision.SuggestedSkill != nil
}

func belongsTo(cases []CorpusCase, id, provenance string) bool {
	for _, item := range cases {
		if item.ID == id {
			return provenance == "all" || item.Provenance == provenance
		}
	}
	return false
}

func scoreDecided(cases []CorpusCase, decisions map[string]Decision) ([]CorpusResult, error) {
	byProvenance := map[string][]CorpusCase{}
	for _, item := range cases {
		byProvenance[item.Provenance] = append(byProvenance[item.Provenance], item)
	}
	results := make([]CorpusResult, 0, 3)
	for _, provenance := range []string{"driven", "synthetic", "all"} {
		subset := byProvenance[provenance]
		if provenance == "all" {
			subset = cases
		}
		if len(subset) == 0 {
			continue
		}
		results = append(results, scoreSubset(provenance, subset, decisions))
	}
	return results, nil
}

func scoreSubset(provenance string, cases []CorpusCase, decisions map[string]Decision) CorpusResult {
	result := CorpusResult{Provenance: provenance, Cases: len(cases)}
	for _, item := range cases {
		decision := decisions[item.ID]
		proposed := decision.Kind == "create" && decision.SuggestedSkill != nil
		if decision.Unusable {
			result.ReviewerErrors++
		}
		if item.Label.ShouldPropose {
			result.ShouldPropose++
			if proposed {
				result.Proposed++
			} else if decision.Unusable {
				result.Failures = append(result.Failures,
					item.ID+": reviewer returned nothing usable, so this is not a judgement: "+decision.Reason)
			} else {
				result.Failures = append(result.Failures, item.ID+": missed a procedure the label says is there")
			}
		} else if proposed {
			result.FalseProposals++
			result.Failures = append(result.Failures, item.ID+": proposed from evidence the label says holds nothing")
		}
		if proposed {
			if invented := inventedEvidence(item.Digest, *decision.SuggestedSkill); len(invented) > 0 {
				result.InventedEvidence++
				result.InventedIDs = append(result.InventedIDs, item.ID)
				result.Failures = append(result.Failures,
					item.ID+": proposal cites evidence it was not given: "+strings.Join(invented, ", "))
			}
		}
	}
	if result.ShouldPropose > 0 {
		result.ProposalRate = round3(float64(result.Proposed) / float64(result.ShouldPropose))
	}
	if negatives := result.Cases - result.ShouldPropose; negatives > 0 {
		result.FalseProposalRate = round3(float64(result.FalseProposals) / float64(negatives))
	}
	result.Verdict = "passed"
	switch {
	case result.ShouldPropose == 0:
		// A corpus with no positive case cannot measure recall, and reporting a
		// rate over an empty denominator as a pass is how a gate becomes
		// decorative.
		result.Verdict = "insufficient_evidence"
	case result.InventedEvidence > 0:
		result.Verdict = "failed"
	case result.ProposalRate < CorpusProposalFloor:
		result.Verdict = "failed"
	case result.FalseProposalRate > CorpusFalseProposalCeiling:
		result.Verdict = "failed"
	}
	return result
}

// inventedEvidence reports identifiers a proposal cites that its digest never
// carried.
//
// The reviewer is handed a digest of references -- event IDs, skill versions,
// tool receipts -- and nothing else. A proposal whose text quotes an identifier
// outside that set did not read it anywhere; it produced it. That is a
// different failure from proposing weakly, and the gate gives it no tolerance,
// because a procedure justified by a receipt that does not exist cannot be
// checked by the person asked to approve it.
func inventedEvidence(digest Digest, suggested SuggestedSkill) []string {
	given := map[string]bool{}
	note := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		given[value] = true
		// A receipt is "event:<id>:<tool>:<status>"; the event alone is a
		// legitimate citation of the same thing.
		if parts := strings.SplitN(value, ":", 3); len(parts) >= 2 {
			given[parts[0]+":"+parts[1]] = true
		}
	}
	for _, group := range [][]string{digest.ToolReceipts, digest.SkillActivations,
		digest.UserCorrections, digest.Artifacts, digest.Decisions} {
		for _, value := range group {
			note(value)
		}
	}
	seen := map[string]bool{}
	var invented []string
	for _, match := range identifierPattern.FindAllString(suggested.Markdown+"\n"+suggested.Reason, -1) {
		reference := trailingPunctuation.ReplaceAllString(match, "")
		if given[reference] || seen[reference] {
			continue
		}
		seen[reference] = true
		invented = append(invented, reference)
	}
	sort.Strings(invented)
	return invented
}

// identifierPattern matches the reference shapes a digest is made of. Prose
// about a tool is not a citation; "event:event_a1b2" is.
//
// Dots and colons appear inside these identifiers -- a skill reference is
// skill:<id>@<version>, a receipt is event:<id>:<tool>:<status> -- so the class
// has to admit them, and a citation at the end of a sentence then swallows the
// full stop. "event:event_real." is not in the digest and would be reported as
// invented, which is the accusation this check exists to make carefully.
var identifierPattern = regexp.MustCompile(`\b(?:event|skill|artifact|blob):[A-Za-z0-9_@.:-]+`)

// trailingPunctuation is what a citation picks up from the prose around it.
var trailingPunctuation = regexp.MustCompile(`[.:,;]+$`)

func round3(value float64) float64 {
	return float64(int(value*1000+0.5)) / 1000
}

// reviewRetries and reviewRetryDelay bound how long a transient provider fault
// can hold up a scoring run.
const reviewRetries = 3

// reviewRetryDelay is a variable so tests can shrink it. Sleeping for real in a
// unit test buys nothing and costs every later run: the three retry tests took
// thirty-six seconds of wall clock to assert arithmetic about attempts.
var reviewRetryDelay = 4 * time.Second

// reviewWithRetry retries a review that failed on the way to the provider.
//
// The harness does not retry tool calls -- an MCP result carries
// automatic_retry:false, because repeating a call that may have already changed
// something is not a safe thing to do on the caller's behalf. A review is the
// opposite: it reads a digest and returns a judgement, it changes nothing, and
// the same digest asked twice is the same question.
//
// The reason to bother is measured. A scoring run of two hundred reviews spent
// fifty-five minutes and was thrown away by one 502 from the gateway near the
// end. An hour of model calls is not something to lose to a transient fault.
//
// A decision the reviewer actually made is never retried, including one it
// failed to phrase -- that is an answer, and Unusable already records it.
func reviewWithRetry(ctx context.Context, reviewer Reviewer, item CorpusCase) (Decision, error) {
	var err error
	for attempt := 0; attempt <= reviewRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return Decision{}, ctx.Err()
			case <-time.After(reviewRetryDelay * time.Duration(attempt)):
			}
		}
		var decision Decision
		decision, err = reviewer.Review(ctx, item.Digest)
		if err == nil {
			return decision, nil
		}
		if ctx.Err() != nil {
			return Decision{}, ctx.Err()
		}
	}
	return Decision{}, fmt.Errorf("after %d attempts: %w", reviewRetries+1, err)
}

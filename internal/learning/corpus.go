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

// ScoreCorpus runs the reviewer over every case and computes the gate, split by
// provenance and again over everything.
func ScoreCorpus(ctx context.Context, reviewer Reviewer, cases []CorpusCase) ([]CorpusResult, error) {
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
		result, err := scoreSubset(ctx, reviewer, provenance, subset)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func scoreSubset(ctx context.Context, reviewer Reviewer, provenance string, cases []CorpusCase) (CorpusResult, error) {
	result := CorpusResult{Provenance: provenance, Cases: len(cases)}
	for _, item := range cases {
		decision, err := reviewer.Review(ctx, item.Digest)
		if err != nil {
			return CorpusResult{}, fmt.Errorf("review %s: %w", item.ID, err)
		}
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
	return result, nil
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
	for _, reference := range identifierPattern.FindAllString(suggested.Markdown+"\n"+suggested.Reason, -1) {
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
var identifierPattern = regexp.MustCompile(`\b(?:event|skill|artifact|blob):[A-Za-z0-9_@.:-]+`)

func round3(value float64) float64 {
	return float64(int(value*1000+0.5)) / 1000
}

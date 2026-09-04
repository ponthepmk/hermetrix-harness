package learning

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fixedReviewer answers from a script so the scorer can be tested without a
// model. The corpus gate is arithmetic over a reviewer's answers; this checks
// the arithmetic.
type fixedReviewer struct {
	proposeFor map[string]bool
	markdown   map[string]string
}

func (r fixedReviewer) Revision() string { return "fixed-reviewer-test" }

func (r fixedReviewer) Review(_ context.Context, digest Digest) (Decision, error) {
	key := digest.GoalAndConstraints
	if !r.proposeFor[key] {
		return Decision{Kind: "no_change", Reason: "scripted decline"}, nil
	}
	markdown := r.markdown[key]
	if markdown == "" {
		markdown = "---\nname: scripted\ndescription: \"scripted\"\n---\n\n# Procedure\n\n1. Do it.\n"
	}
	return Decision{Kind: "create", Reason: "scripted proposal",
		SuggestedSkill: &SuggestedSkill{CanonicalName: "scripted", Markdown: markdown}}, nil
}

func writeCase(t *testing.T, dir string, item CorpusCase) {
	t.Helper()
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, item.ID+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func labelled(should bool) Label {
	return Label{ShouldPropose: should, Rationale: "test", LabeledBy: "test", LabeledAt: "2026-08-24"}
}

func TestCorpusScoresRecallAgainstTheLabelNotTheCount(t *testing.T) {
	dir := t.TempDir()
	propose := map[string]bool{}
	// Six cases hold a procedure; the reviewer finds three of them.
	for i := 0; i < 6; i++ {
		goal := "positive-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: goal, TriggerKind: "repeated_correction", Provenance: "driven",
			Digest: Digest{GoalAndConstraints: goal}, Label: labelled(true)})
		if i < 3 {
			propose[goal] = true
		}
	}
	// Four hold nothing and the reviewer stays quiet on all of them.
	for i := 0; i < 4; i++ {
		goal := "negative-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: goal, TriggerKind: "successful_milestone", Provenance: "driven",
			Digest: Digest{GoalAndConstraints: goal}, Label: labelled(false)})
	}
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	results, err := ScoreCorpus(context.Background(), fixedReviewer{proposeFor: propose}, cases)
	if err != nil {
		t.Fatal(err)
	}
	all := results[len(results)-1]
	if all.Provenance != "all" || all.Cases != 10 {
		t.Fatalf("last result = %+v, want the combined ten", all)
	}
	if all.ShouldPropose != 6 || all.Proposed != 3 {
		t.Fatalf("denominator %d numerator %d, want 6 and 3", all.ShouldPropose, all.Proposed)
	}
	if all.ProposalRate != 0.5 {
		t.Fatalf("proposal rate %v, want 0.5", all.ProposalRate)
	}
	if all.Verdict != "failed" {
		t.Fatalf("verdict %q at half the %v floor, want failed", all.Verdict, CorpusProposalFloor)
	}
}

func TestCorpusCountsAProposalOnEmptyEvidenceAsFalse(t *testing.T) {
	dir := t.TempDir()
	propose := map[string]bool{}
	for i := 0; i < 8; i++ {
		goal := "positive-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: goal, TriggerKind: "explicit_learn", Provenance: "driven",
			Digest: Digest{GoalAndConstraints: goal}, Label: labelled(true)})
		propose[goal] = true
	}
	for i := 0; i < 10; i++ {
		goal := "negative-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: goal, TriggerKind: "successful_milestone", Provenance: "driven",
			Digest: Digest{GoalAndConstraints: goal}, Label: labelled(false)})
	}
	// Two of the ten empty cases draw a proposal anyway: 20%, over the ceiling.
	propose["negative-a"] = true
	propose["negative-b"] = true
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	results, err := ScoreCorpus(context.Background(), fixedReviewer{proposeFor: propose}, cases)
	if err != nil {
		t.Fatal(err)
	}
	all := results[len(results)-1]
	if all.ProposalRate != 1 {
		t.Fatalf("precondition: recall should be perfect here, got %v", all.ProposalRate)
	}
	if all.FalseProposals != 2 || all.FalseProposalRate != 0.2 {
		t.Fatalf("false proposals %d at rate %v, want 2 at 0.2", all.FalseProposals, all.FalseProposalRate)
	}
	if all.Verdict != "failed" {
		t.Fatalf("verdict %q with false-proposal rate over the %v ceiling, want failed",
			all.Verdict, CorpusFalseProposalCeiling)
	}
}

// TestOneInventedCitationFailsTheCorpus covers the term with no tolerance. A
// procedure justified by a receipt that does not exist cannot be checked by the
// person asked to approve it.
func TestOneInventedCitationFailsTheCorpus(t *testing.T) {
	dir := t.TempDir()
	propose := map[string]bool{}
	markdown := map[string]string{}
	for i := 0; i < 10; i++ {
		goal := "positive-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: goal, TriggerKind: "repeated_correction", Provenance: "driven",
			Digest: Digest{GoalAndConstraints: goal,
				ToolReceipts: []string{"event:event_real:workspace.write_file:succeeded"}},
			Label: labelled(true)})
		propose[goal] = true
		// Every proposal cites the receipt it was given, which is legitimate.
		markdown[goal] = "---\nname: scripted\ndescription: \"d\"\n---\n\nSee event:event_real for the check.\n"
	}
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	clean, err := ScoreCorpus(context.Background(),
		fixedReviewer{proposeFor: propose, markdown: markdown}, cases)
	if err != nil {
		t.Fatal(err)
	}
	if last := clean[len(clean)-1]; last.InventedEvidence != 0 || last.Verdict != "passed" {
		t.Fatalf("citing given evidence was counted as invented: %+v", last)
	}
	// One proposal cites a receipt the digest never carried.
	markdown["positive-c"] = "---\nname: scripted\ndescription: \"d\"\n---\n\nConfirmed by event:event_never_seen.\n"
	dirty, err := ScoreCorpus(context.Background(),
		fixedReviewer{proposeFor: propose, markdown: markdown}, cases)
	if err != nil {
		t.Fatal(err)
	}
	last := dirty[len(dirty)-1]
	if last.InventedEvidence != 1 {
		t.Fatalf("invented evidence = %d, want 1", last.InventedEvidence)
	}
	if last.ProposalRate != 1 || last.FalseProposalRate != 0 {
		t.Fatalf("precondition: both rates should still be perfect, got %+v", last)
	}
	if last.Verdict != "failed" {
		t.Fatalf("verdict %q with one invented citation, want failed", last.Verdict)
	}
}

func TestCorpusReportsDrivenAndSyntheticSeparately(t *testing.T) {
	dir := t.TempDir()
	propose := map[string]bool{}
	for i := 0; i < 4; i++ {
		driven := "driven-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: driven, TriggerKind: "successful_milestone", Provenance: "driven",
			Digest: Digest{GoalAndConstraints: driven}, Label: labelled(true)})
		synthetic := "synthetic-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: synthetic, TriggerKind: "skill_failure", Provenance: "synthetic",
			Digest: Digest{GoalAndConstraints: synthetic}, Label: labelled(true)})
		propose[synthetic] = true // the reviewer only handles the invented ones
	}
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	results, err := ScoreCorpus(context.Background(), fixedReviewer{proposeFor: propose}, cases)
	if err != nil {
		t.Fatal(err)
	}
	byProvenance := map[string]CorpusResult{}
	for _, item := range results {
		byProvenance[item.Provenance] = item
	}
	if byProvenance["synthetic"].ProposalRate != 1 {
		t.Fatalf("synthetic rate %v, want 1", byProvenance["synthetic"].ProposalRate)
	}
	if byProvenance["driven"].ProposalRate != 0 {
		t.Fatalf("driven rate %v, want 0", byProvenance["driven"].ProposalRate)
	}
	// The combined number is 0.5 and passes neither reading: reporting only that
	// would let invented cases carry the gate for real ones.
	if byProvenance["driven"].Verdict != "failed" {
		t.Fatalf("driven verdict %q, want failed", byProvenance["driven"].Verdict)
	}
}

// TestAnUnlabelledCaseIsRefused keeps the denominator honest. A case skipped
// for want of a label would quietly shrink the set the rate is taken over.
func TestAnUnlabelledCaseIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, CorpusCase{ID: "labelled", TriggerKind: "explicit_learn", Provenance: "driven",
		Digest: Digest{GoalAndConstraints: "a"}, Label: labelled(true)})
	writeCase(t, dir, CorpusCase{ID: "unlabelled", TriggerKind: "explicit_learn", Provenance: "driven",
		Digest: Digest{GoalAndConstraints: "b"}})
	if _, err := LoadCorpus(dir); err == nil {
		t.Fatal("an unlabelled case was accepted")
	}
	writeCase(t, dir, CorpusCase{ID: "unlabelled", TriggerKind: "explicit_learn", Provenance: "driven",
		Digest: Digest{GoalAndConstraints: "b"}, Label: labelled(false)})
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 {
		t.Fatalf("loaded %d cases, want 2", len(cases))
	}
}

// TestACorpusWithNoPositiveCaseCannotPass covers the shape that made the
// fidelity corpus meaningless: an instrument whose every reading is a pass
// because nothing was ever at stake.
func TestACorpusWithNoPositiveCaseCannotPass(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		goal := "negative-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: goal, TriggerKind: "successful_milestone", Provenance: "driven",
			Digest: Digest{GoalAndConstraints: goal}, Label: labelled(false)})
	}
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	results, err := ScoreCorpus(context.Background(), fixedReviewer{proposeFor: map[string]bool{}}, cases)
	if err != nil {
		t.Fatal(err)
	}
	if verdict := results[len(results)-1].Verdict; verdict != "insufficient_evidence" {
		t.Fatalf("verdict %q on a corpus that cannot measure recall, want insufficient_evidence", verdict)
	}
}

func TestFamilyCoverageCountsWhatARateCannotShow(t *testing.T) {
	cases := []CorpusCase{
		{TriggerKind: "successful_milestone"}, {TriggerKind: "successful_milestone"},
		{TriggerKind: "explicit_learn"},
	}
	coverage := FamilyCoverage(cases)
	if coverage["successful_milestone"] != 2 || coverage["explicit_learn"] != 1 {
		t.Fatalf("coverage = %v", coverage)
	}
	if coverage["skill_failure"] != 0 || coverage["repeated_correction"] != 0 {
		t.Fatalf("absent families should read zero, got %v", coverage)
	}
}

// brokenReviewer answers nothing usable, the way a model does when it returns
// prose instead of a decision object or a proposal with no body.
type brokenReviewer struct{ failFor map[string]bool }

func (r brokenReviewer) Revision() string { return "broken-reviewer-test" }

func (r brokenReviewer) Review(_ context.Context, digest Digest) (Decision, error) {
	if r.failFor[digest.GoalAndConstraints] {
		return Decision{Kind: "no_change", Unusable: true,
			Reason: "reviewer returned no decision object"}, nil
	}
	return Decision{Kind: "create", Reason: "scripted",
		SuggestedSkill: &SuggestedSkill{CanonicalName: "scripted",
			Markdown: "---\nname: scripted\ndescription: \"d\"\n---\n\n1. Do it.\n"}}, nil
}

// TestAReviewerThatCannotAnswerIsNotAReviewerThatDeclined separates the two
// causes a single no_change hides. Live, one review in forty-four came back
// with no parseable decision at all; counted as a judgement it is invisible,
// and recall limited by availability calls for a completely different fix than
// recall limited by judgement.
func TestAReviewerThatCannotAnswerIsNotAReviewerThatDeclined(t *testing.T) {
	dir := t.TempDir()
	failFor := map[string]bool{}
	for i := 0; i < 10; i++ {
		goal := "positive-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: goal, TriggerKind: "explicit_learn", Provenance: "driven",
			Digest: Digest{GoalAndConstraints: goal}, Label: labelled(true)})
	}
	// Five of the ten come back unusable rather than declined.
	for i := 0; i < 5; i++ {
		failFor["positive-"+string(rune('a'+i))] = true
	}
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	results, err := ScoreCorpus(context.Background(), brokenReviewer{failFor: failFor}, cases)
	if err != nil {
		t.Fatal(err)
	}
	all := results[len(results)-1]
	if all.ReviewerErrors != 5 {
		t.Fatalf("reviewer_errors = %d, want 5", all.ReviewerErrors)
	}
	// They still count against recall: a reviewer that could not answer did not
	// propose, and hiding that would flatter the rate.
	if all.Proposed != 5 || all.ProposalRate != 0.5 {
		t.Fatalf("proposed %d at rate %v; unusable answers must not be excluded from the denominator",
			all.Proposed, all.ProposalRate)
	}
	var namedAsError int
	for _, failure := range all.Failures {
		if strings.Contains(failure, "not a judgement") {
			namedAsError++
		}
	}
	if namedAsError != 5 {
		t.Fatalf("%d failures identified as reviewer errors, want 5:\n%v", namedAsError, all.Failures)
	}
}

// TestAWorkingReviewerReportsNoErrors keeps the counter from firing on ordinary
// declines.
func TestAWorkingReviewerReportsNoErrors(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 6; i++ {
		goal := "case-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: goal, TriggerKind: "successful_milestone", Provenance: "driven",
			Digest: Digest{GoalAndConstraints: goal}, Label: labelled(i < 3)})
	}
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	propose := map[string]bool{"case-a": true, "case-b": true, "case-c": true}
	results, err := ScoreCorpus(context.Background(), fixedReviewer{proposeFor: propose}, cases)
	if err != nil {
		t.Fatal(err)
	}
	if errors := results[len(results)-1].ReviewerErrors; errors != 0 {
		t.Fatalf("reviewer_errors = %d on a reviewer that answered every case", errors)
	}
}

// TestEachCaseIsReviewedOnce covers the cost and the consistency. Scoring
// reports driven, synthetic and combined; reviewing per split would spend three
// model calls per case and let the same evidence get different answers in the
// same report.
func TestEachCaseIsReviewedOnce(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 4; i++ {
		writeCase(t, dir, CorpusCase{ID: "driven-" + string(rune('a'+i)), TriggerKind: "explicit_learn",
			Provenance: "driven", Digest: Digest{GoalAndConstraints: "driven-" + string(rune('a'+i))},
			Label: labelled(true)})
		writeCase(t, dir, CorpusCase{ID: "synthetic-" + string(rune('a'+i)), TriggerKind: "skill_failure",
			Provenance: "synthetic", Digest: Digest{GoalAndConstraints: "synthetic-" + string(rune('a'+i))},
			Label: labelled(false)})
	}
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	counting := &countingReviewer{proposeFor: map[string]bool{}}
	var progressed []int
	if _, err := ScoreCorpusWithProgress(context.Background(), counting, cases,
		func(done, total int, _ string) {
			progressed = append(progressed, done)
			if total != len(cases) {
				t.Errorf("progress reported total %d, want %d", total, len(cases))
			}
		}); err != nil {
		t.Fatal(err)
	}
	if counting.calls != len(cases) {
		t.Fatalf("reviewer called %d times for %d cases", counting.calls, len(cases))
	}
	if len(progressed) != len(cases) || progressed[0] != 1 || progressed[len(progressed)-1] != len(cases) {
		t.Fatalf("progress went %v, want 1..%d", progressed, len(cases))
	}
}

type countingReviewer struct {
	proposeFor map[string]bool
	mutex      sync.Mutex
	calls      int
}

func (r *countingReviewer) Revision() string { return "counting-reviewer-test" }

func (r *countingReviewer) Review(_ context.Context, digest Digest) (Decision, error) {
	r.mutex.Lock()
	r.calls++
	r.mutex.Unlock()
	if !r.proposeFor[digest.GoalAndConstraints] {
		return Decision{Kind: "no_change", Reason: "scripted decline"}, nil
	}
	return Decision{Kind: "create", Reason: "scripted",
		SuggestedSkill: &SuggestedSkill{CanonicalName: "scripted",
			Markdown: "---\nname: scripted\ndescription: \"d\"\n---\n\n1. Do it.\n"}}, nil
}

// flakyReviewer answers differently on successive readings of the same case,
// which is what the real one does. Measured directly, three of twelve cases
// changed answer across five readings and every one was a positive.
type flakyReviewer struct {
	// answers maps a goal to the sequence of answers it gives, cycling.
	answers map[string][]bool
	mutex   sync.Mutex
	seen    map[string]int
}

func (r *flakyReviewer) Revision() string { return "flaky-reviewer-test" }

func (r *flakyReviewer) Review(_ context.Context, digest Digest) (Decision, error) {
	r.mutex.Lock()
	if r.seen == nil {
		r.seen = map[string]int{}
	}
	sequence := r.answers[digest.GoalAndConstraints]
	index := r.seen[digest.GoalAndConstraints]
	r.seen[digest.GoalAndConstraints]++
	r.mutex.Unlock()
	propose := len(sequence) > 0 && sequence[index%len(sequence)]
	if !propose {
		return Decision{Kind: "no_change", Reason: "scripted decline"}, nil
	}
	return Decision{Kind: "create", Reason: "scripted",
		SuggestedSkill: &SuggestedSkill{CanonicalName: "scripted",
			Markdown: "---\nname: scripted\ndescription: \"d\"\n---\n\n1. Do it.\n"}}, nil
}

// TestTheGateJudgesTheWorstReadingNotALuckyOne is why repeats exist. A reviewer
// that clears the floor on one reading and misses on another would pass or fail
// on which day the gate ran.
func TestTheGateJudgesTheWorstReadingNotALuckyOne(t *testing.T) {
	dir := t.TempDir()
	answers := map[string][]bool{}
	for i := 0; i < 10; i++ {
		goal := "positive-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: goal, TriggerKind: "explicit_learn", Provenance: "driven",
			Digest: Digest{GoalAndConstraints: goal}, Label: labelled(true)})
		switch {
		case i < 6:
			answers[goal] = []bool{true} // always proposed
		case i < 8:
			answers[goal] = []bool{true, false} // alternates: the unstable pair
		default:
			answers[goal] = []bool{false} // never proposed
		}
	}
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	// One reading catches the alternating pair at their good answer: 8/10 = 0.80.
	single, err := ScoreCorpusRepeated(context.Background(), &flakyReviewer{answers: answers}, cases, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if single[len(single)-1].ProposalRate != 0.8 || single[len(single)-1].Verdict != "passed" {
		t.Fatalf("precondition: a single lucky reading should pass, got %+v", single[len(single)-1])
	}
	// Two readings see them at their bad answer too: the worst is 6/10 = 0.60.
	repeated, err := ScoreCorpusRepeated(context.Background(), &flakyReviewer{answers: answers}, cases, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := repeated[len(repeated)-1]
	if result.Repeats != 2 {
		t.Fatalf("repeats = %d, want 2", result.Repeats)
	}
	if result.ProposalRate != 0.6 {
		t.Fatalf("worst rate = %v, want the worse reading at 0.6", result.ProposalRate)
	}
	if result.ProposalRateRange != [2]float64{0.6, 0.8} {
		t.Fatalf("range = %v, want [0.6 0.8]", result.ProposalRateRange)
	}
	if result.UnstableCases != 2 {
		t.Fatalf("unstable = %d, want the 2 cases that changed answer", result.UnstableCases)
	}
}

// TestAStableReviewerReportsNoInstability keeps the counter honest.
func TestAStableReviewerReportsNoInstability(t *testing.T) {
	dir := t.TempDir()
	answers := map[string][]bool{}
	for i := 0; i < 6; i++ {
		goal := "case-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: goal, TriggerKind: "repeated_correction", Provenance: "driven",
			Digest: Digest{GoalAndConstraints: goal}, Label: labelled(true)})
		answers[goal] = []bool{true}
	}
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	results, err := ScoreCorpusRepeated(context.Background(), &flakyReviewer{answers: answers}, cases, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := results[len(results)-1]
	if result.UnstableCases != 0 || len(result.UnstableIDs) != 0 {
		t.Fatalf("a reviewer that never changed its answer was reported unstable: %+v", result.UnstableIDs)
	}
	if result.ProposalRateRange != [2]float64{1, 1} {
		t.Fatalf("range = %v, want [1 1]", result.ProposalRateRange)
	}
}

// TestAnInventedCitationOnAnyReadingIsKept covers the ordering. A reading that
// invented evidence must be the one reported even when another reading had
// better recall: a proposal citing a receipt that does not exist is a fact
// about the reviewer, and picking the kinder reading would lose it.
func TestAnInventedCitationOnAnyReadingIsKept(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		writeCase(t, dir, CorpusCase{ID: "case-" + string(rune('a'+i)), TriggerKind: "explicit_learn",
			Provenance: "driven",
			Digest: Digest{GoalAndConstraints: "case-" + string(rune('a'+i)),
				ToolReceipts: []string{"event:event_real:workspace.write_file:succeeded"}},
			Label: labelled(true)})
	}
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	// First reading: perfect recall, but one proposal cites a receipt it never
	// received. Second reading: lower recall, nothing invented.
	reviewer := &inventingThenShyReviewer{}
	results, err := ScoreCorpusRepeated(context.Background(), reviewer, cases, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := results[len(results)-1]
	if result.InventedEvidence != 1 {
		t.Fatalf("invented = %d; the reading that invented a citation was discarded", result.InventedEvidence)
	}
	if result.Verdict != "failed" {
		t.Fatalf("verdict = %q, want failed", result.Verdict)
	}
	if result.ProposalRateRange[1] <= result.ProposalRateRange[0] {
		t.Fatalf("precondition: the two readings should differ in recall, got %v", result.ProposalRateRange)
	}
}

// inventingThenShyReviewer proposes everything on the first pass, citing an
// identifier it was never given on one case, then proposes less on the second.
type inventingThenShyReviewer struct {
	mutex sync.Mutex
	seen  map[string]int
}

func (r *inventingThenShyReviewer) Revision() string { return "inventing-reviewer-test" }

func (r *inventingThenShyReviewer) Review(_ context.Context, digest Digest) (Decision, error) {
	r.mutex.Lock()
	if r.seen == nil {
		r.seen = map[string]int{}
	}
	pass := r.seen[digest.GoalAndConstraints]
	r.seen[digest.GoalAndConstraints]++
	r.mutex.Unlock()
	if pass > 0 && digest.GoalAndConstraints > "case-e" {
		return Decision{Kind: "no_change", Reason: "shy on the second pass"}, nil
	}
	body := "---\nname: scripted\ndescription: \"d\"\n---\n\nSee event:event_real.\n"
	if pass == 0 && digest.GoalAndConstraints == "case-a" {
		body = "---\nname: scripted\ndescription: \"d\"\n---\n\nConfirmed by event:event_never_seen.\n"
	}
	return Decision{Kind: "create", Reason: "scripted",
		SuggestedSkill: &SuggestedSkill{CanonicalName: "scripted", Markdown: body}}, nil
}

// TestACitationAtTheEndOfASentenceIsNotInvented covers the accusation this
// check makes. Identifiers legitimately contain dots and colons -- a skill
// reference is skill:<id>@<version>, a receipt is event:<id>:<tool>:<status> --
// so the pattern admits them, and a citation ending a sentence then swallows
// the full stop and stops matching what the digest carried.
func TestACitationAtTheEndOfASentenceIsNotInvented(t *testing.T) {
	digest := Digest{
		ToolReceipts:     []string{"event:event_real:workspace.write_file:succeeded"},
		SkillActivations: []string{"skill:skill_abc@ver_def"},
	}
	for _, body := range []string{
		"Confirmed by event:event_real.",
		"Confirmed by event:event_real, then written.",
		"Applied skill:skill_abc@ver_def; the write succeeded.",
		"See event:event_real:workspace.write_file:succeeded.",
	} {
		if invented := inventedEvidence(digest, SuggestedSkill{Markdown: body}); len(invented) > 0 {
			t.Fatalf("%q reported as inventing %v", body, invented)
		}
	}
	// Trimming punctuation must not turn a genuinely absent identifier into a
	// present one.
	invented := inventedEvidence(digest, SuggestedSkill{Markdown: "Confirmed by event:event_imagined."})
	if len(invented) != 1 || invented[0] != "event:event_imagined" {
		t.Fatalf("invented = %v, want the imagined event with its punctuation trimmed", invented)
	}
}

func TestVerificationReceiptIsAValidEvidenceCitation(t *testing.T) {
	digest := Digest{VerifiedBy: []string{"event:event_measured:workspace.run:succeeded"}}
	suggested := SuggestedSkill{Markdown: "Verified by event:event_measured."}
	if invented := inventedEvidence(digest, suggested); len(invented) != 0 {
		t.Fatalf("a cited verification receipt was reported as invented: %v", invented)
	}
}

// failingReviewer fails a set number of times before answering, the way a
// gateway does when it returns a 502 and then recovers.
type failingReviewer struct {
	failures int
	mutex    sync.Mutex
	calls    int
}

func (r *failingReviewer) Revision() string { return "failing-reviewer-test" }

func (r *failingReviewer) Review(_ context.Context, _ Digest) (Decision, error) {
	r.mutex.Lock()
	r.calls++
	calls := r.calls
	r.mutex.Unlock()
	if calls <= r.failures {
		return Decision{}, errors.New("provider returned HTTP 502")
	}
	return Decision{Kind: "no_change", Reason: "answered after the fault"}, nil
}

// TestATransientProviderFaultDoesNotThrowAwayTheRun covers what cost an hour. A
// scoring run of two hundred reviews spent fifty-five minutes and was discarded
// by a single 502 near the end.
func TestATransientProviderFaultDoesNotThrowAwayTheRun(t *testing.T) {
	shortenRetryDelay(t)
	dir := t.TempDir()
	writeCase(t, dir, CorpusCase{ID: "case-a", TriggerKind: "explicit_learn", Provenance: "driven",
		Digest: Digest{GoalAndConstraints: "a"}, Label: labelled(false)})
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	reviewer := &failingReviewer{failures: 2}
	if _, err := ScoreCorpusRepeated(context.Background(), reviewer, cases, 1, nil); err != nil {
		t.Fatalf("two faults ended the run: %v", err)
	}
	if reviewer.calls != 3 {
		t.Fatalf("reviewer called %d times, want two faults then an answer", reviewer.calls)
	}
}

// TestAProviderThatNeverRecoversStopsTheRun keeps the retry bounded. Repeating
// forever would turn an outage into a hang.
func TestAProviderThatNeverRecoversStopsTheRun(t *testing.T) {
	shortenRetryDelay(t)
	dir := t.TempDir()
	writeCase(t, dir, CorpusCase{ID: "case-a", TriggerKind: "explicit_learn", Provenance: "driven",
		Digest: Digest{GoalAndConstraints: "a"}, Label: labelled(false)})
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	reviewer := &failingReviewer{failures: 1000}
	_, err = ScoreCorpusRepeated(context.Background(), reviewer, cases, 1, nil)
	if err == nil {
		t.Fatal("a provider that never answered was treated as success")
	}
	if !strings.Contains(err.Error(), "attempts") {
		t.Fatalf("error does not say it gave up after retrying: %v", err)
	}
	if reviewer.calls != reviewRetries+1 {
		t.Fatalf("reviewer called %d times, want %d", reviewer.calls, reviewRetries+1)
	}
}

// TestAnUnusableAnswerIsNotRetried draws the line. The reviewer failing to
// phrase a decision is an answer; only a fault on the way to it is retried.
func TestAnUnusableAnswerIsNotRetried(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, CorpusCase{ID: "case-a", TriggerKind: "explicit_learn", Provenance: "driven",
		Digest: Digest{GoalAndConstraints: "a"}, Label: labelled(true)})
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	reviewer := &brokenReviewer{failFor: map[string]bool{"a": true}}
	results, err := ScoreCorpusRepeated(context.Background(), reviewer, cases, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[len(results)-1].ReviewerErrors != 1 {
		t.Fatalf("an unusable answer was not recorded: %+v", results[len(results)-1])
	}
}

// shortenRetryDelay keeps the retry tests about attempts rather than about
// waiting. They asserted the same arithmetic in thirty-six seconds before.
func shortenRetryDelay(t *testing.T) {
	t.Helper()
	previous := reviewRetryDelay
	reviewRetryDelay = time.Millisecond
	t.Cleanup(func() { reviewRetryDelay = previous })
}

// TestConcurrentScoringGivesTheSameAnswers is the property concurrency must not
// break. Results are keyed by case, so running several reviews at once changes
// when answers arrive and nothing about what they are.
func TestConcurrentScoringGivesTheSameAnswers(t *testing.T) {
	dir := t.TempDir()
	answers := map[string][]bool{}
	for i := 0; i < 20; i++ {
		goal := "case-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: goal, TriggerKind: "explicit_learn", Provenance: "driven",
			Digest: Digest{GoalAndConstraints: goal}, Label: labelled(i%2 == 0)})
		answers[goal] = []bool{i%3 == 0}
	}
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	sequential, err := ScoreCorpusConcurrent(context.Background(),
		&flakyReviewer{answers: answers}, cases, 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	concurrent, err := ScoreCorpusConcurrent(context.Background(),
		&flakyReviewer{answers: answers}, cases, 1, 8, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, b := sequential[len(sequential)-1], concurrent[len(concurrent)-1]
	if a.Proposed != b.Proposed || a.ProposalRate != b.ProposalRate ||
		a.FalseProposals != b.FalseProposals || a.Verdict != b.Verdict {
		t.Fatalf("concurrency changed the answer:\n one at a time %+v\n eight at once %+v", a, b)
	}
}

// TestConcurrentScoringCountsEveryCaseOnce guards the shared counter behind the
// progress callback: a race there would report a total that never reaches the
// number of cases, or reaches it twice.
func TestConcurrentScoringCountsEveryCaseOnce(t *testing.T) {
	dir := t.TempDir()
	answers := map[string][]bool{}
	for i := 0; i < 30; i++ {
		goal := "case-" + string(rune('a'+i))
		writeCase(t, dir, CorpusCase{ID: goal, TriggerKind: "skill_failure", Provenance: "driven",
			Digest: Digest{GoalAndConstraints: goal}, Label: labelled(false)})
		answers[goal] = []bool{false}
	}
	cases, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	var mutex sync.Mutex
	seen := map[int]int{}
	if _, err := ScoreCorpusConcurrent(context.Background(), &flakyReviewer{answers: answers},
		cases, 2, 8, func(done, total int, _ string) {
			mutex.Lock()
			defer mutex.Unlock()
			seen[done]++
			if total != len(cases)*2 {
				t.Errorf("total = %d, want %d", total, len(cases)*2)
			}
		}); err != nil {
		t.Fatal(err)
	}
	for step := 1; step <= len(cases)*2; step++ {
		if seen[step] != 1 {
			t.Fatalf("progress reported step %d %d times, want once", step, seen[step])
		}
	}
}

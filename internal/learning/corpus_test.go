package learning

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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

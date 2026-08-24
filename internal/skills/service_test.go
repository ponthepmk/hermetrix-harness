package skills

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"hermetrix-harness/internal/store"
)

func testService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	return NewService(dataStore), dataStore
}

func candidateInput(name, description, body string) CreateCandidateInput {
	return CreateCandidateInput{CanonicalName: name, ScopeKind: "user", Origin: "user_created", Owner: "user",
		ChangeKind: "create", CreatedBy: "user", TriggerKind: "manual", Reason: "repeatable procedure",
		EvidenceRefs: []string{"session:test"}, Markdown: "---\nname: " + name + "\ndescription: \"" + description + "\"\ntags: [test]\ntools: [filesystem.read]\n---\n\n" + body + "\n"}
}

func TestSkillLifecycleIsProposalOnlyVersionedAndRecoverable(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	input := candidateInput("review-work", "Review work with explicit evidence", "# Procedure\n\n1. Read evidence.\n2. Report risks.")
	candidate, err := service.CreateCandidate(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.State != CandidateNeedsReview || !candidate.Checks.Passed {
		t.Fatalf("candidate = %+v", candidate)
	}
	before, err := service.ListSkills(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 0 {
		t.Fatal("candidate leaked into active skills")
	}
	skill, err := service.PromoteCandidate(ctx, candidate.ID, "user", candidate.Revision)
	if err != nil {
		t.Fatal(err)
	}
	version, err := service.GetVersion(ctx, skill.CurrentVersionID)
	if err != nil {
		t.Fatal(err)
	}
	if version.Markdown != input.Markdown {
		t.Fatal("active version is not byte-exact candidate content")
	}
	if version.AuthorActor != "user" || skill.Origin != "user_created" || skill.Owner != "user" {
		t.Fatalf("provenance lost: %+v %+v", skill, version)
	}
	if _, err := service.RecordActivation(ctx, ActivationInput{SessionID: "s1", TurnID: "t1", SkillID: skill.ID,
		VersionID: version.ID, SelectionSource: "explicit", MetadataExposed: true, BodyInjected: true,
		Outcome: "success", OutcomeSource: "user_confirmed", AttributionKind: "observed"}); err != nil {
		t.Fatal(err)
	}
	updated, err := service.GetSkill(ctx, skill.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.SelectedCount != 1 || updated.InjectedCount != 1 || updated.SuccessCount != 1 {
		t.Fatalf("activation aggregate = %+v", updated)
	}
	archive, err := service.ArchiveSkill(ctx, skill.ID, "user", "superseded during test", "")
	if err != nil {
		t.Fatal(err)
	}
	active, _ := service.ListSkills(ctx, false)
	if len(active) != 0 {
		t.Fatal("archived skill remained selectable")
	}
	restore, err := service.RestoreArchive(ctx, archive.ID, "user", "need the old procedure")
	if err != nil {
		t.Fatal(err)
	}
	archivedSkill, _ := service.GetSkill(ctx, skill.ID)
	if archivedSkill.State != StateArchived {
		t.Fatal("restore proposal mutated active state")
	}
	restored, err := service.PromoteCandidate(ctx, restore.ID, "user", restore.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if restored.State != StateActive || restored.CurrentVersionID == version.ID {
		t.Fatalf("restore did not create a new active version: %+v", restored)
	}
	restoredVersion, _ := service.GetVersion(ctx, restored.CurrentVersionID)
	if restoredVersion.Markdown != input.Markdown {
		t.Fatal("restored bytes differ from archived snapshot")
	}
	if _, err := service.RestoreArchive(ctx, archive.ID, "user", "duplicate restore"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("duplicate restore error = %v", err)
	}
}

func TestFailedChecksCannotPromote(t *testing.T) {
	service, _ := testService(t)
	input := candidateInput("Bad Name", "", "body")
	candidate, err := service.CreateCandidate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.State != CandidateQuarantined || candidate.Checks.Passed {
		t.Fatalf("candidate = %+v", candidate)
	}
	_, err = service.PromoteCandidate(context.Background(), candidate.ID, "user", candidate.Revision)
	if !errors.Is(err, ErrCandidateNotReady) {
		t.Fatalf("promotion error = %v", err)
	}
}

func TestCandidateCanBeEditedRecheckedAndPromoted(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	input := candidateInput("fixable-skill", "", "# Incomplete")
	candidate, err := service.CreateCandidate(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.State != CandidateQuarantined {
		t.Fatalf("state = %s", candidate.State)
	}
	valid := candidateInput("fixable-skill", "A repaired and reviewable skill", "# Complete\n\n1. Verify evidence.").Markdown
	updated, err := service.UpdateCandidate(ctx, candidate.ID, UpdateCandidateInput{Markdown: valid, Actor: "user", ExpectedRevision: candidate.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != CandidateNeedsReview || updated.Revision != candidate.Revision+1 || !updated.Checks.Passed {
		t.Fatalf("updated candidate = %+v", updated)
	}
	if _, err := service.UpdateCandidate(ctx, candidate.ID, UpdateCandidateInput{Markdown: valid, Actor: "user", ExpectedRevision: candidate.Revision}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale edit error = %v", err)
	}
	if _, err := service.PromoteCandidate(ctx, updated.ID, "user", updated.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestImprovementClonesActiveVersionWithoutMutatingIt(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	base, _ := service.CreateCandidate(ctx, candidateInput("improvable-skill", "A procedure that can improve", "# Stable procedure"))
	skill, err := service.PromoteCandidate(ctx, base.ID, "user", base.Revision)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := service.GetVersion(ctx, skill.CurrentVersionID)
	improvement, err := service.ProposeImprovement(ctx, skill.ID, "user", "make the procedure clearer")
	if err != nil {
		t.Fatal(err)
	}
	if improvement.ChangeKind != "improve" || improvement.TargetSkillID != skill.ID || improvement.BaseVersionID != skill.CurrentVersionID {
		t.Fatalf("improvement lineage = %+v", improvement)
	}
	if improvement.Markdown != before.Markdown {
		t.Fatal("improvement did not clone the active bytes")
	}
	after, _ := service.GetSkill(ctx, skill.ID)
	if after.CurrentVersionID != skill.CurrentVersionID {
		t.Fatal("creating an improvement mutated the active version")
	}
}

func TestStaleBaseCannotOverwriteNewVersion(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	base, _ := service.CreateCandidate(ctx, candidateInput("stable-skill", "A stable procedure", "# Base\nDo the safe thing."))
	skill, err := service.PromoteCandidate(ctx, base.ID, "user", base.Revision)
	if err != nil {
		t.Fatal(err)
	}
	firstInput := candidateInput("stable-skill", "A stable procedure", "# Revision one\nDo the safer thing.")
	firstInput.ChangeKind, firstInput.TargetSkillID, firstInput.BaseVersionID = "improve", skill.ID, skill.CurrentVersionID
	secondInput := firstInput
	secondInput.Markdown = strings.ReplaceAll(firstInput.Markdown, "Revision one", "Revision two")
	first, _ := service.CreateCandidate(ctx, firstInput)
	second, _ := service.CreateCandidate(ctx, secondInput)
	if _, err := service.PromoteCandidate(ctx, first.ID, "user", first.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PromoteCandidate(ctx, second.ID, "user", second.Revision); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale promotion error = %v", err)
	}
}

func TestImprovementCannotChangeSkillAuthorityMetadata(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	base, _ := service.CreateCandidate(ctx, candidateInput("scoped-skill", "A user-scoped skill", "# Base"))
	skill, err := service.PromoteCandidate(ctx, base.ID, "user", base.Revision)
	if err != nil {
		t.Fatal(err)
	}
	input := candidateInput("scoped-skill", "A user-scoped skill", "# Content update")
	input.ChangeKind, input.TargetSkillID, input.BaseVersionID = "improve", skill.ID, skill.CurrentVersionID
	input.ScopeKind = "workspace"
	candidate, err := service.CreateCandidate(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PromoteCandidate(ctx, candidate.ID, "user", candidate.Revision); !errors.Is(err, ErrImmutableMetadata) {
		t.Fatalf("metadata-changing promotion error = %v", err)
	}
	unchanged, _ := service.GetSkill(ctx, skill.ID)
	if unchanged.ScopeKind != "user" || unchanged.CurrentVersionID != skill.CurrentVersionID {
		t.Fatalf("metadata changed: %+v", unchanged)
	}
}

func TestActivationRejectsMismatchedVersion(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	first, _ := service.CreateCandidate(ctx, candidateInput("first-skill", "First skill", "# First"))
	firstSkill, _ := service.PromoteCandidate(ctx, first.ID, "user", first.Revision)
	second, _ := service.CreateCandidate(ctx, candidateInput("second-skill", "Second skill", "# Second"))
	secondSkill, _ := service.PromoteCandidate(ctx, second.ID, "user", second.Revision)
	_, err := service.RecordActivation(ctx, ActivationInput{SessionID: "s", TurnID: "t", SkillID: firstSkill.ID,
		VersionID: secondSkill.CurrentVersionID, SelectionSource: "test"})
	if err == nil {
		t.Fatal("mismatched skill/version was accepted")
	}
}

func TestDuplicateAnalysisIsAdvisoryAndVersionBound(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	left, _ := service.CreateCandidate(ctx, candidateInput("review-alpha", "Review changes and report risks with evidence", "# Procedure\nRead every changed file. Report risks with evidence and do not modify files."))
	leftSkill, _ := service.PromoteCandidate(ctx, left.ID, "user", left.Revision)
	right, _ := service.CreateCandidate(ctx, candidateInput("review-beta", "Review changes and report risks with evidence", "# Procedure\nRead every changed file. Report risks with evidence and do not modify files."))
	rightSkill, _ := service.PromoteCandidate(ctx, right.ID, "user", right.Revision)
	findings, err := service.AnalyzeRelations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].LeftVersionID == "" || findings[0].RightVersionID == "" {
		t.Fatal("relation lacks version provenance")
	}
	leftAfter, _ := service.GetSkill(ctx, leftSkill.ID)
	rightAfter, _ := service.GetSkill(ctx, rightSkill.ID)
	if leftAfter.State != StateActive || rightAfter.State != StateActive {
		t.Fatal("analysis mutated skill state")
	}
}

// --- O-17: the duplicate analyzer never retrieved anything ---
//
// It is a retrieval stage feeding a human review, so its job is recall. It was
// scored like a judge instead and returned nothing at all, which meant the
// review stage behind it never saw a candidate.

func seedAnalyzerSkill(t *testing.T, service *Service, name, description, body string) {
	t.Helper()
	ctx := context.Background()
	candidate, err := service.CreateCandidate(ctx, CreateCandidateInput{CanonicalName: name, ScopeKind: "user",
		Origin: "user_created", Owner: "user", ChangeKind: "create", CreatedBy: "test", TriggerKind: "manual",
		Reason: "analyzer coverage",
		Markdown: fmt.Sprintf("---\nname: %s\ndescription: \"%s\"\ntags: []\ntools: []\n---\n\n# Procedure\n\n%s\n",
			name, description, body)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PromoteCandidate(ctx, candidate.ID, "test", candidate.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyzerRetrievesAParaphraseAndLeavesStrangersAlone(t *testing.T) {
	service, _ := testService(t)
	seedAnalyzerSkill(t, service, "satang-rounding", "Round Thai money amounts half up in satang",
		"1. Keep every amount as an integer number of satang.\n2. Round half up at each step.\n3. Verify net plus VAT equals gross after rounding.")
	seedAnalyzerSkill(t, service, "money-rounding-thai", "Round Thai monetary values half up using satang integers",
		"1. Amounts are integers in satang, never floats.\n2. Apply half-up rounding at every step.\n3. Check that net + VAT = gross once rounded.")
	seedAnalyzerSkill(t, service, "invoice-numbering", "Format Thai invoice numbers as INV plus five digits",
		"1. Invoice numbers are INV followed by five digits.\n2. Zero pad the sequence on the left.\n3. Fail loudly when the sequence exceeds 99999.")
	seedAnalyzerSkill(t, service, "browser-login", "Sign in to the supplier portal and download the monthly statement",
		"1. Open the supplier portal.\n2. Sign in with the stored credential reference.\n3. Download the statement for the requested month.")

	relations, err := service.AnalyzeRelations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(relations) != 1 {
		t.Fatalf("expected exactly the paraphrase pair, got %d: %+v", len(relations), relations)
	}
	pair := relations[0]
	names := []string{pair.LeftName, pair.RightName}
	sort.Strings(names)
	if names[0] != "money-rounding-thai" || names[1] != "satang-rounding" {
		t.Fatalf("the wrong pair was retrieved: %v", names)
	}
	if pair.Kind != "overlap" {
		t.Fatalf("kind %q, want overlap", pair.Kind)
	}
	// Retrieval, never a decision: the finding has to say so.
	if note, _ := pair.Evidence["note"].(string); !strings.Contains(note, "human review") {
		t.Fatalf("a retrieval candidate does not say it needs review: %q", note)
	}
}

// Two Skills that declare no tools have nothing to compare there. Jaccard
// returns zero for two empty sets, which used to spend the tool weight as
// evidence of difference -- and every agent-proposed Skill declares no tools.
func TestAbsentToolsAreNotEvidenceOfDifference(t *testing.T) {
	withTools := similarityScore(0.5, 0.4, 0.6, false)
	absent := similarityScore(0.5, 0.4, 0, true)
	penalised := 0.60*0.5 + 0.25*0.4 + 0.15*0
	if absent <= penalised {
		t.Fatalf("absent tools scored %.3f, no better than counting them as different (%.3f)", absent, penalised)
	}
	if withTools <= 0 {
		t.Fatalf("declared tools stopped contributing: %.3f", withTools)
	}
	// Identical on every axis that carries signal must reach the top.
	if perfect := similarityScore(1, 1, 0, true); perfect < 0.999 {
		t.Fatalf("two identical tool-less Skills top out at %.3f, so the upper bands are unreachable", perfect)
	}
}

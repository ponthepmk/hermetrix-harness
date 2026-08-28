package skills

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func replayFixture(required ...string) File {
	return File{Path: "tests/preserve.json", Content: []byte(`{"id":"preserve","prompt":"Preserve the verified procedure","required_phrases":["` +
		strings.Join(required, `","`) + `"],"forbidden_phrases":["skip verification"],"required_tools":["filesystem.read"]}`)}
}

func TestImprovementReplayBlocksRegressionAndBindsExactRevision(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	input := candidateInput("replay-safe", "Replay a verified procedure", "# Procedure\n\nAlways verify the exact checksum.")
	input.Files = []File{replayFixture("always verify", "exact checksum")}
	base, err := service.CreateCandidate(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	skill, err := service.PromoteCandidate(ctx, base.ID, "user", base.Revision)
	if err != nil {
		t.Fatal(err)
	}
	improvement, err := service.ProposeImprovement(ctx, skill.ID, "user", "clarify procedure")
	if err != nil {
		t.Fatal(err)
	}
	if !improvement.Checks.ReplayPassed || !improvement.Checks.Passed {
		t.Fatalf("unchanged baseline replay = %+v", improvement.Checks)
	}
	broken := strings.Replace(improvement.Markdown, "Always verify the exact checksum.", "Proceed without the checksum.", 1)
	improvement, err = service.UpdateCandidate(ctx, improvement.ID, UpdateCandidateInput{Markdown: broken, Actor: "user", ExpectedRevision: improvement.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if improvement.Checks.ReplayPassed || improvement.State != CandidateQuarantined {
		t.Fatalf("regression passed replay: %+v", improvement)
	}
	if _, err := service.PromoteCandidate(ctx, improvement.ID, "user", improvement.Revision); !errors.Is(err, ErrCandidateNotReady) {
		t.Fatalf("regressed promotion error = %v", err)
	}
	fixed := strings.Replace(broken, "Proceed without the checksum.", "Always verify the exact checksum and record evidence.", 1)
	improvement, err = service.UpdateCandidate(ctx, improvement.ID, UpdateCandidateInput{Markdown: fixed, Actor: "user", ExpectedRevision: improvement.Revision})
	if err != nil || !improvement.Checks.ReplayPassed {
		t.Fatalf("fixed replay candidate=%+v err=%v", improvement, err)
	}
	runs, err := service.ListCandidateReplays(ctx, improvement.ID)
	if err != nil || len(runs) != 3 || runs[0].CandidateRevision != improvement.Revision || runs[0].CandidateHash != improvement.CandidateHash {
		t.Fatalf("replay history=%+v err=%v", runs, err)
	}
}

func TestReplayFixturesCannotBeWeakened(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	input := candidateInput("fixture-guard", "Keep replay assertions monotonic", "# Procedure\n\nAlways verify evidence and checksum.")
	input.Files = []File{replayFixture("always verify", "checksum")}
	base, _ := service.CreateCandidate(ctx, input)
	skill, _ := service.PromoteCandidate(ctx, base.ID, "user", base.Revision)
	version, _ := service.GetVersion(ctx, skill.CurrentVersionID)
	weakened := File{Path: "tests/preserve.json", Content: []byte(`{"id":"preserve","prompt":"weakened","required_phrases":["always verify"],"forbidden_phrases":["skip verification"],"required_tools":["filesystem.read"]}`)}
	candidate, err := service.CreateCandidate(ctx, CreateCandidateInput{CanonicalName: skill.CanonicalName, ScopeKind: skill.ScopeKind,
		Origin: skill.Origin, Owner: skill.Owner, ChangeKind: "improve", TargetSkillID: skill.ID, BaseVersionID: version.ID,
		CreatedBy: "user", TriggerKind: "manual", Reason: "try weaker tests", Markdown: version.Markdown, Files: []File{weakened}})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Checks.ReplayPassed || candidate.State != CandidateQuarantined {
		t.Fatalf("weakened fixture was accepted: %+v", candidate.Checks)
	}
	runs, _ := service.ListCandidateReplays(ctx, candidate.ID)
	if len(runs) != 1 || len(runs[0].Summary.WeakenedTests) != 1 {
		t.Fatalf("weakened evidence missing: %+v", runs)
	}
}

func TestCapabilityWideningRequiresExactHumanReview(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	base, _ := service.CreateCandidate(ctx, candidateInput("tool-review", "Review widened tool authority", "# Procedure\n\nRead evidence."))
	skill, _ := service.PromoteCandidate(ctx, base.ID, "user", base.Revision)
	candidate, _ := service.ProposeImprovement(ctx, skill.ID, "user", "add a write step")
	updatedMarkdown := strings.Replace(candidate.Markdown, "tools: [filesystem.read]", "tools: [filesystem.read, filesystem.write]", 1)
	candidate, err := service.UpdateCandidate(ctx, candidate.ID, UpdateCandidateInput{Markdown: updatedMarkdown, Actor: "user", ExpectedRevision: candidate.Revision})
	if err != nil || !candidate.Checks.Passed {
		t.Fatalf("candidate=%+v err=%v", candidate, err)
	}
	if _, err := service.PromoteCandidate(ctx, candidate.ID, "user", candidate.Revision); !errors.Is(err, ErrCapabilityReview) {
		t.Fatalf("unreviewed widening error = %v", err)
	}
	review, err := service.ReviewCandidateCapabilities(ctx, candidate.ID, candidate.Revision, "user", "approve")
	if err != nil || len(review.AddedTools) != 1 || review.AddedTools[0] != "filesystem.write" {
		t.Fatalf("review=%+v err=%v", review, err)
	}
	passBehavioralEval(t, service, candidate.ID)
	if _, err := service.PromoteCandidate(ctx, candidate.ID, "user", candidate.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestActivationOutcomeAttributionIsRuntimeBoundAndOneShot(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	base, _ := service.CreateCandidate(ctx, candidateInput("outcome-bound", "Track runtime outcome confidence", "# Procedure\n\nObserve results."))
	skill, _ := service.PromoteCandidate(ctx, base.ID, "user", base.Revision)
	if _, err := service.RecordActivation(ctx, ActivationInput{SessionID: "session", TurnID: "turn", SkillID: skill.ID,
		VersionID: skill.CurrentVersionID, SelectionSource: "runtime", MetadataExposed: true, BodyInjected: true}); err != nil {
		t.Fatal(err)
	}
	changed, err := service.CompleteTurnActivations(ctx, "turn", "success")
	if err != nil || changed != 1 {
		t.Fatalf("changed=%d err=%v", changed, err)
	}
	changed, err = service.CompleteTurnActivations(ctx, "turn", "failure")
	if err != nil || changed != 0 {
		t.Fatalf("outcome was rewritten: changed=%d err=%v", changed, err)
	}
	observed, _ := service.GetSkill(ctx, skill.ID)
	if observed.SuccessCount != 1 || observed.FailureCount != 0 {
		t.Fatalf("outcome aggregate=%+v", observed)
	}
}

// TestImplicitOnlyReplayIsReportedAsSuch covers what a green replay is worth
// when the Skill has no fixtures. CreateCandidate runs the replay itself for an
// improve, and with no author fixture the runner synthesises one asserting the
// manifest name, description and tool list are unchanged. A candidate that
// reverses every procedural step passes it, and the result reads as
// fixtures_total 1, summary passed, replay_passed true.
//
// The gate's strictness is a policy question. Its honesty is not: the reviewer
// approving the promotion must be able to see that nothing behavioural ran.
func TestImplicitOnlyReplayIsReportedAsSuch(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	base := "---\nname: probe-skill\ndescription: \"probe\"\ntags: []\ntools: []\n---\n\n# Procedure\n\n1. Keep amounts in satang.\n2. Round half up.\n"
	seed, err := service.CreateCandidate(ctx, CreateCandidateInput{CanonicalName: "probe-skill", ScopeKind: "user",
		Origin: "user_created", Owner: "user", ChangeKind: "create", CreatedBy: "t", TriggerKind: "manual",
		Reason: "seed", EvidenceRefs: []string{"e"}, Markdown: base})
	if err != nil {
		t.Fatal(err)
	}
	skill, err := service.PromoteCandidate(ctx, seed.ID, "t", 0)
	if err != nil {
		t.Fatal(err)
	}
	reversed := "---\nname: probe-skill\ndescription: \"probe\"\ntags: []\ntools: []\n---\n\n# Procedure\n\n1. Use floating point baht.\n2. Round down and ignore the remainder.\n"
	improve, err := service.CreateCandidate(ctx, CreateCandidateInput{CanonicalName: "probe-skill", ScopeKind: "user",
		Origin: "user_created", Owner: "user", ChangeKind: "improve", TargetSkillID: skill.ID,
		BaseVersionID: skill.CurrentVersionID, CreatedBy: "t", TriggerKind: "manual",
		Reason: "reverse every step", EvidenceRefs: []string{"e"}, Markdown: reversed})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := service.ListCandidateReplays(ctx, improve.ID)
	if err != nil || len(runs) == 0 {
		t.Fatalf("runs=%d err=%v", len(runs), err)
	}
	run := runs[0]
	if run.Summary.AuthorFixtures != 0 || !run.Summary.ImplicitOnly {
		t.Fatalf("summary = %+v, want no author fixtures and implicit_only", run.Summary)
	}
	if run.FixturesTotal != 1 || !run.Summary.Passed {
		t.Fatalf("precondition: the implicit fixture should run and pass, got %+v", run)
	}
	var flagged bool
	for _, finding := range improve.Checks.Findings {
		if finding.Code == "replay_implicit_only" {
			flagged = true
			// An error, not a warning. The owner decided a green result nobody
			// can distinguish from a real one is worse than a refusal, because
			// a refusal says what to do next: write one fixture.
			if finding.Level != "error" {
				t.Fatalf("finding level = %q, want error", finding.Level)
			}
		}
	}
	if !flagged {
		t.Fatalf("a candidate whose replay tested only the manifest carries no such finding: %+v", improve.Checks)
	}
	// And promotion refuses it. This is the candidate that reversed every step
	// of its own procedure and passed the manifest check.
	if _, err := service.PromoteCandidate(ctx, improve.ID, "user", improve.Revision); !errors.Is(err,
		ErrReplayImplicitOnly) {
		t.Fatalf("a manifest-only replay still allowed promotion: %v", err)
	}
}

// TestAnAuthoredFixtureIsNotReportedAsImplicit keeps the warning from firing on
// a Skill that does carry tests.
func TestAnAuthoredFixtureIsNotReportedAsImplicit(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	fixture := `{"id":"keeps-satang","prompt":"round a total","required_phrases":["satang"]}`
	base := "---\nname: probe-skill\ndescription: \"probe\"\ntags: []\ntools: []\n---\n\n# Procedure\n\n1. Keep amounts in satang.\n"
	seed, err := service.CreateCandidate(ctx, CreateCandidateInput{CanonicalName: "probe-skill", ScopeKind: "user",
		Origin: "user_created", Owner: "user", ChangeKind: "create", CreatedBy: "t", TriggerKind: "manual",
		Reason: "seed", EvidenceRefs: []string{"e"}, Markdown: base,
		Files: []File{{Path: "tests/keeps-satang.json", Content: []byte(fixture)}}})
	if err != nil {
		t.Fatal(err)
	}
	skill, err := service.PromoteCandidate(ctx, seed.ID, "t", 0)
	if err != nil {
		t.Fatal(err)
	}
	improved := base + "2. Round half up.\n"
	improve, err := service.CreateCandidate(ctx, CreateCandidateInput{CanonicalName: "probe-skill", ScopeKind: "user",
		Origin: "user_created", Owner: "user", ChangeKind: "improve", TargetSkillID: skill.ID,
		BaseVersionID: skill.CurrentVersionID, CreatedBy: "t", TriggerKind: "manual",
		Reason: "add rounding", EvidenceRefs: []string{"e"}, Markdown: improved,
		Files: []File{{Path: "tests/keeps-satang.json", Content: []byte(fixture)}}})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := service.ListCandidateReplays(ctx, improve.ID)
	if err != nil || len(runs) == 0 {
		t.Fatalf("runs=%d err=%v", len(runs), err)
	}
	if runs[0].Summary.AuthorFixtures == 0 || runs[0].Summary.ImplicitOnly {
		t.Fatalf("an authored fixture was reported as implicit: %+v", runs[0].Summary)
	}
	for _, finding := range improve.Checks.Findings {
		if finding.Code == "replay_implicit_only" {
			t.Fatalf("warning fired on a Skill that has fixtures: %+v", improve.Checks.Findings)
		}
	}
}

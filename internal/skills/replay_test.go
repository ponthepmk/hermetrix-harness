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

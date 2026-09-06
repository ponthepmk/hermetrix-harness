package learning

import (
	"context"
	"strings"
	"testing"
)

// TestStructuredReviewerNamesUnverifiedProposals is the deterministic
// counterpart to TestReviewerInstructionNamesTheUnverifiedOutcomeBoundary: the
// spec's guarantee ("a proposal from an unverified turn must say so in its own
// reason") has to hold on StructuredReviewer.Review itself, not only in the
// text of a prompt a model may or may not be asked to follow. This is the path
// taken whenever a digest already carries a SuggestedSkill, and the reviewer
// wired up when no review provider is configured -- neither of those ever see
// the prompt above.
func TestStructuredReviewerNamesUnverifiedProposals(t *testing.T) {
	baseSkill := func() *SuggestedSkill {
		return &SuggestedSkill{CanonicalName: "n", ScopeKind: "user", Owner: "user",
			ChangeKind: "create", Reason: "explicit reusable procedure",
			Markdown: "---\nname: n\ndescription: \"d\"\n---\n\n1. Do it.\n"}
	}

	t.Run("empty VerifiedBy says so", func(t *testing.T) {
		digest := Digest{GoalAndConstraints: "reuse this", SuggestedSkill: baseSkill()}
		decision, err := StructuredReviewer{}.Review(context.Background(), digest)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(decision.Reason, "not verified by a run") {
			t.Fatalf("reason = %q, want it to say the approach was not verified by a run", decision.Reason)
		}
	})

	t.Run("a real receipt in VerifiedBy leaves the reason alone", func(t *testing.T) {
		digest := Digest{GoalAndConstraints: "reuse this", SuggestedSkill: baseSkill(),
			ToolReceipts: []string{"event:1:workspace.run:completed:0"},
			VerifiedBy:   []string{"event:1:workspace.run:completed:0"}}
		decision, err := StructuredReviewer{}.Review(context.Background(), digest)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(decision.Reason, "not verified by a run") {
			t.Fatalf("reason = %q, a verified proposal should not be flagged as unverified", decision.Reason)
		}
		if decision.Reason != "explicit reusable procedure" {
			t.Fatalf("reason = %q, want the suggestion's own reason unchanged", decision.Reason)
		}
	})

	t.Run("no_change stays plain", func(t *testing.T) {
		decision, err := StructuredReviewer{}.Review(context.Background(), Digest{GoalAndConstraints: "nothing here"})
		if err != nil {
			t.Fatal(err)
		}
		if decision.Kind != "no_change" {
			t.Fatalf("kind = %q, want no_change", decision.Kind)
		}
		if strings.Contains(decision.Reason, "not verified by a run") {
			t.Fatalf("reason = %q, a no_change decision has nothing to caveat", decision.Reason)
		}
	})
}

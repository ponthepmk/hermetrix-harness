package skills

import (
	"context"
	"testing"

	"hermetrix-harness/internal/store"
)

func TestAuthorityManualDefaultGatedPromotionAndRollback(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := NewService(dataStore)
	candidate, err := service.CreateCandidate(ctx, CreateCandidateInput{CanonicalName: "agent-safe-create", ScopeKind: "user",
		Origin: "agent_candidate", Owner: "agent", ChangeKind: "create", CreatedBy: "background_reviewer",
		TriggerKind: "successful_milestone", Reason: "repeated verified workflow", Markdown: authorityMarkdown("agent-safe-create")})
	if err != nil {
		t.Fatal(err)
	}
	// The shipped default lets the agent promote what it writes, so this
	// candidate is promoted without anyone changing a setting first. What makes
	// that safe is the rest of this test: the promotion is an authority action,
	// and the action can be rolled back.
	action, err := service.TryAutomatedPromotion(ctx, candidate.ID)
	if err != nil || action == nil || action.State != "completed" || action.SkillID == "" {
		t.Fatalf("default policy action=%+v err=%v", action, err)
	}

	// Switching to manual has to actually stop the next one.
	policy, err := service.SaveAuthorityPolicy(ctx, SaveAuthorityPolicyInput{Mode: AuthorityManual,
		AllowedScopes: []string{"user"}, MaxCandidateTokens: 4096,
		Actor: "test-user", Reason: "hold every promotion for review", ExpectedRevision: 1})
	if err != nil || policy.Revision != 2 {
		t.Fatalf("policy=%+v err=%v", policy, err)
	}
	held, err := service.CreateCandidate(ctx, CreateCandidateInput{CanonicalName: "agent-held-create", ScopeKind: "user",
		Origin: "agent_candidate", Owner: "agent", ChangeKind: "create", CreatedBy: "background_reviewer",
		TriggerKind: "successful_milestone", Reason: "second verified workflow", Markdown: authorityMarkdown("agent-held-create")})
	if err != nil {
		t.Fatal(err)
	}
	if heldAction, err := service.TryAutomatedPromotion(ctx, held.ID); err != nil || heldAction != nil {
		t.Fatalf("manual policy promoted anyway: action=%+v err=%v", heldAction, err)
	}
	if _, err := service.CreateAuthorityRollback(ctx, action.ID, "test-user", "undo generated capability"); err != nil {
		t.Fatal(err)
	}
	skill, err := service.GetSkill(ctx, action.SkillID)
	if err != nil || skill.State != StateArchived {
		t.Fatalf("skill=%+v err=%v", skill, err)
	}
	actions, err := service.ListAuthorityActions(ctx, 10)
	if err != nil || len(actions) != 1 || actions[0].State != "rolled_back" {
		t.Fatalf("actions=%+v err=%v", actions, err)
	}
}

func authorityMarkdown(name string) string {
	return "---\nname: " + name + "\ndescription: \"Use the verified procedure for this bounded workflow\"\ntags: [verified]\ntools: []\n---\n\n# Procedure\n\n1. Inspect the bounded evidence.\n2. Report the result and retain provenance.\n"
}

package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/runtime"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	toolruntime "hermetrix-harness/internal/tools"
)

// TestSkillManageWritesAndPromotesReversibly is the learning loop the product
// promises: the agent writes down a procedure, the shipped policy promotes it
// without anyone approving each one, and the promotion stays an action a person
// can undo afterwards.
func TestSkillManageWritesAndPromotesReversibly(t *testing.T) {
	ctx := context.Background()
	service, session := skillManageFixture(t)

	receipt := service.executeSkillTool(ctx, session, "turn-1", providers.ToolCall{
		ID: "call-1", Name: "skill_manage",
		Arguments: `{"action":"create","name":"replay-a-flaky-test","description":"When a test fails only sometimes",` +
			`"body":"# Procedure\n\n1. Run it 20 times and record each outcome.","reason":"this worked twice today"}`,
	}, toolruntime.Definition{Name: "skill_manage", Revision: "v1", Effect: "write"})
	if receipt.Status != "succeeded" {
		t.Fatalf("skill_manage failed: %s", receipt.Error)
	}
	var result struct {
		CandidateID string `json:"candidate_id"`
		Promoted    bool   `json:"promoted"`
		Outcome     string `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(receipt.Output), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Promoted {
		t.Fatalf("the shipped policy did not promote an agent-written Skill: %s", result.Outcome)
	}

	// The promotion has to be an action a person can find and undo, or
	// "review it afterwards" is not a real offer.
	actions, err := service.skills.ListAuthorityActions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var promotion *skills.AuthorityAction
	for index := range actions {
		if actions[index].ActionKind == "auto_promote" && actions[index].State == "completed" {
			promotion = &actions[index]
			break
		}
	}
	if promotion == nil {
		t.Fatal("an automatic promotion left no reversible authority action behind")
	}
	if _, err := service.skills.CreateAuthorityRollback(ctx, promotion.ID, "local-user", "not what I wanted"); err != nil {
		t.Fatalf("the promotion could not be undone: %v", err)
	}
}

// TestSkillManageRefusesToOverwriteWhatItHasNotRead pins the read-before-write
// guard: an improvement names an exact version, and only a version frozen into
// this session's catalog counts.
func TestSkillManageRefusesToOverwriteWhatItHasNotRead(t *testing.T) {
	service, session := skillManageFixture(t)
	for _, arguments := range []string{
		`{"action":"improve","skill_id":"skill_made_up","version_id":"ver_made_up","description":"d","body":"b","reason":"r"}`,
		`{"action":"improve","description":"d","body":"b","reason":"r"}`,
		`{"action":"delete","description":"d","body":"b","reason":"r"}`,
		`{"action":"create","description":"d","body":"b","reason":"r"}`,
	} {
		receipt := service.executeSkillTool(context.Background(), session, "turn-1", providers.ToolCall{
			ID: "call-x", Name: "skill_manage", Arguments: arguments,
		}, toolruntime.Definition{Name: "skill_manage", Revision: "v1", Effect: "write"})
		if receipt.Status == "succeeded" {
			t.Errorf("skill_manage accepted %s", arguments)
		}
	}
}

// TestSkillManageIsInTheDirectWaist keeps the tool reachable without a search:
// a procedure worth keeping is worth writing down in the same turn it worked.
func TestSkillManageIsInTheDirectWaist(t *testing.T) {
	registry, err := toolruntime.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	definitions := registry.Definitions()
	var found *toolruntime.Definition
	for index := range definitions {
		if definitions[index].Name == "skill_manage" {
			found = &definitions[index]
		}
	}
	if found == nil {
		t.Fatal("skill_manage is not in the direct tool waist")
	}
	if found.Effect != "write" {
		t.Errorf("skill_manage effect = %q, want write", found.Effect)
	}
	// Deliberately not approval-gated: what it writes is inert until the
	// authority policy promotes it, and that promotion is reversible.
	if found.RequiresApproval {
		t.Error("skill_manage became approval-gated; the authority policy is the gate, not the tool")
	}
	if !strings.Contains(found.Description, "skill_view") {
		t.Error("the description does not tell the model how to improve a Skill it has read")
	}
}

// skillManageFixture builds the smallest service that can run skill_manage: a
// store, a Skill service and a session whose contract is empty, which is the
// state a model is in when it has read nothing yet.
func skillManageFixture(t *testing.T) (*Service, Session) {
	t.Helper()
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	estimator := ctxcompiler.NewAdaptiveEstimator()
	compiler := ctxcompiler.NewCompiler(estimator, ctxcompiler.NewBlobSpiller(dataStore.Blobs), ctxcompiler.StructuredCompactor{})
	registry, err := toolruntime.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	skillService := skills.NewService(dataStore)
	service := NewService(dataStore, providers.NewService(dataStore, nil), compiler, estimator,
		runtime.NewInferenceGate(), registry, skillService)
	return service, Session{ID: "session-1", Contract: SessionContract{}}
}

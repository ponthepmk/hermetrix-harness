package learning

import (
	"context"
	"testing"
)

func TestProjectTaskReviewCannotPromoteGlobalSkill(t *testing.T) {
	ctx := context.Background()
	suggestion := &SuggestedSkill{CanonicalName: "parcel-procedure", ScopeKind: "user", Owner: "model", ChangeKind: "create", Reason: "verified local procedure", Markdown: "---\nname: parcel-procedure\ndescription: \"Verify local parcel lifecycle\"\ntools: []\n---\n\n# Procedure\n\n1. Inspect real order receipts.\n"}
	service, skillService, _ := setupLearning(t, stubReviewer{decision: Decision{Kind: "create", Reason: "reusable", SuggestedSkill: suggestion}})
	_, err := service.store.DB.Exec(`INSERT INTO projects(id,name,root_path,state,created_at,updated_at) VALUES('delivery-project','Delivery','/delivery','active','now','now');INSERT INTO durable_tasks(id,project_id,title,objective,original_request,state,created_at,updated_at) VALUES('delivery-task','delivery-project','Delivery','Delivery','Delivery','verifying','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := service.Enqueue(ctx, EnqueueInput{SessionID: "task:delivery-task", MilestoneID: "auth-proof", TriggerKind: "explicit_learn", Digest: Digest{Outcome: "verified auth only"}})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.RunNext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if completed.ID != job.ID || completed.CandidateID == "" {
		t.Fatalf("missing actual candidate: %+v", completed)
	}
	candidate, err := skillService.GetCandidate(ctx, completed.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.ScopeKind != "workspace" || candidate.ScopeRef != "delivery-project" || candidate.Owner != "user" {
		t.Fatalf("project evidence escaped scope: %+v", candidate)
	}
	active, err := skillService.ListSkills(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) > 1 || (len(active) == 1 && (active[0].ScopeKind != "workspace" || active[0].ScopeRef != "delivery-project")) {
		t.Fatalf("unexpected promoted skill: %+v", active)
	}
}
func TestProjectEvidenceCannotModifyGlobalTargetOrMissingTask(t *testing.T) {
	ctx := context.Background()
	service, _, _ := setupLearning(t, StructuredReviewer{})
	_, err := service.store.DB.Exec(`INSERT INTO projects(id,name,root_path,state,created_at,updated_at) VALUES('p','P','/p','active','now','now');INSERT INTO durable_tasks(id,project_id,title,objective,original_request,state,created_at,updated_at) VALUES('t','p','T','T','T','verifying','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []SuggestedSkill{{ScopeKind: "user", TargetSkillID: "global"}, {ScopeKind: "workspace", ScopeRef: "other", TargetSkillID: "other-skill"}} {
		if err = service.boundSuggestionToSource(ctx, "task:t", &target); err == nil {
			t.Fatalf("cross-scope target allowed: %+v", target)
		}
	}
	missing := SuggestedSkill{ScopeKind: "user"}
	if err = service.boundSuggestionToSource(ctx, "task:missing", &missing); err == nil {
		t.Fatal("missing durable task silently granted global scope")
	}
	local := SuggestedSkill{ScopeKind: "workspace", ScopeRef: "p", TargetSkillID: "local-skill"}
	if err = service.boundSuggestionToSource(ctx, "task:t", &local); err != nil {
		t.Fatal(err)
	}
}

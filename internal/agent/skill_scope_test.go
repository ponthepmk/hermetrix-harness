package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestSessionSkillCatalogHonorsWorkspaceScope(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	projectA := createTestProject(t, service, t.TempDir())
	projectB := createTestProject(t, service, t.TempDir())
	global := seedSkill(t, service, "global-procedure", "GLOBAL")
	scoped := seedSkill(t, service, "easiiot-only", "EASIIOT")
	unbound := seedSkill(t, service, "unbound-workspace", "UNBOUND")
	agentOnly := seedSkill(t, service, "other-agent-only", "OTHER_AGENT")
	for _, row := range []struct{ id, kind, ref string }{
		{scoped.ID, "workspace", projectA},
		{unbound.ID, "workspace", ""},
		{agentOnly.ID, "agent", "another-agent"},
	} {
		if _, err := service.store.DB.ExecContext(ctx, "UPDATE skills SET scope_kind=?,scope_ref=? WHERE id=?", row.kind, row.ref, row.id); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, project string
		wantScoped    bool
	}{{"matching workspace", projectA, true}, {"other workspace", projectB, false}, {"personal chat", "", false}} {
		t.Run(tc.name, func(t *testing.T) {
			session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID, ProjectID: tc.project, ContextProfile: "compact-32k"})
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]bool{}
			for _, binding := range session.Contract.SkillCatalog {
				got[binding.SkillID] = true
			}
			if !got[global.ID] || got[scoped.ID] != tc.wantScoped || got[unbound.ID] || got[agentOnly.ID] {
				t.Fatalf("scope leaked or matching skill missing: %+v", session.Contract.SkillCatalog)
			}
		})
	}
	// Simulate a contract saved by the old runtime: neither the search tool nor
	// the body tool may expose its out-of-scope entry after an upgrade.
	stale := Session{ID: "old-session", ProjectID: projectB, Contract: SessionContract{ProjectID: projectB,
		SkillCatalog:   []SessionSkillBinding{{SkillID: scoped.ID, VersionID: scoped.CurrentVersionID, CanonicalName: scoped.CanonicalName}},
		SelectedSkills: []SessionSkillBinding{{SkillID: scoped.ID, VersionID: scoped.CurrentVersionID}}}}
	search := callSkillTool(t, service, stale, "skill_search", `{"query":"easiiot"}`)
	if search.Status != "succeeded" || strings.Contains(search.Output, scoped.ID) {
		t.Fatalf("stale catalog search leaked: %+v", search)
	}
	view := callSkillTool(t, service, stale, "skill_view", fmt.Sprintf(`{"skill_id":%q,"version_id":%q}`, scoped.ID, scoped.CurrentVersionID))
	if view.Status != "failed" || strings.Contains(view.Output, "EASIIOT") {
		t.Fatalf("stale catalog body leaked: %+v", view)
	}
	filtered, err := service.scopedSkillContract(ctx, stale.Contract)
	if err != nil || len(filtered.SelectedSkills) != 0 || len(filtered.SkillCatalog) != 0 {
		t.Fatalf("stale preloaded body remained eligible: %+v err=%v", filtered, err)
	}
}

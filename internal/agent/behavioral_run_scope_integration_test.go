package agent

import (
	"context"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestBehavioralRunScopeUsesOwnedProjectAndSession(t *testing.T) {
	service, profile, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()

	projectID := createTestProject(t, service, t.TempDir())
	owner, err := service.store.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatalf("OwnerPrincipalID: %v", err)
	}
	if owner == "" {
		t.Fatalf("owner principal ID empty")
	}

	session, err := service.CreateSession(ctx, CreateSessionInput{
		ProviderID:     profile.ID,
		ProjectID:      projectID,
		ContextProfile: "compact-32k",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ProjectID != projectID {
		t.Fatalf("session project mismatch: %v", session.ProjectID)
	}
	if session.OwnerPrincipalID != owner {
		t.Fatalf("session owner mismatch: %v", session.OwnerPrincipalID)
	}
	if session.State != "active" {
		t.Fatalf("session state: %v", session.State)
	}

	candidate := behavioralRunScopeCandidate(t, service.skills, "workspace", projectID)
	b := behavioralRunScopeBindings(t)
	b.ProviderID = profile.ID
	b.ProviderRevision = providers.Revision(profile)
	b.Model = profile.Model
	b.ProjectID = projectID
	b.SessionID = session.ID

	run, err := service.skills.PrepareBehavioralRun(ctx, "hostID-scope-owned-positive", candidate.ID, b)
	if err != nil {
		t.Fatalf("PrepareBehavioralRun: %v", err)
	}
	if run.ID != "hostID-scope-owned-positive" {
		t.Fatalf("run ID: %v", run.ID)
	}
	if run.State != "prepared" {
		t.Fatalf("run state: %v", run.State)
	}
	if run.Manifest.OwnerPrincipalID != owner {
		t.Fatalf("manifest owner: %v", run.Manifest.OwnerPrincipalID)
	}
	if run.Manifest.ScopeRef != projectID {
		t.Fatalf("manifest scope ref: %v", run.Manifest.ScopeRef)
	}
	if run.Manifest.Bindings.ProjectID != projectID {
		t.Fatalf("bindings project: %v", run.Manifest.Bindings.ProjectID)
	}
	if run.Manifest.Bindings.SessionID != session.ID {
		t.Fatalf("bindings session: %v", run.Manifest.Bindings.SessionID)
	}

	var count int
	if err := service.store.DB.QueryRow("SELECT COUNT(*) FROM behavioral_eval_runs").Scan(&count); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 behavioral_eval_runs, got %d", count)
	}
}

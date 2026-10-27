package agent

import (
	"context"
	"testing"

	providers "hermetrix-harness/internal/providers"
	skills "hermetrix-harness/internal/skills"
)

func behavioralRunClaimScopeFixture(t *testing.T) (*Service, skills.Candidate, skills.BehavioralRunBindings) {
	t.Helper()
	service, profile, cleanup := testAgentService(t, successProviderServer(t))
	t.Cleanup(cleanup)

	projectID := createTestProject(t, service, t.TempDir())
	if projectID == "" {
		t.Fatal("createTestProject returned empty project ID")
	}

	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{
		ProviderID:     profile.ID,
		ProjectID:      projectID,
		ContextProfile: "compact-32k",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ID == "" {
		t.Fatal("session ID is empty")
	}
	if session.ProjectID != projectID {
		t.Fatalf("session project mismatch: got %q, want %q", session.ProjectID, projectID)
	}
	if session.OwnerPrincipalID == "" {
		t.Fatal("session owner principal ID is empty")
	}
	if session.State != "active" {
		t.Fatalf("session not active: got %q", session.State)
	}

	owner, err := service.store.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatalf("OwnerPrincipalID: %v", err)
	}
	if owner == "" {
		t.Fatal("owner principal ID is empty")
	}
	if session.OwnerPrincipalID != owner {
		t.Fatalf("session owner mismatch: got %q, want %q", session.OwnerPrincipalID, owner)
	}

	candidate := behavioralRunScopeCandidate(t, service.skills, "workspace", projectID)
	if candidate.ID == "" {
		t.Fatal("candidate ID is empty")
	}

	bindings := behavioralRunScopeBindings(t)
	bindings.ProviderID = profile.ID
	bindings.ProviderRevision = providers.Revision(profile)
	bindings.Model = profile.Model
	bindings.ProjectID = projectID
	bindings.SessionID = session.ID

	return service, candidate, bindings
}

package agent

import (
	"context"
	"errors"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/skills"
)

func TestBehavioralRunRefusesCrossProjectSession(t *testing.T) {
	service, profile, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()

	projectA := createTestProject(t, service, t.TempDir())
	projectB := createTestProject(t, service, t.TempDir())
	if projectA == projectB {
		t.Fatalf("projects not distinct")
	}

	owner, err := service.store.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatalf("OwnerPrincipalID: %v", err)
	}
	if owner == "" {
		t.Fatalf("owner principal ID empty")
	}

	session, err := service.CreateSession(ctx, CreateSessionInput{
		ProviderID:     profile.ID,
		ProjectID:      projectB,
		ContextProfile: "compact-32k",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ProjectID != projectB {
		t.Fatalf("session project mismatch: %v", session.ProjectID)
	}
	if session.OwnerPrincipalID != owner {
		t.Fatalf("session owner mismatch: %v", session.OwnerPrincipalID)
	}
	if session.State != "active" {
		t.Fatalf("session state: %v", session.State)
	}

	candidate := behavioralRunScopeCandidate(t, service.skills, "workspace", projectA)
	b := behavioralRunScopeBindings(t)
	b.ProviderID = profile.ID
	b.ProviderRevision = providers.Revision(profile)
	b.Model = profile.Model
	b.ProjectID = projectA
	b.SessionID = session.ID

	run, err := service.skills.PrepareBehavioralRun(ctx, "hostID-wrong-session-project", candidate.ID, b)
	if !errors.Is(err, skills.ErrBehavioralRunBindings) {
		t.Fatalf("expected ErrBehavioralRunBindings, got %v", err)
	}
	if run.ID != "" {
		t.Fatalf("expected zero run ID, got %v", run.ID)
	}

	b.ProjectID = projectB
	run, err = service.skills.PrepareBehavioralRun(ctx, "hostID-wrong-candidate-project", candidate.ID, b)
	if !errors.Is(err, skills.ErrBehavioralRunBindings) {
		t.Fatalf("expected ErrBehavioralRunBindings, got %v", err)
	}
	if run.ID != "" {
		t.Fatalf("expected zero run ID, got %v", run.ID)
	}

	var count int
	if err := service.store.DB.QueryRow("SELECT COUNT(*) FROM behavioral_eval_runs").Scan(&count); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 behavioral_eval_runs, got %d", count)
	}
}

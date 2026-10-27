package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/skills"
)

func TestBehavioralRunRefusesMissingScopeReferences(t *testing.T) {
	service, profile, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()

	projectID := createTestProject(t, service, t.TempDir())
	if projectID == "" {
		t.Fatalf("project ID empty")
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
		ProjectID:      projectID,
		ContextProfile: "compact-32k",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ID == "" {
		t.Fatalf("session ID empty")
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

	candidate := behavioralRunScopeCandidate(t, service.skills, "user", "")
	if candidate.ID == "" {
		t.Fatalf("candidate ID empty")
	}
	reloaded, err := service.skills.GetCandidate(ctx, candidate.ID)
	if err != nil {
		t.Fatalf("GetCandidate: %v", err)
	}
	if reloaded.OwnerPrincipalID != owner {
		t.Fatalf("candidate owner mismatch: %v", reloaded.OwnerPrincipalID)
	}
	if candidate.ScopeKind != "user" {
		t.Fatalf("candidate scope kind: %v", candidate.ScopeKind)
	}
	if candidate.ScopeRef != "" {
		t.Fatalf("user candidate scope ref must be empty")
	}

	b := behavioralRunScopeBindings(t)
	b.ProviderID = profile.ID
	b.ProviderRevision = providers.Revision(profile)
	b.Model = profile.Model

	cases := []struct {
		hostID    string
		projectID string
		sessionID string
	}{
		{"host-missing-project", "absent-project", ""},
		{"host-missing-session", projectID, "absent-session"},
		{"host-empty-project", "", session.ID},
	}
	for _, c := range cases {
		b.ProjectID = c.projectID
		b.SessionID = c.sessionID

		run, err := service.skills.PrepareBehavioralRun(ctx, c.hostID, candidate.ID, b)
		if !errors.Is(err, skills.ErrBehavioralRunBindings) {
			t.Fatalf("PrepareBehavioralRun: expected ErrBehavioralRunBindings, got %v", err)
		}
		if !reflect.DeepEqual(run, skills.BehavioralRun{}) {
			t.Fatalf("expected zero BehavioralRun, got %+v", run)
		}
	}

	var count int
	if err := service.store.DB.QueryRow("SELECT COUNT(*) FROM behavioral_eval_runs").Scan(&count); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 behavioral_eval_runs, got %d", count)
	}

	b.ProjectID = projectID
	b.SessionID = session.ID

	run, err := service.skills.PrepareBehavioralRun(ctx, "host-positive", candidate.ID, b)
	if err != nil {
		t.Fatalf("positive PrepareBehavioralRun: %v", err)
	}
	if run.ID != "host-positive" {
		t.Fatalf("positive run ID empty")
	}
	if run.State != "prepared" {
		t.Fatalf("positive run state: %v", run.State)
	}
	if run.Manifest.OwnerPrincipalID != owner {
		t.Fatalf("manifest owner mismatch: %v", run.Manifest.OwnerPrincipalID)
	}
	if run.Manifest.ScopeKind != "user" {
		t.Fatalf("manifest scope kind: %v", run.Manifest.ScopeKind)
	}
	if run.Manifest.ScopeRef != "" {
		t.Fatalf("manifest scope ref must be empty")
	}
	if run.Manifest.Bindings.ProjectID != projectID {
		t.Fatalf("manifest project mismatch: %v", run.Manifest.Bindings.ProjectID)
	}
	if run.Manifest.Bindings.SessionID != session.ID {
		t.Fatalf("manifest session mismatch: %v", run.Manifest.Bindings.SessionID)
	}

	if err := service.store.DB.QueryRow("SELECT COUNT(*) FROM behavioral_eval_runs").Scan(&count); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 behavioral_eval_runs, got %d", count)
	}
}

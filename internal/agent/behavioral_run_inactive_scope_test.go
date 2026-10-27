package agent

import (
	"context"
	"errors"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/skills"
)

func TestBehavioralRunRefusesInactiveOwnedReferences(t *testing.T) {
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

	for _, tc := range []struct {
		name     string
		table    string
		id       string
		inactive string
		active   string
	}{
		{"session", "agent_sessions", session.ID, "closed", "active"},
		{"project", "projects", projectID, "archived", "active"},
	} {
		deactivateRes, deactivateErr := service.store.DB.ExecContext(ctx, "UPDATE "+tc.table+" SET state=? WHERE id=?", tc.inactive, tc.id)
		if deactivateErr != nil {
			t.Fatalf("mark %s inactive: %v", tc.name, deactivateErr)
		}
		deactivateAffected, deactivateAffectedErr := deactivateRes.RowsAffected()
		if deactivateAffectedErr != nil || deactivateAffected != 1 {
			t.Fatalf("mark %s inactive: expected 1 affected row, got %d: %v", tc.name, deactivateAffected, deactivateAffectedErr)
		}

		run, err := service.skills.PrepareBehavioralRun(ctx, "hostID-inactive-"+tc.name, candidate.ID, b)
		if !errors.Is(err, skills.ErrBehavioralRunBindings) {
			t.Fatalf("PrepareBehavioralRun with inactive %s: expected ErrBehavioralRunBindings, got %v", tc.name, err)
		}
		if run.ID != "" {
			t.Fatalf("inactive %s: expected zero run, got ID %v", tc.name, run.ID)
		}

		restoreRes, restoreErr := service.store.DB.ExecContext(ctx, "UPDATE "+tc.table+" SET state=? WHERE id=?", tc.active, tc.id)
		if restoreErr != nil {
			t.Fatalf("restore %s active: %v", tc.name, restoreErr)
		}
		restoreAffected, restoreAffectedErr := restoreRes.RowsAffected()
		if restoreAffectedErr != nil || restoreAffected != 1 {
			t.Fatalf("restore %s active: expected 1 affected row, got %d: %v", tc.name, restoreAffected, restoreAffectedErr)
		}
	}

	run, err := service.skills.PrepareBehavioralRun(ctx, "hostID-restored-owned", candidate.ID, b)
	if err != nil {
		t.Fatalf("PrepareBehavioralRun restored-owned: %v", err)
	}
	if run.ID != "hostID-restored-owned" {
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

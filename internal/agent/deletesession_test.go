package agent

import (
	"context"
	"strings"
	"testing"
)

// TestDeleteSessionRemovesTheChatAndKeepsTheEvidence pins what "delete" means
// here: the conversation goes, what was learned from it stays.
func TestDeleteSessionRemovesTheChatAndKeepsTheEvidence(t *testing.T) {
	service, _ := skillManageFixture(t)
	ctx := context.Background()
	store := service.store.DB

	seedProvider(t, service)
	if _, err := store.ExecContext(ctx, `INSERT INTO agent_sessions
    (id,title,provider_id,context_profile,state,created_at,updated_at)
    VALUES('sess-1','Doomed','p1','compact-32k','idle','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecContext(ctx, `INSERT INTO agent_events
    (id,session_id,turn_id,sequence,event_kind,role,content,metadata_json,model,created_at)
    VALUES('ev-1','sess-1','t1',1,'message','user','hello','{}','m','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	// Evidence about how a model actually behaved, measured during that
	// session. It is what calibrates future estimates, and it must survive.
	if _, err := store.ExecContext(ctx, `INSERT INTO token_observations
    (id,session_id,turn_id,step_number,provider_id,model,profile_name,context_snapshot_id,
     predicted_input,actual_input,created_at)
    VALUES('obs-1','sess-1','t1',1,'p1','m','compact-32k','snap-1',100,120,'2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	if err := service.DeleteSession(ctx, "sess-1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	var sessions, events, observations int
	_ = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_sessions WHERE id='sess-1'`).Scan(&sessions)
	_ = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_events WHERE session_id='sess-1'`).Scan(&events)
	_ = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM token_observations WHERE session_id='sess-1'`).Scan(&observations)
	if sessions != 0 || events != 0 {
		t.Errorf("the conversation survived: %d sessions, %d events", sessions, events)
	}
	if observations != 1 {
		t.Errorf("calibration evidence was destroyed with the chat: %d observations left", observations)
	}
	if err := service.DeleteSession(ctx, "sess-1"); err == nil {
		t.Error("deleting a session that is gone reported success")
	}
}

// TestDeleteRefusesASessionMidTurn stops a delete from pulling rows out from
// under a turn that is still writing them.
func TestDeleteRefusesASessionMidTurn(t *testing.T) {
	service, _ := skillManageFixture(t)
	ctx := context.Background()
	seedProvider(t, service)
	if _, err := service.store.DB.ExecContext(ctx, `INSERT INTO agent_sessions
    (id,title,provider_id,context_profile,state,active_turn_id,created_at,updated_at)
    VALUES('sess-2','Busy','p1','compact-32k','running','turn-9','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	err := service.DeleteSession(ctx, "sess-2")
	if err == nil {
		t.Fatal("a running session was deleted")
	}
	if !strings.Contains(err.Error(), "turn running") {
		t.Errorf("refusal = %v", err)
	}
}

// seedProvider satisfies the session table's foreign key without pulling in the
// whole provider service.
func seedProvider(t *testing.T, service *Service) {
	t.Helper()
	_, err := service.store.DB.Exec(`INSERT OR IGNORE INTO provider_profiles
    (id,name,adapter_kind,base_url,model,api_key_env,context_window,context_evidence,max_output_tokens,enabled,created_at,updated_at)
    VALUES('p1','P','openai-compatible','https://host.example/v1','m','',131072,'declared',4096,1,
    '2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
}

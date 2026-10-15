package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hermetrix-harness/internal/identity"
)

func TestStopCancelsStreamBeforeQueueStarts(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slots" {
			fmt.Fprint(w, `[{"id":0,"is_processing":false}]`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	s, p, cleanup := testAgentService(t, server)
	defer cleanup()
	ctx := context.Background()
	session, err := s.CreateSession(ctx, CreateSessionInput{ProviderID: p.ID, ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		result, err := s.RunTurn(ctx, session.ID, TurnInput{Content: "begin"}, nil)
		if err == nil && result.FinishReason != "stopped" {
			err = fmt.Errorf("result=%+v", result)
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("provider never started")
	}
	session, _ = s.GetSession(ctx, session.ID)
	if _, err = s.StopTurn(identity.WithPrincipal(ctx, "another-owner"), session.ID, session.ActiveTurnID); err == nil {
		t.Fatal("foreign owner stopped turn")
	}
	if _, err = s.StopTurn(ctx, session.ID, "wrong-turn"); err == nil {
		t.Fatal("stale stop accepted")
	}
	q, err := s.QueueMessage(ctx, session.ID, QueueInput{Content: "next", IdempotencyKey: "q1"})
	if err != nil {
		t.Fatal(err)
	}
	if work, err := s.DrainOneQueued(ctx); err != nil || work {
		t.Fatalf("queue ran before stop: %v %v", work, err)
	}
	control, err := s.StopTurn(ctx, session.ID, session.ActiveTurnID)
	if err != nil || control == nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stop did not terminate stream")
	}
	control, err = s.StopTurn(ctx, session.ID, session.ActiveTurnID)
	if err != nil || control.State != "stopped" {
		t.Fatalf("duplicate stop: %+v %v", control, err)
	}
	fresh, _ := s.GetSession(ctx, session.ID)
	if fresh.State != "active" || fresh.ActiveTurnID != "" {
		t.Fatalf("lease not released: %+v", fresh)
	}
	if _, err = s.CancelQueuedMessage(ctx, session.ID, q.ID); err != nil {
		t.Fatal(err)
	}
	events, _ := s.ListEvents(ctx, session.ID)
	count := 0
	for _, e := range events {
		if e.EventKind == "turn_stopped" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("stop receipts=%d", count)
	}
}

func TestQueuedMessagesFIFOIdempotencyAndScopeDrift(t *testing.T) {
	s, p, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	session, err := s.CreateSession(ctx, CreateSessionInput{ProviderID: p.ID, ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	// Freeze first-turn skill selection before queueing against this contract.
	if _, err = s.RunTurn(ctx, session.ID, TurnInput{Content: "warmup"}, nil); err != nil {
		t.Fatal(err)
	}
	input := QueueInput{Content: "first queued", IdempotencyKey: "first"}
	first, err := s.QueueMessage(ctx, session.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.QueueMessage(ctx, session.ID, input)
	if err != nil || duplicate.ID != first.ID {
		t.Fatal("queue retry duplicated message", err)
	}
	input.Content = "different"
	if _, err = s.QueueMessage(ctx, session.ID, input); err == nil {
		t.Fatal("key accepted new content")
	}
	second, err := s.QueueMessage(ctx, session.ID, QueueInput{Content: "second queued", IdempotencyKey: "second"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if work, err := s.DrainOneQueued(ctx); err != nil || !work {
			t.Fatal("drain failed", work, err)
		}
	}
	for _, id := range []string{first.ID, second.ID} {
		item, _ := s.queuedMessage(ctx, session.ID, id)
		if item.State != "completed" {
			t.Fatalf("item=%+v", item)
		}
	}
	events, _ := s.ListEvents(ctx, session.ID)
	var texts []string
	for _, e := range events {
		if e.Role == "user" {
			texts = append(texts, e.Content)
		}
	}
	if len(texts) != 3 || texts[1] != "first queued" || texts[2] != "second queued" {
		t.Fatalf("order=%v", texts)
	}
	drift, err := s.QueueMessage(ctx, session.ID, QueueInput{Content: "bound before drift", IdempotencyKey: "drift"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.store.DB.ExecContext(ctx, `UPDATE agent_sessions SET cache_epoch=cache_epoch+1 WHERE id=?`, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if work, err := s.DrainOneQueued(ctx); err != nil || !work {
		t.Fatal(work, err)
	}
	item, _ := s.queuedMessage(ctx, session.ID, drift.ID)
	if item.State != "needs_review" {
		t.Fatalf("scope drift replayed: %+v", item)
	}
}

func TestStopPendingApprovalCancelsWriteAndSteerRetiresOldQueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"write","type":"function","function":{"name":"workspace.write_file","arguments":"{\"path\":\"never.txt\",\"content\":\"bad\",\"expected_sha256\":\"absent\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	root := t.TempDir()
	s, p, cleanup := testAgentServiceAtRoot(t, server, root)
	defer cleanup()
	ctx := context.Background()
	session, err := s.CreateSession(ctx, CreateSessionInput{ProviderID: p.ID, ProjectID: createTestProject(t, s, root), ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := s.RunTurn(ctx, session.ID, TurnInput{Content: "write"}, nil)
	if err != nil || paused.Approval == nil {
		t.Fatalf("pause=%+v %v", paused, err)
	}
	old, err := s.QueueMessage(ctx, session.ID, QueueInput{Content: "obsolete", IdempotencyKey: "old"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.QueueMessage(ctx, session.ID, QueueInput{Content: "new direction", IdempotencyKey: "steer", Mode: "steer", TurnID: paused.TurnID})
	if err != nil {
		t.Fatal(err)
	}
	if next.State != "queued" {
		t.Fatalf("steer=%+v", next)
	}
	old, _ = s.queuedMessage(ctx, session.ID, old.ID)
	if old.State != "needs_review" {
		t.Fatalf("old queue=%+v", old)
	}
	approval, _ := s.GetApproval(ctx, paused.Approval.ID)
	if approval.State != "cancelled" {
		t.Fatalf("approval=%+v", approval)
	}
	if _, err = s.DecideApproval(ctx, approval.ID, ApprovalDecisionInput{Actor: "user", Decision: "approve"}, nil); err == nil {
		t.Fatal("stopped approval replayed")
	}
	if _, err = os.Stat(filepath.Join(root, "never.txt")); !os.IsNotExist(err) {
		t.Fatal("stopped write affected disk", err)
	}
}

func TestRestartDoesNotReplayInterruptedStopOrQueue(t *testing.T) {
	s, p, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	session, err := s.CreateSession(ctx, CreateSessionInput{ProviderID: p.ID, ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	now := formatTime(time.Now().UTC())
	_, err = s.store.DB.ExecContext(ctx, `UPDATE agent_sessions SET state='running',active_turn_id='interrupted-turn' WHERE id=?`, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO agent_turn_controls(session_id,turn_id,state,requested_at,updated_at) VALUES(?,'interrupted-turn','stopping',?,?)`, session.ID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.QueueMessage(ctx, session.ID, QueueInput{Content: "must wait", IdempotencyKey: "after-crash"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverTurnControls(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverMessageQueue(ctx); err != nil {
		t.Fatal(err)
	}
	if work, err := s.DrainOneQueued(ctx); err != nil || work {
		t.Fatalf("replayed blocked queue: %v %v", work, err)
	}
	fresh, _ := s.GetSession(ctx, session.ID)
	if fresh.State != "control_blocked" || fresh.ActiveTurnID != "interrupted-turn" {
		t.Fatalf("lost ownership: %+v", fresh)
	}
	queued, _ := s.queuedMessage(ctx, session.ID, q.ID)
	if queued.State != "needs_review" || queued.Content != "must wait" {
		t.Fatalf("lost queued text: %+v", queued)
	}
}

func TestStopRetainsCommandLeaseUntilPersistedOwnedJobTerminates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[{"id":0,"is_processing":false}]`) }))
	s, p, cleanup := testAgentService(t, server)
	defer cleanup()
	ctx := context.Background()
	session, err := s.CreateSession(ctx, CreateSessionInput{ProviderID: p.ID, ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{results: map[string]RunResult{"owned": {JobID: "owned", State: "running"}, "foreign": {JobID: "foreign", State: "running"}}}
	s.runner = runner
	s.runs.claim(session.ID, "owned", 2)
	_, err = s.store.DB.ExecContext(ctx, `UPDATE agent_sessions SET state='awaiting_approval',active_turn_id='command-turn' WHERE id=?`, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	control, err := s.StopTurn(ctx, session.ID, "command-turn")
	if err != nil || control.State != "needs_review" {
		t.Fatalf("prematurely released command: %+v %v", control, err)
	}
	fresh, _ := s.GetSession(ctx, session.ID)
	if fresh.State != "control_blocked" || fresh.ActiveTurnID != "command-turn" {
		t.Fatal("lost command lease")
	}
	if len(runner.canceled) != 1 || runner.canceled[0] != "owned" {
		t.Fatalf("wrong cancellation scope: %v", runner.canceled)
	}
	// Reconstruct ownership from the stop receipt after process-local memory is lost.
	s.runs = runTracker{}
	runner.finish("owned", RunResult{JobID: "owned", State: "canceled"})
	control, err = s.ReconcileStoppedTurn(ctx, session.ID, "command-turn")
	if err != nil || control.State != "stopped" {
		t.Fatalf("terminal receipt not reconciled: %+v %v", control, err)
	}
	fresh, _ = s.GetSession(ctx, session.ID)
	if fresh.State != "active" {
		t.Fatal("confirmed terminal job still blocked")
	}
	if len(runner.canceled) != 1 || len(runner.started) != 0 {
		t.Fatal("reconciliation replayed command")
	}
}

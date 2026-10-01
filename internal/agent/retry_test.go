package agent

import (
	"context"
	"encoding/json"
	"hermetrix-harness/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestAutoRetryWaitsFiveMinutesAndStopsAfterSuccess(t *testing.T) {
	var calls atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporary gateway failure", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "Finished the original task."}, "finish_reason": "stop"}}})
	}))
	service, p, cleanup := testAgentService(t, gateway)
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: p.ID, ContextProfile: "compact-16k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.RunTurn(ctx, session.ID, TurnInput{Content: "Complete this task"}, nil); err == nil {
		t.Fatal("expected first provider failure")
	}
	status, err := service.RetryStatus(ctx, session.ID)
	if err != nil || status == nil || status.State != "pending" {
		t.Fatalf("schedule %v %v", status, err)
	}
	events, _ := service.ListEvents(ctx, session.ID)
	failed := events[len(events)-1]
	if delta := status.NextAttemptAt.Sub(failed.CreatedAt); delta != 5*time.Minute {
		t.Fatalf("retry delay %v", delta)
	}
	if ran, err := service.RunDueAutoRetry(ctx, status.NextAttemptAt.Add(-time.Nanosecond)); ran || err != nil {
		t.Fatalf("ran early %v %v", ran, err)
	}
	if ran, err := service.RunDueAutoRetry(ctx, status.NextAttemptAt); !ran || err != nil {
		t.Fatalf("retry %v %v", ran, err)
	}
	status, _ = service.RetryStatus(ctx, session.ID)
	if status.State != "completed" || status.Attempts != 1 || calls.Load() != 2 {
		t.Fatalf("completion %v calls=%d", status, calls.Load())
	}
	if ran, err := service.RunDueAutoRetry(ctx, time.Now().Add(time.Hour)); ran || err != nil {
		t.Fatalf("completed task retried %v %v", ran, err)
	}
}

func TestPartialAutoRetryAndPauseSurviveRestart(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "Some work remains. <!-- hermetrix:continue -->"}, "finish_reason": "stop"}}})
	}))
	service, p, cleanup := testAgentService(t, gateway)
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: p.ID, ContextProfile: "compact-16k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "Review everything"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.AssistantEvent.Metadata["continuation_needed"] != true || result.AssistantEvent.Content != "Some work remains." {
		t.Fatalf("partial %+v", result.AssistantEvent)
	}
	before, _ := service.RetryStatus(ctx, session.ID)
	root := service.store.Root
	if err = service.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	service.store = reopened
	defer reopened.Close()
	if err = service.RecoverAutoRetries(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ := service.RetryStatus(ctx, session.ID)
	if !after.NextAttemptAt.Equal(before.NextAttemptAt) {
		t.Fatal("restart reset the deadline")
	}
	if err = service.PauseAutoRetry(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if err = service.RecoverAutoRetries(ctx); err != nil {
		t.Fatal(err)
	}
	if ran, err := service.RunDueAutoRetry(ctx, time.Now().Add(time.Hour)); ran || err != nil {
		t.Fatalf("paused retry %v %v", ran, err)
	}
}

func TestAutomaticRetryDoesNotOverrideNewUserRequest(t *testing.T) {
	var calls atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "offline", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "New request answered"}, "finish_reason": "stop"}}})
	}))
	service, p, cleanup := testAgentService(t, gateway)
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: p.ID, ContextProfile: "compact-16k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	service.RunTurn(ctx, session.ID, TurnInput{Content: "Old request"}, nil)
	events, _ := service.ListEvents(ctx, session.ID)
	source := events[len(events)-1].ID
	if _, err = service.RunTurn(ctx, session.ID, TurnInput{Content: "Different new request"}, nil); err != nil {
		t.Fatal(err)
	}
	service.store.DB.ExecContext(ctx, `UPDATE agent_retries SET state='running' WHERE session_id=?`, session.ID)
	if _, err = service.RunTurn(ctx, session.ID, TurnInput{Content: autoContinuation, IdempotencyKey: "auto-retry:" + source, ExpectedOutcomeID: source}, nil); err == nil {
		t.Fatal("stale retry acquired newer conversation")
	}
	if calls.Load() != 2 {
		t.Fatal("stale retry reached provider")
	}
}

func TestAutomaticContinuationHonorsApprovalMode(t *testing.T) {
	for _, mode := range []string{"auto", "ask_all"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if n == 1 {
					http.Error(w, "offline", 503)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				message := map[string]any{"role": "assistant", "content": "The task is complete"}
				finish := "stop"
				if n == 2 {
					message = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "auto-write", "type": "function", "function": map[string]any{"name": "workspace.write_file", "arguments": `{"path":"approved.txt","content":"approved once","expected_sha256":"absent"}`}}}}
					finish = "tool_calls"
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": finish}}})
			}))
			service, p, cleanup := testAgentService(t, gateway)
			defer cleanup()
			ctx := context.Background()
			root := t.TempDir()
			session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: p.ID, ProjectID: createTestProject(t, service, root), ContextProfile: "compact-16k", QualificationOverride: testQualificationOverride()})
			if err != nil {
				t.Fatal(err)
			}
			actor, err := service.store.LocalPrincipalID(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = service.SetApprovalMode(ctx, session.ID, mode, actor); err != nil {
				t.Fatal(err)
			}
			service.RunTurn(ctx, session.ID, TurnInput{Content: "Create approved.txt once"}, nil)
			status, _ := service.RetryStatus(ctx, session.ID)
			if ran, err := service.RunDueAutoRetry(ctx, status.NextAttemptAt); !ran || err != nil {
				t.Fatalf("retry %v %v", ran, err)
			}
			current, _ := service.GetSession(ctx, session.ID)
			data, readErr := os.ReadFile(filepath.Join(root, "approved.txt"))
			if mode == "auto" {
				if readErr != nil || string(data) != "approved once" || current.State != "active" || calls.Load() != 3 {
					t.Fatalf("auto: data=%q err=%v state=%s calls=%d", data, readErr, current.State, calls.Load())
				}
				approvals, _ := service.ListApprovals(ctx, session.ID)
				if len(approvals) != 1 || approvals[0].State != "executed" {
					t.Fatalf("exact approval audit %+v", approvals)
				}
			} else {
				if !os.IsNotExist(readErr) || current.State != "awaiting_approval" {
					t.Fatalf("ask all changed file or skipped approval: %v %s", readErr, current.State)
				}
				if ran, err := service.RunDueAutoRetry(ctx, time.Now().Add(time.Hour)); ran || err != nil {
					t.Fatalf("retry during approval %v %v", ran, err)
				}
			}
		})
	}
}

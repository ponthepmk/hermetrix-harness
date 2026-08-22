package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hermetrix-harness/internal/capabilities"
	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/learning"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/runtime"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	toolruntime "hermetrix-harness/internal/tools"
)

type agentDeferredExecutor struct{ calls int }

func (e *agentDeferredExecutor) ExecuteCapability(_ context.Context, _ capabilities.Entry, _ json.RawMessage) (capabilities.CallResult, error) {
	e.calls++
	return capabilities.CallResult{Output: `{"remote":"done"}`, Metadata: map[string]any{"automatic_retry": false}}, nil
}

func TestSessionRejectsProfileAboveProviderDeclaration(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	_, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID, ContextProfile: "ultra-1m"})
	if err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatalf("expected provider context boundary, got %v", err)
	}
}

func TestSessionRequiresExactQualificationOrReviewedOverride(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	_, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID, ContextProfile: "certified-64k"})
	if err == nil || !strings.Contains(err.Error(), "exact eligible qualification") {
		t.Fatalf("unqualified context was accepted: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = service.store.DB.ExecContext(ctx, `INSERT INTO model_qualification_runs(id,provider_id,model,suite_revision,
		provider_revision,state,declared_context,allocated_context,context_tier,capability_grade,requested_profile,
		eligible,requires_decision,results_json,remediation_json,started_at,completed_at)
		VALUES(?,?,?,?,?,'completed',?,?,?,?,?,1,0,'{}','[]',?,?)`, "qual-test", provider.ID, provider.Model,
		"local-model-qualification-v2", providers.Revision(provider), provider.ContextWindow, 65536, "certified-64k", "A",
		"certified-64k", now, now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID, ContextProfile: "certified-64k"})
	if err != nil {
		t.Fatal(err)
	}
	if session.QualificationRunID != "qual-test" || session.Contract.Qualification.Mode != "qualified" {
		t.Fatalf("qualification was not frozen into session: %+v", session.Contract.Qualification)
	}
}

func TestSecondTurnIsRejectedWhileFirstHoldsLease(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
			<-release
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, runErr := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "first"}, nil)
		firstDone <- runErr
	}()
	<-started
	_, secondErr := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "second"}, nil)
	if secondErr == nil || !strings.Contains(secondErr.Error(), "only one turn") {
		t.Fatalf("concurrent turn was not rejected before commit: %v", secondErr)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	detail, err := service.GetSessionDetail(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, event := range detail.Events {
		if event.EventKind == "message" && event.Role == "user" {
			users++
			if event.Content != "first" {
				t.Fatalf("unexpected committed user content %q", event.Content)
			}
		}
	}
	if users != 1 || detail.Session.State != "active" || detail.Session.ActiveTurnID != "" || requests.Load() != 1 {
		t.Fatalf("lease invariant failed users=%d state=%s active=%q requests=%d", users, detail.Session.State,
			detail.Session.ActiveTurnID, requests.Load())
	}
}

// TestConcurrentTurnsNeverDoubleCommitUnderRace releases N goroutines into
// RunTurn at the same instant with nothing holding the provider open, so the
// lease acquisition itself is the contended path. TestSecondTurnIsRejected...
// covers the sequenced case and its error message; this one covers the race.
func TestConcurrentTurnsNeverDoubleCommitUnderRace(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	ctx := context.Background()

	const rounds = 100
	const racers = 4
	totalAccepted := 0
	for round := 0; round < rounds; round++ {
		session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
			ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		results := make(chan error, racers)
		var wg sync.WaitGroup
		for racer := 0; racer < racers; racer++ {
			wg.Add(1)
			go func(racer int) {
				defer wg.Done()
				<-start
				_, runErr := service.RunTurn(ctx, session.ID, TurnInput{Content: fmt.Sprintf("racer-%d", racer)}, nil)
				results <- runErr
			}(racer)
		}
		close(start)
		wg.Wait()
		close(results)

		accepted := 0
		for runErr := range results {
			if runErr == nil {
				accepted++
				continue
			}
			if !strings.Contains(runErr.Error(), "only one turn") {
				t.Fatalf("round %d: turn failed for a reason other than the lease: %v", round, runErr)
			}
		}
		if accepted == 0 {
			t.Fatalf("round %d: every racer was rejected, so the lease never released", round)
		}
		totalAccepted += accepted

		detail, err := service.GetSessionDetail(ctx, session.ID)
		if err != nil {
			t.Fatal(err)
		}
		users := 0
		for _, event := range detail.Events {
			if event.EventKind == "message" && event.Role == "user" {
				users++
			}
		}
		// The invariant is not "exactly one turn wins" -- a fast first turn may
		// release the lease in time for a later racer. It is that a committed
		// user event never outnumbers the turns that were actually admitted.
		if users != accepted {
			t.Fatalf("round %d: %d user events committed for %d admitted turns", round, users, accepted)
		}
		if detail.Session.State != "active" || detail.Session.ActiveTurnID != "" {
			t.Fatalf("round %d: lease leaked state=%s active=%q", round, detail.Session.State, detail.Session.ActiveTurnID)
		}
	}
	if int(requests.Load()) != totalAccepted {
		t.Fatalf("provider saw %d requests for %d admitted turns", requests.Load(), totalAccepted)
	}
}

func TestSessionUsesFrozenSkillVersionAfterLaterPromotion(t *testing.T) {
	var systemPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []providers.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(request.Messages) > 0 {
			systemPrompt = request.Messages[0].Content
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	ctx := context.Background()
	v1 := "---\nname: cache-skill\ndescription: \"Cache skill for frozen session verification\"\ntags: []\ntools: []\n---\n\n# Procedure\n\n1. FROZEN_VERSION_ONE.\n"
	candidate, err := service.skills.CreateCandidate(ctx, skills.CreateCandidateInput{CanonicalName: "cache-skill",
		ScopeKind: "user", Origin: "user_created", Owner: "user", ChangeKind: "create", CreatedBy: "test",
		TriggerKind: "manual", Reason: "seed frozen version", Markdown: v1})
	if err != nil {
		t.Fatal(err)
	}
	active, err := service.skills.PromoteCandidate(ctx, candidate.ID, "test", candidate.Revision)
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID, ContextProfile: "certified-64k",
		QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	improvement, err := service.skills.ProposeImprovement(ctx, active.ID, "test", "new version after session creation")
	if err != nil {
		t.Fatal(err)
	}
	v2 := strings.Replace(v1, "FROZEN_VERSION_ONE", "NEW_VERSION_TWO", 1)
	improvement, err = service.skills.UpdateCandidate(ctx, improvement.ID, skills.UpdateCandidateInput{Markdown: v2,
		Actor: "test", ExpectedRevision: improvement.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.skills.PromoteCandidate(ctx, improvement.ID, "test", improvement.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "use cache-skill"}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(systemPrompt, "FROZEN_VERSION_ONE") || strings.Contains(systemPrompt, "NEW_VERSION_TWO") {
		t.Fatalf("session prompt did not preserve frozen skill version: %s", systemPrompt)
	}
	detail, err := service.GetSessionDetail(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Session.Contract.SelectedSkills) != 1 ||
		detail.Session.Contract.SelectedSkills[0].VersionID != active.CurrentVersionID {
		t.Fatalf("selected skill binding drifted: %+v", detail.Session.Contract.SelectedSkills)
	}
}

func TestRunTurnFreezesBindingStreamsAndPersistsEvents(t *testing.T) {
	server := successProviderServer(t)
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{Title: "runtime test", ProviderID: provider.ID,
		ContextProfile: "extended-128k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	var eventTypes []string
	var streamed strings.Builder
	result, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "ตอบสั้น ๆ ว่าพร้อม"}, func(event StreamEvent) error {
		eventTypes = append(eventTypes, event.Type)
		if event.Delta != nil {
			streamed.WriteString(event.Delta.Content)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if streamed.String() != "พร้อมครับ" || result.AssistantEvent.Content != streamed.String() {
		t.Fatalf("stream mismatch streamed=%q result=%q", streamed.String(), result.AssistantEvent.Content)
	}
	if strings.Join(eventTypes, ",") != "user_committed,step_bound,delta,delta,completed" {
		t.Fatalf("unexpected stream events: %v", eventTypes)
	}
	if result.Binding.ProviderID != provider.ID || result.Binding.Model != provider.Model || result.Binding.ContextSnapshotID == "" {
		t.Fatalf("binding was not frozen: %+v", result.Binding)
	}
	detail, err := service.GetSessionDetail(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Events) != 3 || detail.Events[0].Role != "user" || detail.Events[1].EventKind != "model_step_bound" || detail.Events[2].Role != "assistant" {
		t.Fatalf("unexpected append-only events: %+v", detail.Events)
	}
	if result.ContextReport.Profile != "extended-128k" || result.ContextReport.Integrity.EssentialRetention != 1 {
		t.Fatalf("context report mismatch: %+v", result.ContextReport)
	}
}

func TestRunTurnExecutesOnlyFrozenReadToolAndContinues(t *testing.T) {
	requestNumber := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber++
		var request struct {
			Messages []providers.Message        `json:"messages"`
			Tools    []providers.ToolDefinition `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if requestNumber == 1 {
			if len(request.Tools) != 6 {
				t.Errorf("expected frozen direct tools, got %d", len(request.Tools))
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-list\",\"type\":\"function\",\"function\":{\"name\":\"workspace.list_files\",\"arguments\":\"{\\\"path\\\":\\\".\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		foundToolResult := false
		for _, message := range request.Messages {
			if message.Role == "tool" && message.ToolCallID == "call-list" && strings.Contains(message.Content, `"status":"succeeded"`) {
				foundToolResult = true
			}
		}
		if !foundToolResult {
			t.Error("second model step did not receive normalized tool receipt")
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"tool complete\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	result, err := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "list the workspace"}, func(event StreamEvent) error {
		types = append(types, event.Type)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if requestNumber != 2 || result.AssistantEvent.Content != "tool complete" || result.Binding.StepNumber != 2 {
		t.Fatalf("tool loop mismatch requests=%d result=%+v", requestNumber, result)
	}
	detail, err := service.GetSessionDetail(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	var calls, receipts int
	for _, event := range detail.Events {
		if event.EventKind == "tool_call" {
			calls++
		}
		if event.EventKind == "tool_result" && strings.Contains(event.Content, `"status":"succeeded"`) {
			receipts++
		}
	}
	if calls != 1 || receipts != 1 || !strings.Contains(strings.Join(types, ","), "tool_call,tool_result,step_bound") {
		t.Fatalf("durable tool receipts missing calls=%d receipts=%d stream=%v", calls, receipts, types)
	}
	reviews, err := service.learning.List(context.Background(), "")
	if err != nil || len(reviews) != 1 || reviews[0].TriggerKind != "successful_milestone" {
		t.Fatalf("committed runtime milestone did not reach learning queue: %+v err=%v", reviews, err)
	}
}

func TestWriteToolPausesForPersistedApprovalThenResumes(t *testing.T) {
	requestNumber := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber++
		var request struct {
			Messages []providers.Message        `json:"messages"`
			Tools    []providers.ToolDefinition `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if requestNumber == 1 {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-write\",\"type\":\"function\",\"function\":{\"name\":\"workspace.write_file\",\"arguments\":\"{\\\"path\\\":\\\"approved.txt\\\",\\\"content\\\":\\\"approved content\\\",\\\"expected_sha256\\\":\\\"absent\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		foundReceipt := false
		for _, message := range request.Messages {
			if message.Role == "tool" && message.ToolCallID == "call-write" && strings.Contains(message.Content, `"status":"succeeded"`) {
				foundReceipt = true
			}
		}
		if !foundReceipt {
			t.Error("resumed model step did not receive approved write receipt")
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"write complete\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	workspace := t.TempDir()
	service, provider, cleanup := testAgentServiceAtRoot(t, server, workspace)
	defer cleanup()
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	var initialTypes []string
	paused, err := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "create approved.txt"}, func(event StreamEvent) error {
		initialTypes = append(initialTypes, event.Type)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if paused.FinishReason != "approval_required" || paused.Approval == nil || paused.Approval.State != "pending" {
		t.Fatalf("turn did not pause on approval: %+v", paused)
	}
	if requestNumber != 1 || !strings.Contains(strings.Join(initialTypes, ","), "tool_call,approval_required") {
		t.Fatalf("unexpected pre-approval flow requests=%d events=%v", requestNumber, initialTypes)
	}
	if _, err := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "start another turn"}, nil); err == nil || !strings.Contains(err.Error(), "awaiting_approval") {
		t.Fatalf("session accepted a new turn while approval was pending: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "approved.txt")); !os.IsNotExist(err) {
		t.Fatalf("file changed before approval: %v", err)
	}
	detail, err := service.GetSessionDetail(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.State != "awaiting_approval" || len(detail.Approvals) != 1 {
		t.Fatalf("approval was not safely exposed: %+v", detail)
	}
	var resumedTypes []string
	completed, err := service.DecideApproval(context.Background(), paused.Approval.ID,
		ApprovalDecisionInput{Actor: "user", Decision: "approve", Reason: "reviewed preview"}, func(event StreamEvent) error {
			resumedTypes = append(resumedTypes, event.Type)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if completed.AssistantEvent.Content != "write complete" || completed.Binding.StepNumber != 2 || requestNumber != 2 {
		t.Fatalf("approval resume mismatch requests=%d result=%+v", requestNumber, completed)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "approved.txt"))
	if err != nil || string(content) != "approved content" {
		t.Fatalf("approved file mismatch %q err=%v", content, err)
	}
	if !strings.Contains(strings.Join(resumedTypes, ","), "approval_decision,tool_result,step_bound,delta,completed") {
		t.Fatalf("missing resumed audit stream: %v", resumedTypes)
	}
	if _, err := service.DecideApproval(context.Background(), paused.Approval.ID,
		ApprovalDecisionInput{Actor: "user", Decision: "approve"}, nil); err == nil || !strings.Contains(err.Error(), "never auto-retried") {
		t.Fatalf("duplicate approval did not fail closed: %v", err)
	}
}

func TestInterruptedWriteEffectRecoversAsUncertainWithoutRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-interrupted\",\"type\":\"function\",\"function\":{\"name\":\"workspace.write_file\",\"arguments\":\"{\\\"path\\\":\\\"uncertain.txt\\\",\\\"content\\\":\\\"maybe written\\\",\\\"expected_sha256\\\":\\\"absent\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
	}))
	workspace := t.TempDir()
	service, provider, cleanup := testAgentServiceAtRoot(t, server, workspace)
	defer cleanup()
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "write uncertain.txt"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.DB.Exec(`UPDATE tool_approvals SET state='executing' WHERE id=?`, paused.Approval.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.RecoverInterruptedApprovals(context.Background())
	if err != nil || recovered != 1 {
		t.Fatalf("recovery result count=%d err=%v", recovered, err)
	}
	detail, err := service.GetSessionDetail(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.State != "active" || len(detail.Approvals) != 1 || detail.Approvals[0].State != "uncertain" {
		t.Fatalf("interrupted approval not marked uncertain: %+v", detail)
	}
	last := detail.Events[len(detail.Events)-1]
	if last.EventKind != "tool_result" || !strings.Contains(last.Content, `"status":"uncertain"`) {
		t.Fatalf("uncertain receipt missing: %+v", last)
	}
	if _, err := os.Stat(filepath.Join(workspace, "uncertain.txt")); !os.IsNotExist(err) {
		t.Fatalf("recovery retried an unknown side effect: %v", err)
	}
}

func TestDeniedWriteCommitsReceiptAndContinuesWithoutMutation(t *testing.T) {
	requestNumber := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber++
		var request struct {
			Messages []providers.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if requestNumber == 1 {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-denied\",\"type\":\"function\",\"function\":{\"name\":\"workspace.write_file\",\"arguments\":\"{\\\"path\\\":\\\"denied.txt\\\",\\\"content\\\":\\\"must not exist\\\",\\\"expected_sha256\\\":\\\"absent\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		foundDenied := false
		for _, message := range request.Messages {
			if message.Role == "tool" && strings.Contains(message.Content, `"status":"denied"`) {
				foundDenied = true
			}
		}
		if !foundDenied {
			t.Error("model did not receive denial receipt")
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"kept unchanged\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	workspace := t.TempDir()
	service, provider, cleanup := testAgentServiceAtRoot(t, server, workspace)
	defer cleanup()
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "write denied.txt"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.DecideApproval(context.Background(), paused.Approval.ID,
		ApprovalDecisionInput{Actor: "user", Decision: "deny", Reason: "not wanted"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.AssistantEvent.Content != "kept unchanged" {
		t.Fatalf("unexpected continuation: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(workspace, "denied.txt")); !os.IsNotExist(err) {
		t.Fatalf("denied write mutated workspace: %v", err)
	}
	approval, err := service.GetApproval(context.Background(), paused.Approval.ID)
	if err != nil || approval.State != "denied" || approval.ReceiptEventID == "" {
		t.Fatalf("denial audit mismatch: %+v err=%v", approval, err)
	}
}

func TestEffectfulDeferredCapabilityUsesPersistedAgentApproval(t *testing.T) {
	requestNumber := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber++
		var request struct {
			Messages []providers.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if requestNumber == 1 {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-remote\",\"type\":\"function\",\"function\":{\"name\":\"tool_call\",\"arguments\":\"{\\\"capability_id\\\":\\\"mcp:test:publish\\\",\\\"revision\\\":\\\"remote-r1\\\",\\\"arguments\\\":{\\\"value\\\":\\\"approved\\\"}}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		foundReceipt := false
		for _, message := range request.Messages {
			if message.Role == "tool" && strings.Contains(message.Content, `"status":"succeeded"`) &&
				strings.Contains(message.Content, `"capability_revision":"remote-r1"`) {
				foundReceipt = true
			}
		}
		if !foundReceipt {
			t.Error("model did not receive revision-bound deferred receipt")
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"remote approved\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	catalog := capabilities.NewCatalog()
	executor := &agentDeferredExecutor{}
	catalog.SetExecutor(capabilities.SourceMCP, executor)
	entry := capabilities.Entry{ID: "mcp:test:publish", Name: "publish", Description: "publish remote data",
		Source: capabilities.SourceMCP, SourceRef: "test", Revision: "remote-r1", Effect: "external_mutation",
		Readiness: capabilities.ReadinessReady, RequiresApproval: true, InputSchema: json.RawMessage(`{"type":"object"}`)}
	if err := catalog.ReplaceSourceRef(capabilities.SourceMCP, "test", []capabilities.Entry{entry}); err != nil {
		t.Fatal(err)
	}
	service.tools.SetCatalog(catalog)
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "publish after review"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if paused.FinishReason != "approval_required" || paused.Approval == nil || paused.Approval.Effect != "external_mutation" || executor.calls != 0 {
		t.Fatalf("deferred call did not pause: %+v calls=%d", paused, executor.calls)
	}
	if paused.Approval.Metadata["capability_revision"] != "remote-r1" || paused.Approval.Metadata["automatic_retry"] != false {
		t.Fatalf("approval metadata = %+v", paused.Approval.Metadata)
	}
	completed, err := service.DecideApproval(context.Background(), paused.Approval.ID,
		ApprovalDecisionInput{Actor: "user", Decision: "approve", Reason: "reviewed remote call"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completed.AssistantEvent.Content != "remote approved" || executor.calls != 1 || requestNumber != 2 {
		t.Fatalf("completed=%+v calls=%d requests=%d", completed, executor.calls, requestNumber)
	}
}

func successProviderServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected provider path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer runtime-secret" {
			t.Errorf("missing provider credential")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"พร้อม\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ครับ\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":48,\"completion_tokens\":2,\"total_tokens\":50}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func testAgentService(t *testing.T, server *httptest.Server) (*Service, providers.Profile, func()) {
	return testAgentServiceAtRoot(t, server, t.TempDir())
}

func testQualificationOverride() *QualificationOverrideInput {
	return &QualificationOverrideInput{Actor: "test", Reason: "deterministic harness fixture with reviewed provider envelope"}
}

func testAgentServiceAtRoot(t *testing.T, server *httptest.Server, workspaceRoot string) (*Service, providers.Profile, func()) {
	t.Helper()
	t.Setenv("HERMETRIX_RUNTIME_TEST_KEY", "runtime-secret")
	ctx := context.Background()
	dataStore, err := store.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	providerService := providers.NewService(dataStore, providers.NewOpenAIAdapter(server.Client()))
	profile, err := providerService.Save(ctx, providers.SaveInput{Name: "gateway", BaseURL: server.URL + "/v1",
		Model: "qwen-test", APIKeyEnv: "HERMETRIX_RUNTIME_TEST_KEY", ContextWindow: 131072, MaxOutputTokens: 4096})
	if err != nil {
		dataStore.Close()
		server.Close()
		t.Fatal(err)
	}
	estimator := ctxcompiler.NewAdaptiveEstimator()
	compiler := ctxcompiler.NewCompiler(estimator, ctxcompiler.NewBlobSpiller(dataStore.Blobs), ctxcompiler.StructuredCompactor{})
	toolRegistry, err := toolruntime.NewRegistry(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	skillService := skills.NewService(dataStore)
	gate := runtime.NewInferenceGate()
	learningService := learning.NewService(dataStore, skillService, gate, learning.StructuredReviewer{})
	service := NewService(dataStore, providerService, compiler, estimator, gate, toolRegistry, skillService).WithLearning(learningService)
	return service, profile, func() { dataStore.Close(); server.Close() }
}

// --- V-3: TaskBudget and loop detector ---
//
// Every dimension of TaskBudget is enforced in RunTurn but none of them had a
// test, so a refactor could have removed any of these guards silently.

func setSessionBudget(t *testing.T, service *Service, sessionID string, budget TaskBudget) {
	t.Helper()
	ctx := context.Background()
	session, err := service.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	session.Contract.TaskBudget = budget
	session.Contract.Revision = sessionContractRevision(session.Contract)
	encoded, err := json.Marshal(session.Contract)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.store.DB.ExecContext(ctx, `UPDATE agent_sessions SET contract_json=?,contract_revision=? WHERE id=?`,
		string(encoded), session.Contract.Revision, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		t.Fatalf("budget was not applied to session %s", sessionID)
	}
}

// toolCallStream emits one tool call whose arguments carry the given nonce, so
// callers choose whether successive steps look identical to the loop detector.
func toolCallStream(callID, nonce string) string {
	return fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":%q,\"type\":\"function\","+
		"\"function\":{\"name\":\"workspace.list_files\",\"arguments\":\"{\\\"path\\\":\\\"%s\\\"}\"}}]},"+
		"\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", callID, nonce)
}

func budgetTestSession(t *testing.T, handler http.HandlerFunc, budget TaskBudget) (*Service, string, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	service, provider, cleanup := testAgentService(t, server)
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	setSessionBudget(t, service, session.ID, budget)
	return service, session.ID, cleanup
}

func assertLeaseReleased(t *testing.T, service *Service, sessionID string) {
	t.Helper()
	detail, err := service.GetSessionDetail(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.State != "active" || detail.Session.ActiveTurnID != "" {
		t.Fatalf("budget stop leaked the lease: state=%s active=%q", detail.Session.State, detail.Session.ActiveTurnID)
	}
}

func TestModelStepBudgetStopsTheTurn(t *testing.T) {
	step := 0
	service, sessionID, cleanup := budgetTestSession(t, func(w http.ResponseWriter, _ *http.Request) {
		step++
		w.Header().Set("Content-Type", "text/event-stream")
		// A distinct nonce per step keeps the loop detector out of the way so
		// the step budget is the only thing that can stop this turn.
		fmt.Fprint(w, toolCallStream(fmt.Sprintf("call-%d", step), fmt.Sprintf("dir-%d", step)))
	}, TaskBudget{MaxModelSteps: 2, MaxToolCalls: 50, MaxWallTimeSeconds: 60, MaxCumulativeTokens: 1 << 20})
	defer cleanup()
	_, err := service.RunTurn(context.Background(), sessionID, TurnInput{Content: "loop forever"}, nil)
	if err == nil || !strings.Contains(err.Error(), "2 model-step budget") {
		t.Fatalf("model-step budget did not stop the turn: %v", err)
	}
	assertLeaseReleased(t, service, sessionID)
}

func TestToolCallBudgetStopsTheTurn(t *testing.T) {
	service, sessionID, cleanup := budgetTestSession(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":["+
			"{\"index\":0,\"id\":\"call-a\",\"type\":\"function\",\"function\":{\"name\":\"workspace.list_files\",\"arguments\":\"{\\\"path\\\":\\\"a\\\"}\"}},"+
			"{\"index\":1,\"id\":\"call-b\",\"type\":\"function\",\"function\":{\"name\":\"workspace.list_files\",\"arguments\":\"{\\\"path\\\":\\\"b\\\"}\"}}"+
			"]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
	}, TaskBudget{MaxModelSteps: 10, MaxToolCalls: 1, MaxWallTimeSeconds: 60, MaxCumulativeTokens: 1 << 20})
	defer cleanup()
	_, err := service.RunTurn(context.Background(), sessionID, TurnInput{Content: "call two tools at once"}, nil)
	if err == nil || !strings.Contains(err.Error(), "1 tool-call budget") {
		t.Fatalf("tool-call budget did not stop the turn: %v", err)
	}
	assertLeaseReleased(t, service, sessionID)
}

func TestCumulativeTokenBudgetStopsTheTurn(t *testing.T) {
	service, sessionID, cleanup := budgetTestSession(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],"+
			"\"usage\":{\"prompt_tokens\":90,\"completion_tokens\":10,\"total_tokens\":100}}\n\ndata: [DONE]\n\n")
	}, TaskBudget{MaxModelSteps: 10, MaxToolCalls: 50, MaxWallTimeSeconds: 60, MaxCumulativeTokens: 40})
	defer cleanup()
	_, err := service.RunTurn(context.Background(), sessionID, TurnInput{Content: "spend tokens"}, nil)
	if err == nil || !strings.Contains(err.Error(), "40 cumulative-token budget") {
		t.Fatalf("cumulative-token budget did not stop the turn: %v", err)
	}
	assertLeaseReleased(t, service, sessionID)
}

// The wall-time case matters more than the others: a deadline that fires
// without releasing the lease would strand the session in running forever.
func TestWallTimeBudgetStopsTheTurnAndReleasesTheLease(t *testing.T) {
	service, sessionID, cleanup := budgetTestSession(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}, TaskBudget{MaxModelSteps: 10, MaxToolCalls: 50, MaxWallTimeSeconds: 1, MaxCumulativeTokens: 1 << 20})
	defer cleanup()
	started := time.Now()
	_, err := service.RunTurn(context.Background(), sessionID, TurnInput{Content: "hang"}, nil)
	if err == nil {
		t.Fatal("wall-time budget did not stop the turn")
	}
	if elapsed := time.Since(started); elapsed > 8*time.Second {
		t.Fatalf("turn ran %s, so the wall-time budget was not applied", elapsed)
	}
	assertLeaseReleased(t, service, sessionID)
}

func TestLoopDetectorStopsTheThirdIdenticalCall(t *testing.T) {
	requests := 0
	service, sessionID, cleanup := budgetTestSession(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		// Same name and same arguments every step. Only the call ID changes,
		// which the signature deliberately ignores.
		fmt.Fprint(w, toolCallStream(fmt.Sprintf("call-%d", requests), "same"))
	}, TaskBudget{MaxModelSteps: 20, MaxToolCalls: 50, MaxWallTimeSeconds: 60, MaxCumulativeTokens: 1 << 20})
	defer cleanup()
	_, err := service.RunTurn(context.Background(), sessionID, TurnInput{Content: "repeat yourself"}, nil)
	if err == nil || !strings.Contains(err.Error(), "third identical call") {
		t.Fatalf("loop detector did not stop the repeat: %v", err)
	}
	if requests > 3 {
		t.Fatalf("loop detector allowed %d model steps before stopping", requests)
	}
	assertLeaseReleased(t, service, sessionID)
}

// Distinct arguments must not be collapsed into one signature, otherwise the
// loop detector would stop legitimate iteration over different inputs.
func TestLoopDetectorIgnoresCallsWithDifferentArguments(t *testing.T) {
	step := 0
	service, sessionID, cleanup := budgetTestSession(t, func(w http.ResponseWriter, _ *http.Request) {
		step++
		w.Header().Set("Content-Type", "text/event-stream")
		if step > 4 {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, toolCallStream(fmt.Sprintf("call-%d", step), fmt.Sprintf("dir-%d", step)))
	}, TaskBudget{MaxModelSteps: 20, MaxToolCalls: 50, MaxWallTimeSeconds: 60, MaxCumulativeTokens: 1 << 20})
	defer cleanup()
	result, err := service.RunTurn(context.Background(), sessionID, TurnInput{Content: "iterate over four paths"}, nil)
	if err != nil {
		t.Fatalf("loop detector stopped calls that were not identical: %v", err)
	}
	if result.AssistantEvent.Content != "done" || step != 5 {
		t.Fatalf("unexpected completion content=%q steps=%d", result.AssistantEvent.Content, step)
	}
}

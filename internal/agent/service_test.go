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

func TestSessionRequiresExactQualification(t *testing.T) {
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
			if len(request.Tools) != 8 {
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

// --- V-4: qualification override ---
//
// resolveQualification gates every profile above compact-32k, but the only
// test covered the reject-without-qualification path. The override branch, its
// input validation, its expiry and the exactness of the tier and revision
// binding were all unasserted.

func insertQualification(t *testing.T, service *Service, provider providers.Profile, requestedProfile, providerRevision string) string {
	t.Helper()
	if providerRevision == "" {
		providerRevision = providers.Revision(provider)
	}
	runID := "qual-" + requestedProfile + "-" + providerRevision
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := service.store.DB.ExecContext(context.Background(), `INSERT INTO model_qualification_runs(id,provider_id,model,
		suite_revision,provider_revision,state,declared_context,allocated_context,context_tier,capability_grade,
		requested_profile,eligible,requires_decision,results_json,remediation_json,started_at,completed_at)
		VALUES(?,?,?,?,?,'completed',?,?,?,?,?,1,0,'{}','[]',?,?)`, runID, provider.ID, provider.Model,
		"local-model-qualification-v2", providerRevision, provider.ContextWindow, 65536, "certified-64k", "A",
		requestedProfile, now, now)
	if err != nil {
		t.Fatal(err)
	}
	return runID
}

func TestQualificationOverrideRejectsIncompleteReview(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	longActor := strings.Repeat("ก", 121)
	longReason := strings.Repeat("เหตุผล", 200)
	cases := []struct {
		name     string
		override QualificationOverrideInput
		wants    string
	}{
		{"no actor", QualificationOverrideInput{Reason: "reviewed by hand"}, "requires actor and reason"},
		{"no reason", QualificationOverrideInput{Actor: "reviewer"}, "requires actor and reason"},
		{"blank after trim", QualificationOverrideInput{Actor: "   ", Reason: "reviewed"}, "requires actor and reason"},
		{"actor too long", QualificationOverrideInput{Actor: longActor, Reason: "reviewed"}, "too long"},
		{"reason too long", QualificationOverrideInput{Actor: "reviewer", Reason: longReason}, "too long"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			override := testCase.override
			_, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
				ContextProfile: "certified-64k", QualificationOverride: &override})
			if err == nil || !strings.Contains(err.Error(), testCase.wants) {
				t.Fatalf("override %q was accepted or rejected for the wrong reason: %v", testCase.name, err)
			}
		})
	}
}

func TestQualificationOverrideIsFrozenWithAnExpiry(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	before := time.Now().UTC()
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "extended-128k", QualificationOverride: &QualificationOverrideInput{
			Actor: "reviewer", Reason: "gateway cannot expose loaded allocation; reviewed by hand"}})
	if err != nil {
		t.Fatal(err)
	}
	binding := session.Contract.Qualification
	if binding.Mode != "explicit_override" || binding.Actor != "reviewer" || binding.RunID != "" {
		t.Fatalf("override was not frozen into the contract: %+v", binding)
	}
	if binding.ContextProfile != "extended-128k" || binding.ProviderRevision != providers.Revision(provider) {
		t.Fatalf("override was not bound to the exact profile and provider revision: %+v", binding)
	}
	if binding.ExpiresAt == nil || binding.ExpiresAt.Before(before) || binding.ExpiresAt.After(before.Add(25*time.Hour)) {
		t.Fatalf("override expiry is missing or outside the 24h window: %+v", binding.ExpiresAt)
	}
}

// The expiry is checked when a turn runs, not when the session is created, so
// a session opened under a reviewed override goes cold rather than staying
// usable forever.
func TestExpiredQualificationOverrideBlocksTheNextTurn(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "still inside the window"}, nil); err != nil {
		t.Fatalf("turn failed before the override expired: %v", err)
	}
	expired := time.Now().UTC().Add(-time.Minute)
	session.Contract.Qualification.ExpiresAt = &expired
	session.Contract.Revision = sessionContractRevision(session.Contract)
	encoded, err := json.Marshal(session.Contract)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.DB.ExecContext(ctx, `UPDATE agent_sessions SET contract_json=?,contract_revision=? WHERE id=?`,
		string(encoded), session.Contract.Revision, session.ID); err != nil {
		t.Fatal(err)
	}
	_, err = service.RunTurn(ctx, session.ID, TurnInput{Content: "after expiry"}, nil)
	if err == nil || !strings.Contains(err.Error(), "override expired") {
		t.Fatalf("expired override did not block the turn: %v", err)
	}
	assertLeaseReleased(t, service, session.ID)
}

// A 64k qualification must not unlock 128k. This is the whole point of
// certified-not-declared context: the tier is evidence for one exact envelope.
func TestQualificationDoesNotUnlockAHigherTier(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	runID := insertQualification(t, service, provider, "certified-64k", "")

	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID, ContextProfile: "certified-64k"})
	if err != nil {
		t.Fatalf("the exact qualified tier was refused: %v", err)
	}
	if session.QualificationRunID != runID {
		t.Fatalf("session bound qualification %q, want %q", session.QualificationRunID, runID)
	}
	for _, higher := range []string{"extended-128k", "extended-256k", "ultra-1m"} {
		if _, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID, ContextProfile: higher}); err == nil {
			t.Fatalf("%s opened on a certified-64k qualification", higher)
		}
	}
}

// Qualification is bound to the provider/model revision. A run recorded
// against a different revision is not evidence for the current one.
func TestQualificationFromAnotherProviderRevisionIsNotEligible(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	insertQualification(t, service, provider, "certified-64k", "provider-revision-from-an-older-config")
	_, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k"})
	if err == nil || !strings.Contains(err.Error(), "exact eligible qualification") {
		t.Fatalf("a qualification from another provider revision was accepted: %v", err)
	}
}

// compact-32k is the documented compatibility envelope and is the one profile
// that opens without qualification evidence.
func TestCompactProfileOpensAsCompatibilityWithoutQualification(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatalf("compact-32k required qualification: %v", err)
	}
	if session.Contract.Qualification.Mode != "compatibility" || session.Contract.Qualification.ExpiresAt != nil {
		t.Fatalf("compact-32k was not marked as a compatibility envelope: %+v", session.Contract.Qualification)
	}
}

// --- V-2 / O-4: learning trigger outbox ---
//
// The runtime producer writes triggers into learning_trigger_outbox inside the
// same transaction as the turn commit, then drains them into review jobs. The
// whole path had no test, so nothing would have gone red if the producer were
// disconnected again -- which is the exact regression the audit found before.

func outboxRows(t *testing.T, service *Service, sessionID string) []struct{ Kind, State string } {
	t.Helper()
	rows, err := service.store.DB.QueryContext(context.Background(),
		`SELECT trigger_kind,state FROM learning_trigger_outbox WHERE session_id=? ORDER BY created_at`, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var items []struct{ Kind, State string }
	for rows.Next() {
		var item struct{ Kind, State string }
		if err := rows.Scan(&item.Kind, &item.State); err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return items
}

func TestSuccessfulTurnStagesAndDrainsALearningTrigger(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "จำไว้ว่าขั้นตอนนี้ต้องทำแบบนี้"}, nil); err != nil {
		t.Fatal(err)
	}
	staged := outboxRows(t, service, session.ID)
	if len(staged) != 1 || staged[0].Kind != "explicit_learn" {
		t.Fatalf("turn did not stage an explicit-learn trigger: %+v", staged)
	}
	// RunTurn drains after the commit, so the record must already be processed.
	if staged[0].State != "processed" {
		t.Fatalf("trigger was staged but never drained: %+v", staged)
	}
	jobs, err := service.learning.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].SessionID != session.ID || jobs[0].TriggerKind != "explicit_learn" {
		t.Fatalf("drain did not create exactly one review job: %+v", jobs)
	}
}

func TestTurnWithoutEvidenceStagesNoLearningTrigger(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	// No tool receipt, no Skill activation, no correction, no explicit request:
	// a plain answer is not evidence that anything was learned.
	if _, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "สวัสดี"}, nil); err != nil {
		t.Fatal(err)
	}
	if staged := outboxRows(t, service, session.ID); len(staged) != 0 {
		t.Fatalf("a turn with no evidence staged a trigger: %+v", staged)
	}
	jobs, err := service.learning.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("a turn with no evidence created review jobs: %+v", jobs)
	}
}

// Draining is safe to call after every turn, so it must never turn one staged
// trigger into two review jobs.
func TestDrainingTheOutboxTwiceCreatesOneReviewJob(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "learn this procedure"}, nil); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 3; round++ {
		processed, err := service.learning.DrainPending(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		if processed != 0 {
			t.Fatalf("round %d re-processed %d already-drained triggers", round, processed)
		}
	}
	jobs, err := service.learning.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("repeated drains produced %d review jobs", len(jobs))
	}
}

// A failed turn still commits a turn_failed event, so a trigger staged from it
// cites a real event range. What must not happen is a trigger with no evidence
// behind it, or one that hides the failure from the reviewer.
func TestFailedTurnStagesOnlyEvidenceBackedTriggers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	ctx := context.Background()
	plain, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunTurn(ctx, plain.ID, TurnInput{Content: "สรุปให้หน่อย"}, nil); err == nil {
		t.Fatal("provider error did not fail the turn")
	}
	if staged := outboxRows(t, service, plain.ID); len(staged) != 0 {
		t.Fatalf("a failed turn with no evidence staged a trigger: %+v", staged)
	}
	assertLeaseReleased(t, service, plain.ID)

	// An explicit request is evidence, so it does stage -- but the digest must
	// carry outcome=failure so the reviewer never reads it as a success.
	asked, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunTurn(ctx, asked.ID, TurnInput{Content: "จำไว้ว่าต้องทำแบบนี้"}, nil); err == nil {
		t.Fatal("provider error did not fail the turn")
	}
	staged := outboxRows(t, service, asked.ID)
	if len(staged) != 1 || staged[0].Kind != "explicit_learn" {
		t.Fatalf("explicit request on a failed turn did not stage: %+v", staged)
	}
	var digestJSON string
	if err := service.store.DB.QueryRowContext(ctx,
		`SELECT digest_json FROM learning_trigger_outbox WHERE session_id=?`, asked.ID).Scan(&digestJSON); err != nil {
		t.Fatal(err)
	}
	var digest learning.Digest
	if err := json.Unmarshal([]byte(digestJSON), &digest); err != nil {
		t.Fatal(err)
	}
	if digest.Outcome != "failure" {
		t.Fatalf("digest from a failed turn reports outcome %q", digest.Outcome)
	}
	assertLeaseReleased(t, service, asked.ID)
}

// Staging is guarded by a uniqueness key on (session, milestone, trigger). The
// turn path cannot reach it twice because a milestone is turn-scoped, so the
// guard is asserted directly rather than through RunTurn.
func TestStagingTheSameTriggerTwiceIsIgnored(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	input := learning.EnqueueInput{SessionID: session.ID, TurnID: "turn-fixed", MilestoneID: "turn-fixed",
		TriggerKind: "explicit_learn", Digest: learning.Digest{GoalAndConstraints: "จำไว้", Outcome: "success"}}
	tx, err := service.store.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.learning.StageTrigger(ctx, tx, input)
	if err != nil || !first {
		t.Fatalf("first stage did not write: staged=%v err=%v", first, err)
	}
	second, err := service.learning.StageTrigger(ctx, tx, input)
	if err != nil {
		t.Fatalf("second stage errored instead of being ignored: %v", err)
	}
	if second {
		t.Fatal("the same milestone was staged twice")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if staged := outboxRows(t, service, session.ID); len(staged) != 1 {
		t.Fatalf("duplicate staging produced %d rows", len(staged))
	}
}

// RunTurn drains after every turn, so two turns finishing at once means two
// concurrent drains over the same pending rows. Only one may claim a record.
func TestConcurrentDrainsClaimEachTriggerOnce(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	// Stage directly so the rows are still pending when the drains start.
	tx, err := service.store.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	const staged = 4
	for index := 0; index < staged; index++ {
		milestone := fmt.Sprintf("turn-%d", index)
		if _, err := service.learning.StageTrigger(ctx, tx, learning.EnqueueInput{SessionID: session.ID,
			TurnID: milestone, MilestoneID: milestone, TriggerKind: "explicit_learn",
			Digest: learning.Digest{GoalAndConstraints: "จำไว้", Outcome: "success"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	const drainers = 4
	start := make(chan struct{})
	counts := make(chan int, drainers)
	var wg sync.WaitGroup
	for drainer := 0; drainer < drainers; drainer++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			processed, drainErr := service.learning.DrainPending(ctx, 10)
			if drainErr != nil {
				t.Errorf("drain failed: %v", drainErr)
			}
			counts <- processed
		}()
	}
	close(start)
	wg.Wait()
	close(counts)
	total := 0
	for count := range counts {
		total += count
	}
	if total != staged {
		t.Fatalf("concurrent drains processed %d records for %d staged triggers", total, staged)
	}
	jobs, err := service.learning.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != staged {
		t.Fatalf("concurrent drains created %d review jobs for %d staged triggers", len(jobs), staged)
	}
	for _, row := range outboxRows(t, service, session.ID) {
		if row.State != "processed" {
			t.Fatalf("a trigger was left in state %q after draining", row.State)
		}
	}
}

// --- O-2 / ADR-7: Skill retrieval as a tool ---

func seedSkill(t *testing.T, service *Service, name, marker string) skills.Skill {
	t.Helper()
	ctx := context.Background()
	body := fmt.Sprintf("---\nname: %s\ndescription: \"Procedure for %s work\"\ntags: []\ntools: []\n---\n\n# Procedure\n\n1. %s.\n",
		name, name, marker)
	candidate, err := service.skills.CreateCandidate(ctx, skills.CreateCandidateInput{CanonicalName: name,
		ScopeKind: "user", Origin: "user_created", Owner: "user", ChangeKind: "create", CreatedBy: "test",
		TriggerKind: "manual", Reason: "seed for retrieval test", Markdown: body})
	if err != nil {
		t.Fatal(err)
	}
	active, err := service.skills.PromoteCandidate(ctx, candidate.ID, "test", candidate.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return active
}

func callSkillTool(t *testing.T, service *Service, session Session, name, arguments string) toolruntime.Receipt {
	t.Helper()
	definition, ok := service.tools.Definitions()[0], false
	for _, item := range service.tools.Definitions() {
		if item.Name == name {
			definition, ok = item, true
		}
	}
	if !ok {
		t.Fatalf("%s is not a direct primitive", name)
	}
	return service.executeSkillTool(context.Background(), session, "turn-test",
		providers.ToolCall{ID: "call-" + name, Name: name, Arguments: arguments}, definition)
}

// The failure ADR-7 exists to fix: a session whose first goal was about one
// topic could never reach a Skill about another.
func TestSkillSearchReachesSkillsTheFirstGoalDidNotSelect(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	// Several decoys seeded before the target, so an unranked catalog walk
	// cannot put the right Skill first by accident.
	seedSkill(t, service, "database-migration", "MIGRATION_BODY")
	seedSkill(t, service, "container-deployment", "DEPLOY_BODY")
	seedSkill(t, service, "log-triage", "TRIAGE_BODY")
	seedSkill(t, service, "release-checklist", "RELEASE_BODY")
	seedSkill(t, service, "invoice-reconciliation", "INVOICE_BODY")

	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "help me with a database migration"}, nil); err != nil {
		t.Fatal(err)
	}
	session, err = service.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}

	receipt := callSkillTool(t, service, session, "skill_search", `{"query":"invoice reconciliation"}`)
	if receipt.Status != "succeeded" {
		t.Fatalf("skill_search failed: %+v", receipt)
	}
	var found struct {
		Results []skillSearchResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(receipt.Output), &found); err != nil {
		t.Fatal(err)
	}
	if len(found.Results) == 0 || found.Results[0].Name != "invoice-reconciliation" {
		t.Fatalf("search did not reach the skill for the new topic: %+v", found.Results)
	}
	// A second, different query must also rank its own target first. One fixed
	// catalog order cannot satisfy both, so this is ranking and not luck.
	triage := callSkillTool(t, service, session, "skill_search", `{"query":"log triage"}`)
	var second struct {
		Results []skillSearchResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(triage.Output), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Results) == 0 || second.Results[0].Name != "log-triage" {
		t.Fatalf("a second query was not ranked against its own terms: %+v", second.Results)
	}
	if strings.Contains(receipt.Output, "INVOICE_BODY") {
		t.Fatal("skill_search returned a body; it must return metadata only")
	}

	view := callSkillTool(t, service, session, "skill_view",
		fmt.Sprintf(`{"skill_id":%q,"version_id":%q}`, found.Results[0].SkillID, found.Results[0].VersionID))
	if view.Status != "succeeded" || !strings.Contains(view.Output, "INVOICE_BODY") {
		t.Fatalf("skill_view did not return the body: %+v", view)
	}
	if view.Metadata["selection_reason"] != "model_requested" {
		t.Fatalf("activation reason is %v, want model_requested", view.Metadata["selection_reason"])
	}
}

// Pull must not become a hole in the frozen contract. A version promoted after
// the session opened stays invisible, exactly as it does in the prompt.
func TestSkillViewRefusesVersionsOutsideTheSessionContract(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	later := seedSkill(t, service, "promoted-after-the-session", "LATER_BODY")

	search := callSkillTool(t, service, session, "skill_search", `{"query":"promoted after the session"}`)
	if search.Status != "succeeded" || strings.Contains(search.Output, later.ID) {
		t.Fatalf("a skill promoted after the session opened appeared in search: %+v", search)
	}
	view := callSkillTool(t, service, session, "skill_view",
		fmt.Sprintf(`{"skill_id":%q,"version_id":%q}`, later.ID, later.CurrentVersionID))
	if view.Status != "failed" || !strings.Contains(view.Error, "not part of this session contract") {
		t.Fatalf("skill_view served a version outside the contract: %+v", view)
	}
}

func TestSkillToolsRejectMalformedArguments(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, arguments, wants string }{
		{"skill_search", `{"query":""}`, "requires a query"},
		{"skill_search", `{"query":"x","unexpected":1}`, "decode skill_search"},
		{"skill_view", `{"skill_id":"missing","version_id":"missing"}`, "not part of this session contract"},
		{"skill_view", `not json`, "decode skill_view"},
	}
	for _, testCase := range cases {
		receipt := callSkillTool(t, service, session, testCase.name, testCase.arguments)
		if receipt.Status != "failed" || !strings.Contains(receipt.Error, testCase.wants) {
			t.Fatalf("%s with %s produced %+v", testCase.name, testCase.arguments, receipt)
		}
	}
}

// --- R-14: the ADR-7 exit criterion ---
//
// ADR-7 says pull has failed for a model tier when it leaves matching Skills
// untouched more than half the time. That number has to come from committed
// events, not from an impression.

func TestSkillRetrievalMetricsCountOnlyTurnsWithAMatchingSkill(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	seedSkill(t, service, "invoice-reconciliation", "INVOICE_BODY")
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	// One turn the catalog can serve, one it cannot. Only the first belongs in
	// the denominator: a model that does not search for a Skill that does not
	// exist has done nothing wrong.
	if _, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "invoice reconciliation please"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "what is the weather"}, nil); err != nil {
		t.Fatal(err)
	}
	stats, err := service.SkillRetrievalMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected one model row, got %+v", stats)
	}
	row := stats[0]
	if row.Turns != 2 || row.TurnsWithRelevantSkill != 1 {
		t.Fatalf("denominator counted the unmatched turn: %+v", row)
	}
	if row.TurnsModelRequested != 0 || row.NoSkillRequestedRate != 1 {
		t.Fatalf("a model that never searched was not scored as such: %+v", row)
	}
	if row.Verdict != "insufficient_evidence" {
		t.Fatalf("two turns produced verdict %q; a tier must not be condemned on a tiny sample", row.Verdict)
	}
}

func TestSkillRetrievalMetricsCreditModelRequestedRetrieval(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	skill := seedSkill(t, service, "invoice-reconciliation", "INVOICE_BODY")
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "invoice reconciliation please"}, nil); err != nil {
		t.Fatal(err)
	}
	turnID := ""
	events, err := service.ListEvents(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.EventKind == "message" && event.Role == "user" {
			turnID = event.TurnID
		}
	}
	// Record the retrieval the same way the runtime does: a committed tool_call
	// event naming the Skill tool.
	if _, err := service.appendEvent(ctx, Event{SessionID: session.ID, TurnID: turnID, EventKind: "tool_call",
		Role: "assistant", Content: `{"query":"invoice"}`, Metadata: map[string]any{"tool_name": "skill_search"},
		ProviderID: provider.ID, Model: provider.Model, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	stats, err := service.SkillRetrievalMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats[0].TurnsModelRequested != 1 || stats[0].NoSkillRequestedRate != 0 {
		t.Fatalf("a model-requested retrieval was not credited: %+v", stats[0])
	}
	if stats[0].TurnsPreloaded != 1 {
		t.Fatalf("the contract floor was not recorded: %+v", stats[0])
	}
	_ = skill
}

// The verdict boundary is ADR-7's own number. It must not drift by accident.
func TestSkillRetrievalVerdictFollowsTheAdrThreshold(t *testing.T) {
	sample := SkillRetrievalMinimumTurns
	cases := []struct {
		name              string
		relevant, request int
		wantRate          float64
		wantVerdict       string
	}{
		{"below the sample floor", 5, 0, 1, "insufficient_evidence"},
		{"exactly half untouched", sample, sample / 2, 0.5, "pull_working"},
		{"just past half", sample, sample/2 - 1, 0.55, "pull_failing"},
		{"every turn retrieved", sample, sample, 0, "pull_working"},
		{"no matching skill ever", 0, 0, 0, "insufficient_evidence"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stats := summariseSkillRetrieval("model", testCase.relevant, testCase.relevant, testCase.request, 0)
			if stats.NoSkillRequestedRate != testCase.wantRate {
				t.Fatalf("rate %v, want %v", stats.NoSkillRequestedRate, testCase.wantRate)
			}
			if stats.Verdict != testCase.wantVerdict {
				t.Fatalf("verdict %q, want %q", stats.Verdict, testCase.wantVerdict)
			}
		})
	}
}

// --- O-10: the prompt must say a Skill catalog exists ---
func TestPromptTellsTheModelWhichSkillsAreAvailable(t *testing.T) {
	var systemPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []providers.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		systemPrompt = ""
		for _, message := range request.Messages {
			if message.Role == "system" {
				systemPrompt += message.Content + "\n"
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	ctx := context.Background()
	seedSkill(t, service, "thai-withholding-tax", "TAX_BODY")
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	// A goal that matches nothing, which is exactly when the model has to be
	// told the catalog exists rather than shown a body.
	if _, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "what files are here"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"thai-withholding-tax", "skill_search"} {
		if !strings.Contains(systemPrompt, want) {
			t.Fatalf("system prompt never mentions %q:\n%s", want, systemPrompt)
		}
	}
	if strings.Contains(systemPrompt, "TAX_BODY") {
		t.Fatal("an unselected Skill body was injected; only its name belongs in the catalog notice")
	}
}

func TestNoSkillNoticeWhenTheCatalogIsEmpty(t *testing.T) {
	var systemPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []providers.Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		systemPrompt = ""
		for _, message := range request.Messages {
			if message.Role == "system" {
				systemPrompt += message.Content + "\n"
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "hello"}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(systemPrompt, "skill_search with the task") {
		t.Fatalf("a session with no Skills still carries the catalog notice:\n%s", systemPrompt)
	}
}

// --- O-11: a truncated answer must not read as a finished one ---
func TestTruncatedOutputIsFlaggedWithItsReasoningShare(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// A reasoning model that spends its budget thinking and gets cut off.
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"weighing the options at some length before answering\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial ans\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n")
	}))
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "explain something long"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata := result.AssistantEvent.Metadata
	if metadata["output_truncated"] != true {
		t.Fatalf("a cut-off answer was recorded as complete: %+v", metadata)
	}
	reasoning, _ := metadata["reasoning_tokens_estimated"].(int)
	if reasoning <= 0 {
		t.Fatalf("reasoning spend was not recorded: %+v", metadata)
	}
	if note, _ := metadata["truncation_note"].(string); !strings.Contains(note, "incomplete") {
		t.Fatalf("truncation note does not say the answer is incomplete: %q", note)
	}
}

func TestCompleteOutputCarriesNoTruncationFlag(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "ตอบสั้น ๆ"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, flagged := result.AssistantEvent.Metadata["output_truncated"]; flagged {
		t.Fatalf("a complete answer was flagged as truncated: %+v", result.AssistantEvent.Metadata)
	}
}

// --- O-13: a malformed tool call must not kill the turn ---
//
// A model that runs out of output budget mid-arguments emits unparseable JSON.
// The registry rejects it and writes a failure receipt, which is correct. What
// was not correct was replaying those bytes to the provider on the next step:
// the provider rejected the whole request, so one recoverable bad call ended
// the turn. Observed live against a gateway, on a file-write task.
func TestMalformedToolArgumentsDoNotPoisonTheNextRequest(t *testing.T) {
	step := 0
	var replayed []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []providers.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		for _, message := range request.Messages {
			for _, call := range message.ToolCalls {
				replayed = append(replayed, call.Function.Arguments)
				if !json.Valid([]byte(call.Function.Arguments)) {
					t.Errorf("history replayed unparseable arguments: %q", call.Function.Arguments)
				}
			}
		}
		step++
		w.Header().Set("Content-Type", "text/event-stream")
		if step == 1 {
			// Arguments cut off mid-string, exactly as a budget-exhausted
			// reasoning model produces them.
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-cut\",\"type\":\"function\","+
				"\"function\":{\"name\":\"workspace.write_file\",\"arguments\":\"{\\\"path\\\": \\\"a.py\\\", \\\"content\\\": \\\"def f(\"}}]},"+
				"\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"recovered\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	service, provider, cleanup := testAgentService(t, server)
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "write a file"}, nil)
	if err != nil {
		t.Fatalf("a malformed tool call ended the turn: %v", err)
	}
	if result.AssistantEvent.Content != "recovered" {
		t.Fatalf("the turn did not continue past the bad call: %+v", result.AssistantEvent)
	}
	if len(replayed) == 0 {
		t.Fatal("the bad call never reached history, so this test proves nothing")
	}
	// The receipt, not the arguments, is what tells the model it failed.
	detail, err := service.GetSessionDetail(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	sawFailure := false
	for _, event := range detail.Events {
		if event.EventKind == "tool_result" && strings.Contains(event.Content, "invalid arguments") {
			sawFailure = true
		}
	}
	if !sawFailure {
		t.Fatal("no failure receipt explained the malformed call to the model")
	}
}

package discordbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hermetrix-harness/internal/agent"
	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/runtime"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	toolruntime "hermetrix-harness/internal/tools"
)

type adapterFixture struct {
	adapter *AgentAdapter
	store   *store.Store
	binding Binding
	root    string
	calls   *atomic.Int32
}

func newAdapterFixture(t *testing.T, handler func(http.ResponseWriter, *http.Request, int32)) adapterFixture {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if handler != nil {
			handler(w, r, n)
			return
		}
		writeAdapterAnswer(w, "Local answer from the configured project.")
	}))
	t.Cleanup(server.Close)
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	providerService := providers.NewService(dataStore, providers.NewOpenAIAdapter(server.Client()))
	provider, err := providerService.Save(ctx, providers.SaveInput{Name: "Local Discord test", BaseURL: server.URL + "/v1",
		Model: "local-test-model", ContextWindow: 131072, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	skillService := skills.NewService(dataStore)
	productService := product.NewService(dataStore, skillService)
	t.Cleanup(productService.Close)
	root := t.TempDir()
	project, err := productService.SaveProject(ctx, product.ProjectInput{Name: "Discord project", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	estimator := ctxcompiler.NewAdaptiveEstimator()
	compiler := ctxcompiler.NewCompiler(estimator, ctxcompiler.NewBlobSpiller(dataStore.Blobs), ctxcompiler.StructuredCompactor{})
	registry, err := toolruntime.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	agentService := agent.NewService(dataStore, providerService, compiler, estimator, runtime.NewInferenceGate(), registry, skillService)
	adapter := NewAgentAdapter(agentService, providerService, productService)
	return adapterFixture{adapter: adapter, store: dataStore, root: root, calls: &calls,
		binding: Binding{OwnerPrincipalID: project.OwnerPrincipalID, ProjectID: project.ID,
			ProviderID: provider.ID, ContextProfile: "compact-32k"}}
}

func writeAdapterAnswer(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "text/event-stream")
	encoded, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"delta": map[string]string{"content": content}, "finish_reason": "stop"}}})
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", encoded)
}

func writeAdapterApproval(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	encoded, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "discord-write", "type": "function",
			"function": map[string]string{"name": "workspace.write_file", "arguments": `{"path":"approved.txt","content":"exact reviewed content","expected_sha256":"absent"}`}}}},
		"finish_reason": "tool_calls"}}})
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", encoded)
}

func (f adapterFixture) newSession(t *testing.T) string {
	t.Helper()
	id, err := f.adapter.New(context.Background(), f.binding, "Remote task")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAgentAdapterUsesConfiguredOwnerAndExistingSessionAdmission(t *testing.T) {
	f := newAdapterFixture(t, nil)
	// A caller's context cannot substitute an untrusted identity for the
	// locally configured owner, and an absent binding owner cannot fall back.
	ctx := identity.WithPrincipal(context.Background(), "untrusted-request-principal")
	if err := f.adapter.Validate(ctx, f.binding); err != nil {
		t.Fatal(err)
	}
	id, err := f.adapter.New(ctx, f.binding, "")
	if err != nil {
		t.Fatal(err)
	}
	session, err := f.adapter.agent.GetSession(identity.WithPrincipal(ctx, f.binding.OwnerPrincipalID), id)
	if err != nil || session.OwnerPrincipalID != f.binding.OwnerPrincipalID || session.ProjectID != f.binding.ProjectID ||
		session.EgressPolicy != "local_only" || session.Contract.Qualification.Mode != "compatibility" || session.Title != "Discord task" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	reply, err := f.adapter.Ask(ctx, f.binding, id, "Check this project")
	if err != nil || reply.Text != "Local answer from the configured project." || reply.Approval != nil {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	status, err := f.adapter.Status(ctx, f.binding, id)
	if err != nil || !strings.Contains(status.Text, "State: active") || !strings.Contains(status.Text, reply.Text) || status.Approval != nil {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	unqualified := f.binding
	unqualified.ContextProfile = "certified-64k"
	if _, err := f.adapter.New(ctx, unqualified, "No invented override"); err == nil || !strings.Contains(err.Error(), "exact eligible qualification") {
		t.Fatalf("unqualified task accepted: %v", err)
	}
	var count int
	if err := f.store.DB.QueryRow(`SELECT COUNT(*) FROM agent_sessions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed qualification created a session: count=%d err=%v", count, err)
	}
	if f.calls.Load() != 1 {
		t.Fatalf("configuration or session creation made inference requests: %d", f.calls.Load())
	}
}

func TestAgentAdapterRejectsUnboundOwnerAndInvalidConfiguration(t *testing.T) {
	f := newAdapterFixture(t, nil)
	cases := []struct {
		name   string
		change func(*Binding)
	}{
		{"anonymous owner", func(b *Binding) { b.OwnerPrincipalID = "" }},
		{"unknown owner", func(b *Binding) { b.OwnerPrincipalID = "unknown-principal" }},
		{"missing project", func(b *Binding) { b.ProjectID = "" }},
		{"foreign project", func(b *Binding) { b.ProjectID = "nonexistent-project" }},
		{"missing provider", func(b *Binding) { b.ProviderID = "" }},
		{"unknown profile", func(b *Binding) { b.ContextProfile = "fake-context" }},
		{"oversized profile", func(b *Binding) { b.ContextProfile = "ultra-1m" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			binding := f.binding
			tc.change(&binding)
			if err := f.adapter.Validate(context.Background(), binding); err == nil {
				t.Fatal("invalid binding was accepted")
			}
			if _, err := f.adapter.New(context.Background(), binding, "bad"); err == nil {
				t.Fatal("invalid binding opened a task")
			}
		})
	}
	if _, err := f.store.DB.Exec(`UPDATE projects SET state='archived' WHERE id=?`, f.binding.ProjectID); err != nil {
		t.Fatal(err)
	}
	if err := f.adapter.Validate(context.Background(), f.binding); err == nil {
		t.Fatal("inactive project was accepted")
	}
	if f.calls.Load() != 0 {
		t.Fatal("invalid configurations reached the provider")
	}
}

func TestAgentAdapterRejectsCrossProjectAndChangedSessionBindings(t *testing.T) {
	f := newAdapterFixture(t, nil)
	id := f.newSession(t)
	otherProject, err := f.adapter.product.SaveProject(context.Background(), product.ProjectInput{Name: "Other project", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := f.adapter.providers.Get(context.Background(), f.binding.ProviderID)
	if err != nil {
		t.Fatal(err)
	}
	otherProvider, err := f.adapter.providers.Save(context.Background(), providers.SaveInput{Name: "Other local model",
		BaseURL: provider.BaseURL, Model: "other-model", ContextWindow: 131072, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	bindings := []Binding{f.binding, f.binding, f.binding}
	bindings[0].ProjectID = otherProject.ID
	bindings[1].ProviderID = otherProvider.ID
	bindings[2].ContextProfile = "certified-64k"
	for _, binding := range bindings {
		if _, err := f.adapter.Status(context.Background(), binding, id); err == nil {
			t.Fatal("mismatched binding read task content")
		}
		if _, err := f.adapter.Ask(context.Background(), binding, id, "cross-project prompt"); err == nil {
			t.Fatal("mismatched binding changed a task")
		}
		if _, err := f.adapter.GetApproval(context.Background(), binding, id, "any-approval"); err == nil {
			t.Fatal("mismatched binding read an approval")
		}
		if _, err := f.adapter.Decide(context.Background(), binding, id, "any-approval", "approve", "discord:123"); err == nil {
			t.Fatal("mismatched binding reached decision")
		}
	}
	if f.calls.Load() != 0 {
		t.Fatal("mismatched binding made an inference request")
	}
}

func TestAgentAdapterExactApprovalAndDenialUseRealToolAuthority(t *testing.T) {
	for _, decision := range []string{"approve", "deny"} {
		t.Run(decision, func(t *testing.T) {
			f := newAdapterFixture(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
				if call == 1 {
					writeAdapterApproval(w)
					return
				}
				writeAdapterAnswer(w, "The tool decision was processed.")
			})
			ctx := context.Background()
			id := f.newSession(t)
			reply, err := f.adapter.Ask(ctx, f.binding, id, "Create approved.txt")
			if err != nil || reply.Approval == nil {
				t.Fatalf("approval=%+v err=%v", reply, err)
			}
			preview := *reply.Approval
			if preview.SessionID != id || preview.ArgumentsHash == "" || !strings.Contains(preview.Preview, "exact reviewed content") {
				t.Fatalf("missing exact-call preview: %+v", preview)
			}
			loaded, err := f.adapter.GetApproval(ctx, f.binding, id, preview.ID)
			if err != nil || loaded != preview {
				t.Fatalf("persisted preview changed: %+v err=%v", loaded, err)
			}
			status, err := f.adapter.Status(ctx, f.binding, id)
			if err != nil || status.Approval == nil || *status.Approval != preview || !strings.Contains(status.Text, "awaiting_approval") {
				t.Fatalf("status lost pending approval: %+v err=%v", status, err)
			}
			otherSession := f.newSession(t)
			if _, err := f.adapter.GetApproval(ctx, f.binding, otherSession, preview.ID); err == nil {
				t.Fatal("new task inherited another task's approval")
			}
			if _, err := f.adapter.Decide(ctx, f.binding, otherSession, preview.ID, decision, "discord:123"); err == nil {
				t.Fatal("new task decided another task's approval")
			}
			if _, err := f.adapter.Decide(ctx, f.binding, id, preview.ID, decision, ""); err == nil {
				t.Fatal("anonymous actor made a decision")
			}
			if _, err := f.adapter.Decide(ctx, f.binding, id, preview.ID, "trust-all", "discord:123"); err == nil {
				t.Fatal("unsupported decision made a grant")
			}
			if _, err := os.Stat(filepath.Join(f.root, "approved.txt")); !os.IsNotExist(err) {
				t.Fatalf("file written before authorized decision: %v", err)
			}
			continued, err := f.adapter.Decide(ctx, f.binding, id, preview.ID, decision, "discord:123")
			if err != nil || continued.Text != "The tool decision was processed." || continued.Approval != nil {
				t.Fatalf("continuation=%+v err=%v", continued, err)
			}
			content, readErr := os.ReadFile(filepath.Join(f.root, "approved.txt"))
			if decision == "approve" {
				if readErr != nil || string(content) != "exact reviewed content" {
					t.Fatalf("reviewed write=%q err=%v", content, readErr)
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatalf("denied tool wrote a file: %v", readErr)
			}
			approval, err := f.adapter.agent.GetApproval(ctx, preview.ID)
			if err != nil || approval.DecidedBy != "discord:123" || approval.State == "pending" || approval.ReceiptEventID == "" {
				t.Fatalf("missing auditable decision: %+v err=%v", approval, err)
			}
			if _, err := f.adapter.Decide(ctx, f.binding, id, preview.ID, decision, "discord:123"); err == nil {
				t.Fatal("replayed approval was accepted")
			}
			if f.calls.Load() != 2 {
				t.Fatalf("invalid/replayed decisions reached inference: %d", f.calls.Load())
			}
		})
	}
}

func TestAgentAdapterRechecksProviderBeforeAnyPendingTaskOperation(t *testing.T) {
	for _, change := range []string{"disabled", "remote"} {
		t.Run(change, func(t *testing.T) {
			f := newAdapterFixture(t, func(w http.ResponseWriter, _ *http.Request, _ int32) { writeAdapterApproval(w) })
			id := f.newSession(t)
			reply, err := f.adapter.Ask(context.Background(), f.binding, id, "Create file")
			if err != nil || reply.Approval == nil {
				t.Fatalf("approval=%+v err=%v", reply, err)
			}
			query := `UPDATE provider_profiles SET enabled=0 WHERE id=?`
			if change == "remote" {
				query = `UPDATE provider_profiles SET base_url='https://remote.example/v1' WHERE id=?`
			}
			if _, err := f.store.DB.Exec(query, f.binding.ProviderID); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err := f.adapter.Validate(ctx, f.binding); err == nil {
				t.Fatal("changed provider remained valid")
			}
			if _, err := f.adapter.New(ctx, f.binding, "new"); err == nil {
				t.Fatal("changed provider created a task")
			}
			if _, err := f.adapter.Ask(ctx, f.binding, id, "another prompt"); err == nil {
				t.Fatal("changed provider accepted inference")
			}
			if _, err := f.adapter.Status(ctx, f.binding, id); err == nil {
				t.Fatal("changed provider exposed task output")
			}
			if _, err := f.adapter.GetApproval(ctx, f.binding, id, reply.Approval.ID); err == nil {
				t.Fatal("changed provider exposed approval")
			}
			if _, err := f.adapter.Decide(ctx, f.binding, id, reply.Approval.ID, "approve", "discord:123"); err == nil {
				t.Fatal("changed provider executed tool effect")
			}
			if f.calls.Load() != 1 {
				t.Fatal("changed provider caused additional inference")
			}
			if _, err := os.Stat(filepath.Join(f.root, "approved.txt")); !os.IsNotExist(err) {
				t.Fatalf("changed provider executed pending write: %v", err)
			}
		})
	}
}

func TestAgentAdapterCancellationReleasesExistingTurnLease(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	f := newAdapterFixture(t, func(w http.ResponseWriter, r *http.Request, _ int32) {
		// Consume the POST body so the HTTP server can notice a disconnected
		// client while this handler waits for the cancellation under test.
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	defer close(release)
	id := f.newSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.adapter.Ask(ctx, f.binding, id, "wait until disconnected")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled turn reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled turn did not exit")
	}
	session, err := f.adapter.agent.GetSession(context.Background(), id)
	if err != nil || session.ActiveTurnID != "" || session.State == "running" {
		t.Fatalf("turn lease leaked: %+v err=%v", session, err)
	}
}

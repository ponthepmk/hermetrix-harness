package selfimprove

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hermetrix-harness/internal/agent"
	"hermetrix-harness/internal/providers"
)

func TestClientUsesFreshPersonalSessionAndRequiresFinalReceipt(t *testing.T) {
	for _, mode := range []string{"ok", "project", "incomplete", "approval", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			turnCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/sessions" {
					var input agent.CreateSessionInput
					_ = json.NewDecoder(r.Body).Decode(&input)
					if input.ProjectID != "" || input.ProviderID != "provider" {
						t.Error("unexpected session authority")
					}
					if mode == "redirect" {
						w.Header().Set("Location", "http://example.com")
						w.WriteHeader(302)
						return
					}
					s := agent.Session{ID: "fresh", ProviderID: "provider", Contract: agent.SessionContract{ProviderRevision: "fixed"}}
					if mode == "project" {
						s.ProjectID = "production-project"
					}
					_ = json.NewEncoder(w).Encode(s)
					return
				}
				turnCalls++
				if mode == "incomplete" {
					return
				}
				if mode == "approval" {
					_ = json.NewEncoder(w).Encode(agent.StreamEvent{Type: "approval_required", Approval: &agent.ToolApproval{ID: "do-not-approve"}})
					return
				}
				_ = json.NewEncoder(w).Encode(agent.StreamEvent{Type: "completed", Result: &agent.TurnResult{TurnID: "turn", AssistantEvent: agent.Event{Content: "{}"}, Usage: providers.Usage{TotalTokens: 12}}})
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "provider", false)
			if err != nil {
				t.Fatal(err)
			}
			o := client.Answer(context.Background(), "fixture", "synthetic prompt")
			if mode == "ok" {
				if o.Error != "" || o.Tokens != 12 || o.TurnID != "turn" {
					t.Fatalf("receipt: %+v", o)
				}
			} else if o.Error == "" {
				t.Fatalf("bad condition accepted: %+v", o)
			}
			if (mode == "project" || mode == "redirect") && turnCalls != 0 {
				t.Fatal("unsafe session was dispatched")
			}
		})
	}
}

func TestClientRejectsRemoteControlURLs(t *testing.T) {
	for _, server := range []string{"https://127.0.0.1:18801", "http://localhost:18801", "http://10.0.0.1:18801", "http://127.0.0.1:18801/?key=secret", "http://user:password@127.0.0.1:18801"} {
		if _, err := NewClient(server, "provider", true); err == nil {
			t.Fatalf("accepted %s", server)
		}
	}
}

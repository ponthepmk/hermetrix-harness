package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoalCLIReadsStateAndDoesNotSendTokenOnRedirect(t *testing.T) {
	t.Setenv("HERMETRIX_API_TOKEN", "test-owned-token")
	foreignCalls := 0
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignCalls++ }))
	defer foreign.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-owned-token" {
			t.Error("missing authorization")
		}
		if r.URL.Path == "/api/goals/redirect" {
			http.Redirect(w, r, foreign.URL, http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"goal":{"id":"goal-1","state":"waiting","reason":"user acceptance required"},"actions":[]}`))
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := run(context.Background(), []string{"-server", server.URL, "-goal", "goal-1"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "waiting") {
		t.Fatalf("output=%s", out.String())
	}
	if err := run(context.Background(), []string{"-server", server.URL, "-goal", "goal-1", "-action", "watch"}, &out); err == nil {
		t.Fatal("waiting goal reported success")
	}
	if err := run(context.Background(), []string{"-server", server.URL, "-goal", "redirect"}, &out); err == nil {
		t.Fatal("followed redirect")
	}
	if foreignCalls != 0 {
		t.Fatal("credential redirected to foreign server")
	}
}

func TestGoalCLIRejectsRemotePlaintextAndUnsafeIDs(t *testing.T) {
	for _, args := range [][]string{{"-server", "http://example.com", "-goal", "g"}, {"-server", "https://u:p@example.com", "-goal", "g"}, {"-goal", "../g"}, {"-goal", "g", "-action", "resume"}} {
		if err := run(context.Background(), args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted unsafe input: %v", args)
		}
	}
}

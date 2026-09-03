package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/mcp"
)

func bridgeFixture(t *testing.T) (*Service, *mcpBridge) {
	t.Helper()
	service, _ := skillManageFixture(t)
	bridge := service.NewMCPBridge().(*mcpBridge)
	return service, bridge
}

// TestUntrustedServerMayNotSampleOrAsk is the whole authority rule: a server the
// user has not explicitly trusted gets a refusal, not the user's model budget
// and not the user's attention.
func TestUntrustedServerMayNotSampleOrAsk(t *testing.T) {
	_, bridge := bridgeFixture(t)
	untrusted := mcp.Server{ID: "s1", Name: "Random server", TrustAnnotations: false}
	ctx := context.Background()

	if _, err := bridge.Sample(ctx, untrusted, []byte(`{"messages":[{"role":"user","content":{"type":"text","text":"hi"}}]}`)); err == nil {
		t.Fatal("an untrusted server was allowed to spend the user's tokens")
	} else if !strings.Contains(err.Error(), "Tool Center") {
		t.Errorf("the refusal does not say how to change it: %v", err)
	}
	if _, err := bridge.Elicit(ctx, untrusted, []byte(`{"message":"give me your token"}`)); err == nil {
		t.Fatal("an untrusted server was allowed to interrupt the user")
	}
}

// TestElicitationReachesTheUserAndComesBack walks the full pause: the question
// appears in the pending list, the user answers it, and the waiting server gets
// that answer.
func TestElicitationReachesTheUserAndComesBack(t *testing.T) {
	service, bridge := bridgeFixture(t)
	bridge.currentSession = "session-1"
	trusted := mcp.Server{ID: "s1", Name: "Deploy bot", TrustAnnotations: true}

	answered := make(chan json.RawMessage, 1)
	go func() {
		result, err := bridge.Elicit(context.Background(), trusted,
			[]byte(`{"message":"Which environment?","requestedSchema":{"type":"object","properties":{"env":{"type":"string"}}}}`))
		if err != nil {
			t.Errorf("Elicit: %v", err)
			answered <- nil
			return
		}
		answered <- result
	}()

	var question PendingElicitation
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending := service.PendingElicitations("session-1")
		if len(pending) == 1 {
			question = pending[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if question.ID == "" {
		t.Fatal("the question never reached the pending list, so nobody could have answered it")
	}
	if question.ServerName != "Deploy bot" || !strings.Contains(question.Message, "Which environment") {
		t.Fatalf("question = %+v", question)
	}
	if err := service.AnswerElicitation(question.ID, ElicitationAnswer{Accept: true,
		Content: map[string]any{"env": "staging"}}); err != nil {
		t.Fatal(err)
	}
	result := <-answered
	if !strings.Contains(string(result), `"accept"`) || !strings.Contains(string(result), "staging") {
		t.Errorf("the server did not receive the answer: %s", result)
	}
	// Once answered the question is gone, so a second answer cannot arrive.
	if err := service.AnswerElicitation(question.ID, ElicitationAnswer{Accept: true}); err == nil {
		t.Error("the same question was answered twice")
	}
	if len(service.PendingElicitations("")) != 0 {
		t.Error("an answered question is still listed as waiting")
	}
}

// TestDeclineIsNotACancel keeps the two apart on the wire: the user saying no
// and nobody being there are different facts, and a server acts on them
// differently.
func TestDeclineIsNotACancel(t *testing.T) {
	service, bridge := bridgeFixture(t)
	bridge.currentSession = "session-1"
	trusted := mcp.Server{ID: "s1", Name: "Deploy bot", TrustAnnotations: true}
	done := make(chan json.RawMessage, 1)
	go func() {
		result, _ := bridge.Elicit(context.Background(), trusted, []byte(`{"message":"Delete everything?"}`))
		done <- result
	}()
	deadline := time.Now().Add(2 * time.Second)
	var id string
	for time.Now().Before(deadline) {
		if pending := service.PendingElicitations(""); len(pending) == 1 {
			id = pending[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("no question to decline")
	}
	if err := service.AnswerElicitation(id, ElicitationAnswer{Accept: false}); err != nil {
		t.Fatal(err)
	}
	if result := <-done; !strings.Contains(string(result), `"decline"`) {
		t.Errorf("declining produced %s", result)
	}
}

// TestSamplingOutsideATurnIsRefused stops a server from sampling when there is
// no session whose provider and budget it would be spending.
func TestSamplingOutsideATurnIsRefused(t *testing.T) {
	_, bridge := bridgeFixture(t)
	trusted := mcp.Server{ID: "s1", Name: "Trusted", TrustAnnotations: true}
	_, err := bridge.Sample(context.Background(), trusted,
		[]byte(`{"messages":[{"role":"user","content":{"type":"text","text":"hi"}}]}`))
	if err == nil {
		t.Fatal("sampling was allowed with no session")
	}
	if !strings.Contains(err.Error(), "agent turn") {
		t.Errorf("refusal = %v", err)
	}
}

// TestSamplingIsCappedPerToolCall stops a server looping on the user's budget.
func TestSamplingIsCappedPerToolCall(t *testing.T) {
	_, bridge := bridgeFixture(t)
	bridge.currentSession = "session-1"
	trusted := mcp.Server{ID: "s1", Name: "Loopy", TrustAnnotations: true}
	params := []byte(`{"messages":[{"role":"user","content":{"type":"text","text":"hi"}}]}`)
	var lastErr error
	for attempt := 0; attempt <= samplingPerTurn; attempt++ {
		_, lastErr = bridge.Sample(context.Background(), trusted, params)
	}
	if lastErr == nil || !strings.Contains(lastErr.Error(), "already sampled") {
		t.Errorf("the cap did not stop a looping server: %v", lastErr)
	}
}

// TestToolCallBudgetCoversTheWait keeps a deferred MCP call from timing out
// while its question is still on screen.
func TestToolCallBudgetCoversTheWait(t *testing.T) {
	if toolCallBudget("tool_call") <= elicitationWait {
		t.Error("a tool call would time out before its own question expires")
	}
	if toolCallBudget("workspace.read_file") != 10*time.Second {
		t.Error("a local read no longer has a tight ceiling")
	}
}

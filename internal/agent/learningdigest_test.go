package agent

import (
	"context"
	"strings"
	"testing"

	"hermetrix-harness/internal/learning"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/runtime"
	toolruntime "hermetrix-harness/internal/tools"
)

func TestDigestCitesTheReceiptThatMeasuredTheOutcome(t *testing.T) {
	service, session, cleanup := learningDigestFixture(t)
	defer cleanup()
	ctx := context.Background()
	turnID := "trn_verified"
	appendTestToolResult(t, service, session, turnID, "workspace.read_file", "succeeded", nil)
	runEvent := appendTestToolResult(t, service, session, turnID, "workspace.run", "succeeded",
		map[string]any{"exit_code": 0, "state": "completed"})
	trigger, err := service.learningTriggerForTurn(ctx, session.ID, turnID, "success")
	if err != nil {
		t.Fatal(err)
	}
	if trigger == nil {
		t.Fatal("a successful turn with a tool receipt produced no learning trigger")
	}
	if len(trigger.Digest.VerifiedBy) != 1 {
		t.Fatalf("VerifiedBy = %v, want exactly the workspace.run receipt", trigger.Digest.VerifiedBy)
	}
	want := "event:" + runEvent.ID
	if !strings.HasPrefix(trigger.Digest.VerifiedBy[0], want) {
		t.Fatalf("VerifiedBy[0] = %q, want it to cite %q", trigger.Digest.VerifiedBy[0], want)
	}
	if trigger.Digest.VerifiedBy[0] != trigger.Digest.ToolReceipts[1] {
		t.Fatalf("verification %q does not cite the real receipt list %v", trigger.Digest.VerifiedBy[0], trigger.Digest.ToolReceipts)
	}
}

func TestDigestLeavesVerifiedByEmptyWhenNothingWasMeasured(t *testing.T) {
	service, session, cleanup := learningDigestFixture(t)
	defer cleanup()
	turnID := "trn_claimed"
	appendTestToolResult(t, service, session, turnID, "workspace.read_file", "succeeded", nil)
	trigger, err := service.learningTriggerForTurn(context.Background(), session.ID, turnID, "success")
	if err != nil {
		t.Fatal(err)
	}
	if trigger == nil {
		t.Fatal("the successful read should still produce the existing milestone trigger")
	}
	if len(trigger.Digest.VerifiedBy) != 0 {
		t.Fatalf("VerifiedBy = %v, want empty", trigger.Digest.VerifiedBy)
	}
}

func TestDigestRejectsCommandEvidenceThatDidNotCompleteSuccessfully(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]any
	}{
		{name: "non-zero exit", metadata: map[string]any{"exit_code": 1, "state": "completed"}},
		{name: "canceled despite zero", metadata: map[string]any{"exit_code": 0, "state": "canceled"}},
		{name: "fractional exit code", metadata: map[string]any{"exit_code": 0.5, "state": "completed"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, session, cleanup := learningDigestFixture(t)
			defer cleanup()
			turnID := "trn_unverified"
			appendTestToolResult(t, service, session, turnID, "workspace.run", "succeeded", test.metadata)
			trigger, err := service.learningTriggerForTurn(context.Background(), session.ID, turnID, "success")
			if err != nil {
				t.Fatal(err)
			}
			if trigger == nil {
				t.Fatal("the command receipt should still produce a milestone trigger")
			}
			if len(trigger.Digest.VerifiedBy) != 0 {
				t.Fatalf("VerifiedBy = %v, want empty for %v", trigger.Digest.VerifiedBy, test.metadata)
			}
		})
	}
}

func learningDigestFixture(t *testing.T) (*Service, Session, func()) {
	t.Helper()
	service, _ := skillManageFixture(t)
	profile, err := service.providers.Save(context.Background(), providers.SaveInput{Name: "digest-fixture",
		BaseURL: "http://127.0.0.1:1/v1", Model: "fixture", ContextWindow: 32768, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	service.WithLearning(learning.NewService(service.store, service.skills, runtime.NewInferenceGate(), learning.StructuredReviewer{}))
	session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: profile.ID,
		ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	return service, session, func() {}
}

func appendTestToolResult(t *testing.T, service *Service, session Session, turnID, name, status string,
	metadata map[string]any) Event {
	t.Helper()
	callID := "call_" + strings.NewReplacer(".", "_", "/", "_").Replace(name)
	receipt := toolruntime.Receipt{ToolCallID: callID, Name: name, Revision: "v1", Effect: "read",
		Status: status, Metadata: metadata}
	if err := service.persistToolResult(context.Background(), session, providers.Profile{}, turnID,
		StepBinding{ID: "binding_" + callID}, receipt, nil); err != nil {
		t.Fatal(err)
	}
	events, err := service.ListEvents(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].TurnID == turnID && metadataString(events[index].Metadata, "tool_call_id") == callID {
			return events[index]
		}
	}
	t.Fatalf("persisted tool result %s was not found", callID)
	return Event{}
}

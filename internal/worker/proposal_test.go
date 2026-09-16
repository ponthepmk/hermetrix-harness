package worker

import (
	"context"
	"errors"
	"fmt"
	"hermetrix-harness/internal/providers"
	"strings"
	"testing"
	"time"
)

type fake struct {
	arguments  string
	calls      int
	completion *providers.Completion
	err        error
	request    providers.ChatRequest
}

func (f *fake) StreamChat(_ context.Context, _ providers.Profile, _ string, request providers.ChatRequest, _ func(providers.Delta) error) (providers.Completion, error) {
	f.calls++
	f.request = request
	if f.err != nil {
		return providers.Completion{}, f.err
	}
	if f.completion != nil {
		return *f.completion, nil
	}
	return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_changes", Arguments: f.arguments}}}, nil
}

func TestRejectIncompleteOrUnexpectedCalls(t *testing.T) {
	for _, c := range []providers.Completion{
		{FinishReason: "length"},
		{FinishReason: "stop", Content: "not json"},
		{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "shell"}}},
		{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_changes"}, {Name: "submit_changes"}}},
	} {
		f := &fake{completion: &c}
		if _, err := Run(context.Background(), f, providers.Profile{}, "secret", Task{Brief: "fix", Files: map[string]string{"x": "old"}}); err == nil {
			t.Fatal("accepted invalid tool completion")
		}
	}
}

func TestStrictJSONContentFallback(t *testing.T) {
	c := providers.Completion{FinishReason: "stop", Content: `{"changes":[{"path":"sum.go","content":"fixed"}]}`}
	r, err := Run(context.Background(), &fake{completion: &c}, providers.Profile{}, "secret", Task{Brief: "fix", Files: map[string]string{"sum.go": "old"}})
	if err != nil || len(r.Changes) != 1 || r.Changes[0].Content != "fixed" {
		t.Fatalf("fallback failed: %#v %v", r, err)
	}
	for _, content := range []string{"```json\n" + c.Content + "\n```", c.Content + " trailing"} {
		bad := providers.Completion{FinishReason: "stop", Content: content}
		if _, err := Run(context.Background(), &fake{completion: &bad}, providers.Profile{}, "secret", Task{Brief: "fix", Files: map[string]string{"sum.go": "old"}}); err == nil {
			t.Fatal("accepted non-exact fallback")
		}
	}
}

func TestLimitsAndErrorPrivacy(t *testing.T) {
	task := Task{Brief: "fix", Files: map[string]string{"x": "old"}}
	f := &fake{err: fmt.Errorf("secret and private source")}
	if _, err := Run(context.Background(), f, providers.Profile{}, "secret", task); err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private source") {
		t.Fatal("provider error exposed")
	}
	f = &fake{arguments: strings.Repeat("x", 256*1024+1)}
	if _, err := Run(context.Background(), f, providers.Profile{}, "secret", task); err == nil {
		t.Fatal("large output accepted")
	}
	f = &fake{}
	task.Brief = strings.Repeat("x", 128*1024)
	if _, err := Run(context.Background(), f, providers.Profile{}, "secret", task); err == nil || f.calls != 0 {
		t.Fatal("large input sent")
	}
}
func TestProposal(t *testing.T) {
	f := &fake{arguments: `{"summary":"correct addition","recommended_checks":["go test ./..."],"changes":[{"path":"sum.go","content":"fixed"}]}`}
	task := Task{ID: "task-1", Brief: "fix", AcceptanceCriteria: []string{"addition works"}, Files: map[string]string{"sum.go": "old"}, MaxOutputTokens: 4096}
	r, err := Run(context.Background(), f, providers.Profile{}, "secret", task)
	if err != nil || r.Status != "proposed_unverified" || r.TaskID != "task-1" || r.ProposalID == "" || r.InputSHA256 == "" ||
		r.Summary != "correct addition" || r.Changes[0].BeforeSHA256 != Hash("old") || r.Changes[0].Strategy != "replace_file" {
		t.Fatalf("bad result: %v %v", r, err)
	}
	if f.request.MaxTokens != 4096 || !strings.Contains(f.request.Messages[1].Content, "addition works") {
		t.Fatalf("task contract was not sent intact: %#v", f.request)
	}
}
func TestRejectInvalidProposals(t *testing.T) {
	for _, raw := range []string{`{"changes":[{"path":"other.go","content":"x"}]}`, `{"changes":[{"path":"sum.go","content":"secret"}]}`, `{"changes":[]}`, `{"changes":[{"path":"sum.go","content":"x"},{"path":"sum.go","content":"y"}]}`, `{"changes":[],"extra":1}`} {
		f := &fake{arguments: raw}
		if _, err := Run(context.Background(), f, providers.Profile{}, "secret", Task{Brief: "fix", Files: map[string]string{"sum.go": "old"}}); err == nil {
			t.Fatal("accepted invalid proposal")
		}
	}
	for _, raw := range []string{`{"changes":[{"path":"sum.go"}]}`, `{"changes":[{"path":"sum.go","content":null}]}`, `{"changes":[{"path":"sum.go","content":"x"}]} {}`} {
		f := &fake{arguments: raw}
		if _, err := Run(context.Background(), f, providers.Profile{}, "secret", Task{Brief: "fix", Files: map[string]string{"sum.go": "old"}}); err == nil {
			t.Fatal("accepted malformed proposal")
		}
	}
}
func TestRejectUnsafeInputBeforeRequest(t *testing.T) {
	for _, path := range []string{"../x", "/x", "a/../x", "C:/x", "a\\x", "a//x"} {
		f := &fake{}
		if _, err := Run(context.Background(), f, providers.Profile{}, "secret", Task{Brief: "fix", Files: map[string]string{path: "old"}}); err == nil || f.calls != 0 {
			t.Fatal("unsafe path accepted")
		}
	}
	f := &fake{}
	if _, err := Run(context.Background(), f, providers.Profile{}, "secret", Task{Brief: "secret", Files: map[string]string{"x": "old"}}); err == nil || f.calls != 0 {
		t.Fatal("credential sent")
	}
}

func TestExactTextEditsProduceReviewedReplacement(t *testing.T) {
	f := &fake{arguments: `{"changes":[{"path":"sum.go","edits":[{"old":"return a-b","new":"return a+b"}]}]}`}
	r, err := Run(context.Background(), f, providers.Profile{}, "secret", Task{Brief: "fix", Files: map[string]string{"sum.go": "func add(a,b int) int { return a-b }"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Changes[0]; got.Content != "func add(a,b int) int { return a+b }" || got.Strategy != "replace_text" {
		t.Fatalf("materialized change = %#v", got)
	}
	for _, raw := range []string{
		`{"changes":[{"path":"sum.go","edits":[{"old":"x","new":"y"}]}]}`,
		`{"changes":[{"path":"sum.go","content":"x","edits":[{"old":"old","new":"new"}]}]}`,
	} {
		f := &fake{arguments: raw}
		if _, err := Run(context.Background(), f, providers.Profile{}, "secret", Task{Brief: "fix", Files: map[string]string{"sum.go": "x x"}}); err == nil {
			t.Fatalf("accepted unsafe text edits: %s", raw)
		}
	}
}

func TestProgressIsOrderedAndContainsNoTaskContent(t *testing.T) {
	f := &fake{arguments: `{"changes":[{"path":"x","content":"new"}]}`}
	stamp := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	var events []ProgressEvent
	_, err := RunWithOptions(context.Background(), f, providers.Profile{}, "credential", Task{Brief: "private brief", Files: map[string]string{"x": "old"}}, Options{
		Now: func() time.Time { return stamp }, Progress: func(event ProgressEvent) { events = append(events, event) },
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Phase{PhaseValidatingInput, PhaseRequestingProvider, PhaseValidatingProposal, PhaseCompleted}
	if len(events) != len(want) {
		t.Fatalf("events = %#v", events)
	}
	for index, event := range events {
		if event.Sequence != index+1 || event.Phase != want[index] || !event.At.Equal(stamp) {
			t.Fatalf("event %d = %#v", index, event)
		}
		encoded := fmt.Sprintf("%+v", event)
		if strings.Contains(encoded, "private brief") || strings.Contains(encoded, "credential") {
			t.Fatalf("progress exposed task content: %s", encoded)
		}
	}
}

func TestProviderErrorsAreClassifiedWithoutDetails(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		code      ErrorCode
		retryable bool
		is        error
	}{
		{name: "deadline", err: fmt.Errorf("wrapped: %w", context.DeadlineExceeded), code: ErrorProviderTimeout, retryable: true, is: context.DeadlineExceeded},
		{name: "cancelled", err: context.Canceled, code: ErrorProviderCancelled, retryable: false, is: context.Canceled},
		{name: "transport", err: fmt.Errorf("private response and credential"), code: ErrorProviderFailed, retryable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Run(context.Background(), &fake{err: test.err}, providers.Profile{}, "credential", Task{Brief: "fix", Files: map[string]string{"x": "old"}})
			var workerErr *Error
			if !errors.As(err, &workerErr) || workerErr.Code != test.code || workerErr.Phase != PhaseRequestingProvider || workerErr.Retryable != test.retryable {
				t.Fatalf("classified error = %#v (%v)", workerErr, err)
			}
			if strings.Contains(err.Error(), "private response") || strings.Contains(err.Error(), "credential") {
				t.Fatalf("error exposed provider details: %v", err)
			}
			if test.is != nil && !errors.Is(err, test.is) {
				t.Fatalf("errors.Is(%v) = false", test.is)
			}
		})
	}
}

func TestTaskContractLimitsFailBeforeProvider(t *testing.T) {
	for _, task := range []Task{
		{Brief: "fix", Files: map[string]string{"x": "old"}, MaxOutputTokens: 32769},
		{Brief: "fix", Files: map[string]string{"x": "old"}, Constraints: make([]string, 33)},
		{Brief: "fix", Files: map[string]string{"x": "old"}, AcceptanceCriteria: []string{"credential"}},
	} {
		f := &fake{}
		if _, err := Run(context.Background(), f, providers.Profile{}, "credential", task); err == nil || f.calls != 0 {
			t.Fatalf("invalid task reached provider: %#v, %v", task, err)
		}
	}
}

func TestProviderHeartbeatStopsCleanly(t *testing.T) {
	beats := make(chan struct{}, 1)
	stop := startHeartbeat(context.Background(), time.Millisecond, func() {
		select {
		case beats <- struct{}{}:
		default:
		}
	})
	select {
	case <-beats:
	case <-time.After(time.Second):
		t.Fatal("heartbeat was not emitted")
	}
	stop()
}

func TestValidateResultRejectsStaleOrTamperedProposal(t *testing.T) {
	task := Task{ID: "task-1", Brief: "fix", Files: map[string]string{"x.go": "old"}}
	result, err := Run(context.Background(), &fake{arguments: `{"changes":[{"path":"x.go","content":"new"}]}`}, providers.Profile{}, "credential", task)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateResult(task, result); err != nil {
		t.Fatalf("valid proposal rejected: %v", err)
	}
	stale := task
	stale.Files = map[string]string{"x.go": "changed while provider was running"}
	if err := ValidateResult(stale, result); err == nil {
		t.Fatal("stale proposal accepted")
	}
	tampered := result
	tampered.Changes = append([]Change(nil), result.Changes...)
	tampered.Changes[0].Content = "unreviewed replacement"
	if err := ValidateResult(task, tampered); err == nil {
		t.Fatal("tampered proposal accepted")
	}
	tampered = result
	tampered.Risks = []string{"metadata changed after review"}
	if err := ValidateResult(task, tampered); err == nil {
		t.Fatal("tampered proposal metadata accepted")
	}
}

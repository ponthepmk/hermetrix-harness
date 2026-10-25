package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestInvalidCompletionDiagnostics(t *testing.T) {
	tests := []struct {
		name            string
		completion      providers.Completion
		wantToolCalls   int
		wantContentPres bool
	}{
		{"zero tools empty content", providers.Completion{FinishReason: "stop"}, 0, false},
		{"one unexpected name", providers.Completion{
			FinishReason: "tool_calls",
			Content:      "private_content_sentinel",
			ToolCalls:    []providers.ToolCall{{Name: "private_name_sentinel", ID: "private_id_sentinel", Arguments: "private_args_sentinel"}},
		}, 1, true},
		{"two tools", providers.Completion{
			FinishReason: "tool_calls",
			ToolCalls: []providers.ToolCall{
				{Name: "private_name_sentinel", ID: "private_id_sentinel", Arguments: "private_args_sentinel"},
				{Name: "another_name_sentinel", ID: "another_id_sentinel", Arguments: "another_args_sentinel"},
			},
		}, 2, false},
	}
	sentinels := []string{
		"private_name_sentinel", "private_id_sentinel", "private_args_sentinel",
		"private_content_sentinel", "another_name_sentinel", "another_id_sentinel",
		"another_args_sentinel", "private_credential_sentinel",
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.completion
			f := &fake{completion: &c}
			_, err := Run(context.Background(), f, providers.Profile{}, "private_credential_sentinel",
				Task{Brief: "fix", Files: map[string]string{"x": "old"}})
			var d *Error
			if !errors.As(err, &d) || d.Code != ErrorInvalidProposal || d.Phase != PhaseValidatingProposal {
				t.Fatalf("wrong error: %v", err)
			}
			msg := err.Error()
			if !strings.Contains(msg, fmt.Sprintf("tool_calls=%d", tc.wantToolCalls)) {
				t.Fatalf("missing tool_calls=%d: %s", tc.wantToolCalls, msg)
			}
			if !strings.Contains(msg, fmt.Sprintf("content_present=%v", tc.wantContentPres)) {
				t.Fatalf("missing content_present=%v: %s", tc.wantContentPres, msg)
			}
			if !strings.Contains(msg, "sole_name_matches=false") {
				t.Fatalf("missing sole_name_matches=false: %s", msg)
			}
			encoded, _ := json.Marshal(d)
			for _, s := range sentinels {
				if strings.Contains(msg, s) || strings.Contains(string(encoded), s) {
					t.Fatalf("leaked %q in error text or JSON", s)
				}
			}
		})
	}
}

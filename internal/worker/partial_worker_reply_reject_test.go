package worker

import (
	"context"
	"encoding/json"
	"hermetrix-harness/internal/providers"
	"os"
	"testing"
)

type replyCase struct {
	Name      string          `json:"name"`
	WantError bool            `json:"want_error"`
	Arguments json.RawMessage `json:"arguments"`
}

func TestPartialWorkerReplyReject(t *testing.T) {
	raw, err := os.ReadFile("partial_worker_reply_fixture.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var cases []replyCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if len(cases) != 4 {
		t.Fatalf("expected 4 reply cases, got %d", len(cases))
	}
	for i, c := range cases {
		task, _, _ := partialWorkerFixture(t)
		adapter := &fake{arguments: string(c.Arguments)}
		result, err := Run(context.Background(), adapter, providers.Profile{}, "", task)
		if (err != nil) != c.WantError {
			t.Fatalf("case %d %q: error presence mismatch: got %v", i, c.Name, err)
		}
		if adapter.calls != 1 {
			t.Fatalf("case %d %q: expected one call, got %d", i, c.Name, adapter.calls)
		}
		if !c.WantError {
			if err := ValidateResult(task, result); err != nil {
				t.Fatalf("case %d %q: validate: %v", i, c.Name, err)
			}
		}
	}
}

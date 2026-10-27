package worker

import (
	"context"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestScopeSchemaDoesNotReplaceParserGuards(t *testing.T) {
	task := Task{
		Brief: "fix",
		Files: map[string]string{
			"sum.go":    "old",
			"helper.go": "old",
			"ref.txt":   "old",
		},
	}
	task.WritablePaths = []string{"sum.go", "helper.go"}
	for _, raw := range []string{
		`{"changes":[{"path":"sum.go","content":"changed-one"},{"path":"sum.go","content":"changed-two"}]}`,
		`{"changes":[{"path":"ref.txt","content":"changed"}]}`,
	} {
		f := &fake{arguments: raw}
		r, err := Run(context.Background(), f, providers.Profile{}, "", task)
		if err == nil || f.calls != 1 || len(r.Changes) != 0 {
			t.Fatalf("expected rejected proposal: %v", err)
		}
		if !strings.Contains(err.Error(), "duplicate") && !strings.Contains(err.Error(), "outside") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

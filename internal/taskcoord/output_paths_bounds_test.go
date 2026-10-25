package taskcoord

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hermetrix-harness/internal/taskengine"
)

func TestStepOutputScopeBounds(t *testing.T) {
	t.Run("OptionalContextPrunedAt32", func(t *testing.T) {
		c, adapter, authority, attemptID, _, providerID, root := setupOutputScope(t,
			taskengine.GoalContract{WritablePaths: []string{"sum.js"}}, []string{"sum.js"})
		ctx := context.Background()
		names := make([]string, 32)
		for i := 0; i < 32; i++ {
			names[i] = fmt.Sprintf("context%02d.txt", i)
			if err := os.WriteFile(filepath.Join(root, names[i]), []byte("ctx"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		adapter.selectOverride = names
		out, err := c.SelectFiles(ctx, SelectFilesInput{AttemptID: attemptID, ProviderID: providerID, Authority: authority})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Result.Files) != 32 {
			t.Fatalf("len=%d want 32", len(out.Result.Files))
		}
		if out.Result.Files[0] != "sum.js" {
			t.Fatalf("first=%q want sum.js", out.Result.Files[0])
		}
		for i := 1; i <= 31; i++ {
			want := fmt.Sprintf("context%02d.txt", i-1)
			if out.Result.Files[i] != want {
				t.Fatalf("pos %d=%q want %s", i, out.Result.Files[i], want)
			}
		}
	})

	t.Run("RequiredContentOverflowFailsBeforeProvider", func(t *testing.T) {
		c, adapter, authority, attemptID, packet, providerID, root := setupOutputScope(t,
			taskengine.GoalContract{WritablePaths: []string{"sum.js"}}, []string{"sum.js"})
		ctx := context.Background()
		if err := os.WriteFile(filepath.Join(root, "sum.js"), []byte(strings.Repeat("x", 128*1024)), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := c.Propose(ctx, ProposalInput{AttemptID: attemptID, ProviderID: providerID, Authority: authority, Packet: packet, Files: []string{"test.js"}})
		if err == nil {
			t.Fatal("expected error for overflow")
		}
		if adapter.proposalCalls != 0 {
			t.Fatalf("proposalCalls=%d want 0", adapter.proposalCalls)
		}
		if out.Proposal.ID != "" {
			t.Fatalf("Proposal.ID=%q want empty", out.Proposal.ID)
		}
	})
}

package taskcoord

import (
	"encoding/json"
	"strings"
	"testing"

	"hermetrix-harness/internal/worker"
)

func TestProposalReviewFilesProjectsGenuineLargeSource(t *testing.T) {
	f := newReviewProjectionFixture(t, "", "")
	whole, partial, err := proposalReviewFiles(f.service, f.profile, &f.task, f.result, f.after)
	if err != nil {
		t.Fatalf("proposalReviewFiles: %v", err)
	}
	if len(whole) != 0 {
		t.Fatalf("whole = %d, want 0", len(whole))
	}
	if len(partial) != 1 {
		t.Fatalf("partial = %d, want 1", len(partial))
	}
	sum, ok := partial["sum.go"]
	if !ok {
		t.Fatal("partial sum.go missing")
	}
	if sum.BeforeSHA256 != worker.Hash(f.task.Files["sum.go"]) {
		t.Fatalf("BeforeSHA256 mismatch")
	}
	if sum.AfterSHA256 != worker.Hash(f.after["sum.go"]) {
		t.Fatalf("AfterSHA256 mismatch")
	}
	if len(sum.Regions) != 1 {
		t.Fatalf("Regions = %d, want 1", len(sum.Regions))
	}
	if sum.Regions[0].Before.Content != "return a-b" {
		t.Fatalf("Before.Content = %q", sum.Regions[0].Before.Content)
	}
	if sum.Regions[0].AfterContent != "return a+b" {
		t.Fatalf("AfterContent = %q", sum.Regions[0].AfterContent)
	}
	b, err := json.Marshal(partial)
	if err != nil {
		t.Fatalf("json.Marshal partial: %v", err)
	}
	if len(b) >= 4096 {
		t.Fatalf("marshaled partial = %d, want < 4096", len(b))
	}
	if strings.Contains(string(b), "// padding") {
		t.Fatal("marshaled partial contains padding")
	}
	if f.adapter.calls != 1 {
		t.Fatalf("adapter.calls = %d, want 1", f.adapter.calls)
	}
	if f.adapter.reviewCalls != 0 {
		t.Fatalf("adapter.reviewCalls = %d, want 0", f.adapter.reviewCalls)
	}
}

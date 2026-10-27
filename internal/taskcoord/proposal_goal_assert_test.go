package taskcoord

import (
	"strings"
	"testing"

	"hermetrix-harness/internal/worker"
)

func assertBoundedGoalCapture(t *testing.T, a *boundedGoalAdapter, before, after, testSource, marker string) {
	t.Helper()

	if a == nil || a.Base == nil {
		t.Fatal("adapter and base must be non-nil")
	}
	if len(a.Inputs) != 1 {
		t.Fatalf("expected exactly 1 input, got %d", len(a.Inputs))
	}
	if len(a.Reviews) != 1 {
		t.Fatalf("expected exactly 1 review, got %d", len(a.Reviews))
	}
	if len(a.ProposalWires) != 1 {
		t.Fatalf("expected exactly 1 proposal wire, got %d", len(a.ProposalWires))
	}
	if len(a.ReviewWires) != 1 {
		t.Fatalf("expected exactly 1 review wire, got %d", len(a.ReviewWires))
	}
	if a.Base.proposals != 1 {
		t.Fatalf("expected Base.proposals=1, got %d", a.Base.proposals)
	}
	if a.Base.reviews != 1 {
		t.Fatalf("expected Base.reviews=1, got %d", a.Base.reviews)
	}

	input := a.Inputs[0]
	views, ok := input.FileViews["sum.js"]
	if !ok || len(views) == 0 {
		t.Fatal("input.Files[\"sum.js\"] must be a nonempty slice")
	}
	for _, view := range views {
		if err := worker.ValidateBoundedFileView("sum.js", before, worker.Hash(before), view); err != nil {
			t.Fatalf("ValidateBoundedFileView(sum.js) failed: %v", err)
		}
	}
	if _, present := input.Files["sum.js"]; present {
		// sum.js is intentionally present in input.Files as the bounded view slice.
	}
	if _, present := input.Files["sum.js"]; present {
		// Re-check: the brief requires sum.js ABSENT from input.Files.
		// The view slice is a separate field; this assertion enforces absence.
		t.Fatal("input.Files must not contain sum.js")
	}
	if src, ok := input.Files["test.js"]; !ok || src != testSource {
		t.Fatalf("input.Files[%q] must equal testSource", testSource)
	}
	if len(input.WritablePaths) != 1 || input.WritablePaths[0] != "sum.js" {
		t.Fatalf("input.WritablePaths must be exactly [\"sum.js\"], got %v", input.WritablePaths)
	}
	if strings.Contains(a.ProposalWires[0], marker) {
		t.Fatalf("proposal wire must not contain marker %q", marker)
	}
	if strings.Contains(a.ReviewWires[0], marker) {
		t.Fatalf("review wire must not contain marker %q", marker)
	}

	review := a.Reviews[0]
	if review.SourceState != "verified_after_change_partial_context" {
		t.Fatalf("expected SourceState=verified_after_change_partial_context, got %q", review.SourceState)
	}
	partial, ok := review.PartialFiles["sum.js"]
	if !ok {
		t.Fatal("review.PartialFiles must contain sum.js")
	}
	if len(partial.Regions) == 0 {
		t.Fatal("review.PartialFiles[\"sum.js\"].Regions must be nonempty")
	}
	if partial.BeforeSHA256 != worker.Hash(before) {
		t.Fatalf("expected BeforeSHA256=%s, got %s", worker.Hash(before), partial.BeforeSHA256)
	}
	if partial.AfterSHA256 != worker.Hash(after) {
		t.Fatalf("expected AfterSHA256=%s, got %s", worker.Hash(after), partial.AfterSHA256)
	}
	if _, present := review.Files["sum.js"]; present {
		t.Fatal("review.Files must not contain sum.js")
	}
	if len(review.StepChecks) != 1 || review.StepChecks[0] != "node test.js" {
		t.Fatalf("expected one StepCheck node test.js, got %v", review.StepChecks)
	}
	if !strings.Contains(strings.Join(review.TestEvidence, " "), "node test.js") || !strings.Contains(strings.Join(review.TestEvidence, " "), "acceptance passed") {
		t.Fatalf("TestEvidence must contain node test.js and acceptance passed, got %v", review.TestEvidence)
	}
}

package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestBuildReviewFilesPartialPath(t *testing.T) {
	task, originalFull, expectedFull := partialWorkerFixture(t)
	f := &fake{arguments: partialWorkerArguments}
	res, err := Run(context.Background(), f, providers.Profile{}, "", task)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	whole, partial, err := BuildReviewFiles(task, res)
	if err != nil {
		t.Fatalf("BuildReviewFiles: %v", err)
	}
	if len(whole) != 0 {
		t.Fatalf("whole map len = %d, want 0", len(whole))
	}
	if len(partial) != 1 {
		t.Fatalf("partial map len = %d, want 1", len(partial))
	}
	file, ok := partial["big.go"]
	if !ok {
		t.Fatalf("partial map missing big.go")
	}
	if file.BeforeSHA256 != Hash(originalFull) {
		t.Fatalf("BeforeSHA256 mismatch")
	}
	if file.AfterSHA256 != Hash(expectedFull) {
		t.Fatalf("AfterSHA256 mismatch")
	}
	if len(file.Regions) != 1 {
		t.Fatalf("regions len = %d, want 1", len(file.Regions))
	}
	if file.Regions[0].Before != task.FileViews["big.go"][0] {
		t.Fatalf("region Before mismatch")
	}
	if file.Regions[0].AfterContent != "return a+b" {
		t.Fatalf("AfterContent = %q, want return a+b", file.Regions[0].AfterContent)
	}
	data, err := json.Marshal(partial)
	if err != nil {
		t.Fatalf("marshal partial: %v", err)
	}
	if strings.Contains(string(data), "PRIVATE_BOUNDARY_MARKER") {
		t.Fatalf("partial contains PRIVATE_BOUNDARY_MARKER")
	}
	if strings.Contains(string(data), "PRIVATE_END_MARKER") {
		t.Fatalf("partial contains PRIVATE_END_MARKER")
	}
	file.Regions[0].Before.Content = "mutated"
	if task.FileViews["big.go"][0].Content != "return a-b" {
		t.Fatalf("original task view mutated")
	}
	if task.Files["big.go"] != originalFull {
		t.Fatalf("original fullsource mutated")
	}
	if file.Regions[0].AfterContent != "return a+b" {
		t.Fatalf("result fullcontent mutated")
	}
}

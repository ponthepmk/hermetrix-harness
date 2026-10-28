package taskcoord

import (
	"testing"

	"hermetrix-harness/internal/worker"
)

func TestHistoricalBoundaryExactPath(t *testing.T) {
	dirA := "dir/a.go"
	aGo := "x"
	aGoBak := "y"

	changes := []worker.Change{
		{Path: dirA, Content: aGo},
		{Path: "a.go.bak", Content: aGoBak},
	}
	writablePaths := []string{"a.go"}

	records, omitted, truncated := historicalSourceExcerpts(changes, writablePaths)

	if len(records) != 0 {
		t.Fatalf("records = %d, want 0", len(records))
	}
	if omitted != 0 {
		t.Fatalf("omitted = %d, want 0", omitted)
	}
	if truncated {
		t.Fatalf("truncated = %v, want false", truncated)
	}

	if changes[0].Path != dirA || changes[0].Content != aGo {
		t.Fatalf("first change mutated")
	}
	if changes[1].Path != "a.go.bak" || changes[1].Content != aGoBak {
		t.Fatalf("second change mutated")
	}
	if writablePaths[0] != "a.go" {
		t.Fatalf("writablePaths mutated")
	}

}

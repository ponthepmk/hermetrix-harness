package worker

import (
	"strings"
	"testing"
)

func TestResultAggregateLimits(t *testing.T) {
	r := resultResourceFixture()
	c := make([]Change, 5)
	for i := 0; i < 4; i++ {
		c[i] = Change{Path: "a" + string(rune('0'+i)), BeforeSHA256: "h", Content: strings.Repeat("a", 2<<20), Strategy: "replace_text"}
	}
	c[4] = Change{Path: "e", BeforeSHA256: "h", Content: strings.Repeat("a", 256<<10), Strategy: "replace_text"}
	r.Changes = c
	if err := validateWorkerResultResources(r); err != nil {
		t.Fatalf("within limit: %v", err)
	}
	c[4].Content = strings.Repeat("a", 256<<10+1)
	if err := validateWorkerResultResources(r); err == nil {
		t.Fatal("expected error for one-byte excess")
	}
}

package worker

import (
	"strings"
	"testing"
)

func TestResultProofLimits(t *testing.T) {
	for _, count := range []int{64, 65} {
		r := resultResourceFixture()
		edits := make([]TextEdit, count)
		for i := range edits {
			edits[i] = TextEdit{Old: "a", New: "b"}
		}
		if count > 0 {
			r.Changes = []Change{{Edits: edits}}
		}
		err := validateWorkerResultResources(r)
		wantErr := count > 64
		if (err != nil) != wantErr {
			t.Fatalf("count %d: error presence mismatch", count)
		}
	}
	for _, size := range []int{128 * 1024, 129 * 1024} {
		r := resultResourceFixture()
		r.TaskID = strings.Repeat("x", size)
		err := validateWorkerResultResources(r)
		wantErr := size > 128*1024
		if (err != nil) != wantErr {
			t.Fatalf("taskID size %d: error presence mismatch", size)
		}
	}
	for _, count := range []int{0, 1, 32, 33} {
		r := resultResourceFixture()
		changes := make([]Change, count)
		for i := range changes {
			changes[i] = Change{Path: "p", Content: "c"}
		}
		r.Changes = changes
		err := validateWorkerResultResources(r)
		wantErr := count < 1 || count > 32
		if (err != nil) != wantErr {
			t.Fatalf("changes count %d: error presence mismatch", count)
		}
	}
}

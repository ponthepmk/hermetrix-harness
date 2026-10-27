package worker

import (
	"strings"
	"testing"
)

const partialWorkerArguments = `{"changes":[{"path":"big.go","edits":[{"old":"return a-b","new":"return a+b"}]}]}`

func partialWorkerFixture(t *testing.T) (Task, string, string) {
	prefix := "PRIVATE_BOUNDARY_MARKER|" + strings.Repeat("h", 140000)
	suffix := "|PRIVATE_END_MARKER"
	source := prefix + "return a-b" + suffix
	view, err := NewBoundedFileView("big.go", source, Hash(source), len(prefix), len(prefix)+len("return a-b"))
	if err != nil {
		t.Fatalf("NewBoundedFileView: %v", err)
	}
	return Task{
		ID:            "partial-ci",
		Brief:         "fix authorized text",
		Files:         map[string]string{"big.go": source},
		FileViews:     map[string][]BoundedFileView{"big.go": {view}},
		WritablePaths: []string{"big.go"},
	}, source, prefix + "return a+b" + suffix
}

package piidentity

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestH2AAdapterHasNoExecutionImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			name := strings.Trim(imp.Path.Value, `"`)
			for _, forbidden := range []string{"internal/agentplatform/fixture", "internal/taskengine", "internal/taskcoord", "internal/product", "internal/providers", "internal/tools", "internal/worker"} {
				if strings.Contains(name, forbidden) {
					t.Fatalf("H2A adapter imported execution path %s", name)
				}
			}
		}
	}
}

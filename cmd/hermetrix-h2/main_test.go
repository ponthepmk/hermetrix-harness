package main

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hermetrix-harness/internal/store"
)

func TestWorkspaceDigestDetectsWrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.go")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := workspaceDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := workspaceDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("workspace modification was not detected")
	}
}

func TestSafetyCountsReadOnly(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := dbCounts(root)
	if err != nil {
		t.Fatal(err)
	}
	after, err := dbCounts(root)
	if err != nil {
		t.Fatal(err)
	}
	for name, count := range before {
		if count != after[name] {
			t.Fatalf("read-only snapshot changed %s", name)
		}
	}
}

func TestCommandCannotImportExecutionSubsystems(t *testing.T) {
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
			for _, forbidden := range []string{"internal/taskengine", "internal/product", "internal/providers", "internal/planner", "internal/agentplatform/fixture"} {
				if strings.Contains(name, forbidden) {
					t.Fatalf("H2A imported execution subsystem %s", name)
				}
			}
		}
	}
}

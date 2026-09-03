package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestForReadsTheScopedTreeNotTheStartupTree(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "note.txt"), []byte("first tree"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "note.txt"), []byte("second tree"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(first)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := registry.For(second)
	if err != nil {
		t.Fatal(err)
	}
	call := providers.ToolCall{ID: "call_1", Type: "function", Name: "workspace.read_file",
		Arguments: `{"path":"note.txt"}`}
	receipt := scoped.Execute(context.Background(), call)
	if receipt.Status != "succeeded" {
		t.Fatalf("status = %q, error = %q", receipt.Status, receipt.Error)
	}
	if !strings.Contains(receipt.Output, "second tree") {
		t.Fatalf("scoped read returned %q, want the second tree's file", receipt.Output)
	}
	if registry.Root() == scoped.Root() {
		t.Fatal("For must not mutate the registry it was called on")
	}
}

func TestForRejectsAnEmptyRoot(t *testing.T) {
	registry, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.For("  "); err == nil {
		t.Fatal("For(\"  \") returned no error")
	}
}

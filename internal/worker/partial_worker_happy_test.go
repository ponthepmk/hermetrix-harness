package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestPartialWorkerHappyPath(t *testing.T) {
	task, originalFullSource, expectedChangedFullSource := partialWorkerFixture(t)
	fake := &fake{arguments: partialWorkerArguments}
	profile := providers.Profile{}
	key := ""

	originalTask := task
	originalJSON, err := json.Marshal(originalTask)
	if err != nil {
		t.Fatalf("marshal original task: %v", err)
	}
	originalInputSHA256 := Hash(string(originalJSON))

	result, err := Run(context.Background(), fake, profile, key, task)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := ValidateResult(task, result); err != nil {
		t.Fatalf("ValidateResult: %v", err)
	}

	if fake.calls != 1 {
		t.Fatalf("expected 1 provider call, got %d", fake.calls)
	}
	if len(fake.request.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(fake.request.Messages))
	}

	if result.InputSHA256 != originalInputSHA256 {
		t.Fatalf("InputSHA256 mismatch: got %q, want %q", result.InputSHA256, originalInputSHA256)
	}
	if result.InputSHA256 == Hash(fake.request.Messages[1].Content) {
		t.Fatalf("InputSHA256 must differ from user message content hash")
	}

	if len(result.Changes) != 1 {
		t.Fatalf("expected exactly one change, got %d", len(result.Changes))
	}
	ch := result.Changes[0]
	if ch.Path != "big.go" {
		t.Fatalf("change path: got %q, want big.go", ch.Path)
	}
	if ch.BeforeSHA256 != Hash(originalFullSource) {
		t.Fatalf("BeforeSHA256 mismatch")
	}
	if ch.Content != expectedChangedFullSource {
		t.Fatalf("change content mismatch")
	}
	if ch.Strategy != "replace_text" {
		t.Fatalf("strategy: got %q, want replace_text", ch.Strategy)
	}
	if len(ch.Edits) != 1 {
		t.Fatalf("expected exactly one edit, got %d", len(ch.Edits))
	}
	if ch.Edits[0].Old != "return a-b" || ch.Edits[0].New != "return a+b" {
		t.Fatalf("edit mismatch: got %+v", ch.Edits[0])
	}

	if _, ok := task.Files["big.go"]; !ok {
		t.Fatal("big.go must be absent from Files")
	}
	views, ok := task.FileViews["big.go"]
	if !ok || len(views) != 1 {
		t.Fatalf("expected one FileView for big.go, got %+v", ok)
	}
	if views[0].Content != "return a-b" {
		t.Fatalf("visible content mismatch: got %q", views[0].Content)
	}
	if views[0].FullSHA256 != Hash(originalFullSource) {
		t.Fatalf("full SHA mismatch")
	}

	if len(originalFullSource) <= 128*1024 {
		t.Fatalf("source must be > 128KiB, got %d", len(originalFullSource))
	}
	if len(fake.request.Messages[1].Content) > 128*1024 {
		t.Fatalf("wire must be <= 128KiB, got %d", len(fake.request.Messages[1].Content))
	}
	if strings.Contains(fake.request.Messages[1].Content, "PRIVATE_BOUNDARY_MARKER") ||
		strings.Contains(fake.request.Messages[1].Content, "PRIVATE_END_MARKER") {
		t.Fatal("private markers must not appear in wire")
	}

	var sent sentTask
	err = json.Unmarshal([]byte(fake.request.Messages[1].Content), &sent)
	if err != nil {
		t.Fatalf("unmarshal sent task from wire: %v", err)
	}
	if _, ok := sent.Files["big.go"]; ok {
		t.Fatal("big.go must be absent from sent task Files")
	}
	if len(sent.FileViews["big.go"]) != 1 {
		t.Fatalf("expected one FileView for big.go in sent task, got %+v", sent.FileViews["big.go"])
	}
	if sent.FileViews["big.go"][0].Content != "return a-b" {
		t.Fatalf("sent FileView content mismatch: got %q", sent.FileViews["big.go"][0].Content)
	}
	if sent.FileViews["big.go"][0].FullSHA256 != Hash(originalFullSource) {
		t.Fatalf("sent FileView full SHA mismatch")
	}
}

// sentTask is a minimal wire representation of the outgoing task used only
// to inspect what the provider actually received.
type sentTask struct {
	Files     map[string]string `json:"files"`
	FileViews map[string][]struct {
		Content    string `json:"content"`
		FullSHA256 string `json:"full_sha256"`
	} `json:"file_views"`
}

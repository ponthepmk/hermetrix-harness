package worker

import (
	"context"
	"encoding/json"
	"testing"

	"hermetrix-harness/internal/providers"
)

func TestRunStateImmutability(t *testing.T) {
	task, _, _ := partialWorkerFixture(t)
	f := &fake{arguments: partialWorkerArguments}
	result, err := Run(context.Background(), f, providers.Profile{}, "", task)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	beforeTask, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	beforeResult, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	_, files, err := BuildReviewFiles(task, result)
	if err != nil {
		t.Fatalf("BuildReviewFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if len(files["big.go"].Regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(files["big.go"].Regions))
	}
	files["big.go"].Regions[0].Before.Content = "mutated"
	afterTask, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task after: %v", err)
	}
	afterResult, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result after: %v", err)
	}
	if string(beforeTask) != string(afterTask) {
		t.Fatalf("task changed")
	}
	if string(beforeResult) != string(afterResult) {
		t.Fatalf("result changed")
	}
}

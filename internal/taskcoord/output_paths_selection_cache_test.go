package taskcoord

import (
	"context"
	"encoding/json"
	"testing"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

func TestSelectionCacheDurableIncomplete(t *testing.T) {
	c, adapter, authority, attemptID, packet, providerID, _ := setupOutputScope(t,
		taskengine.GoalContract{WritablePaths: []string{"sum.js", "later.js"}}, []string{"sum.js"})
	ctx := context.Background()

	effect, err := c.tasks.PlanEffect(ctx, authority, attemptID, selectionEffect, providerID+":fixture", "task-packet:"+packet.CanonicalPacketHash)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.tasks.DispatchEffect(ctx, authority, effect.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(worker.FileSelectionResult{Files: []string{"test.js"}, Rationale: "legacy incomplete selection"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := c.products.CreateArtifact(ctx, product.ArtifactInput{
		ProjectID: packet.ProjectID,
		Name:      "incomplete-selection.json",
		Kind:      "task_file_selection",
		MIMEType:  "application/json",
		Content:   string(body),
		Metadata: map[string]any{
			"task_id":      packet.TaskID,
			"step_id":      packet.Step.ID,
			"attempt_id":   attemptID,
			"packet_hash":  packet.CanonicalPacketHash,
			"operation_id": effect.OperationID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.tasks.ObserveEffect(ctx, effect.OperationID, map[string]any{
		"status":      "selected",
		"files":       []string{"test.js"},
		"artifact_id": artifact.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := c.SelectFiles(ctx, SelectFilesInput{AttemptID: attemptID, ProviderID: providerID, Authority: authority})
	if err == nil {
		t.Fatal("SelectFiles must fail: durable selection missing required sum.js")
	}
	if adapter.selectionCalls != 0 {
		t.Fatalf("durable effect replayed provider: selectionCalls=%d want 0", adapter.selectionCalls)
	}
	if out.Effect.OperationID != effect.OperationID {
		t.Fatalf("OperationID=%s want %s", out.Effect.OperationID, effect.OperationID)
	}
}

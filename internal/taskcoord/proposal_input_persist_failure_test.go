package taskcoord

import (
	"context"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/taskengine"
)

func TestProposalInputPersistenceFailureIsErrorAndNotReplayed(t *testing.T) {
	a := &goalAdapter{}
	c, ts, task, startInput, _, db := newGoalFixtureWithStore(t, a, taskengine.GoalContract{})
	ctx := context.Background()

	planned, err := c.AutoPlan(ctx, AutoPlanInput{
		TaskID:               task.ID,
		ExpectedTaskRevision: task.Revision,
		ProviderID:           startInput.ProviderID,
		Actor:                "planner",
	})
	if err != nil {
		t.Fatalf("AutoPlan: %v", err)
	}
	if planned.Task.ID != task.ID {
		t.Fatalf("planned task mismatch: %s", planned.Task.ID)
	}
	task = planned.Task

	packet, err := ts.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatalf("BuildNextStepPacket: %v", err)
	}

	run, err := ts.BeginRun(ctx, taskengine.BeginRunInput{
		TaskID:               task.ID,
		ExpectedTaskRevision: task.Revision,
		Owner:                "coordinator",
		LeaseDuration:        time.Minute,
	})
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}

	task, err = ts.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	attempt, err := ts.BeginStepAttempt(ctx, taskengine.BeginAttemptInput{
		RunID:                run.ID,
		LeaseToken:           run.LeaseToken,
		StepKey:              packet.Step.Key,
		ExpectedTaskRevision: task.Revision,
		ExpectedStepRevision: task.Plan.Steps[0].Revision,
		InputHash:            packet.CanonicalPacketHash,
		Packet:               &packet,
	})
	if err != nil {
		t.Fatalf("BeginStepAttempt: %v", err)
	}

	_, err = db.DB.ExecContext(ctx, `CREATE TRIGGER reject_input BEFORE INSERT ON artifacts WHEN NEW.kind='code_proposal_input' BEGIN SELECT RAISE(ABORT,'snapshot-write-rejected'); END`)
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	doc, err := c.products.ReadProjectFile(ctx, task.ProjectID, "sum.js")
	if err != nil {
		t.Fatalf("ReadProjectFile: %v", err)
	}
	authority := taskengine.RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}

	out, err := c.Propose(ctx, ProposalInput{
		AttemptID:  attempt.ID,
		ProviderID: startInput.ProviderID,
		Authority:  authority,
		Files:      []string{"sum.js"},
	})
	if err == nil {
		t.Fatalf("Propose: expected error, got none")
	}
	if !strings.Contains(err.Error(), "snapshot-write-rejected") {
		t.Fatalf("Propose error missing snapshot-write-rejected: %v", err)
	}
	if out.Proposal.ID != "" {
		t.Fatalf("Proposal.ID should be empty, got %s", out.Proposal.ID)
	}
	if out.Artifact.ID != "" {
		t.Fatalf("Artifact.ID should be empty, got %s", out.Artifact.ID)
	}
	if out.Effect.State != taskengine.EffectObserved {
		t.Fatalf("Effect.State should be EffectObserved, got %s", out.Effect.State)
	}
	if out.Effect.Receipt["status"] != "input_persist_failed" {
		t.Fatalf("Receipt status should be input_persist_failed, got %v", out.Effect.Receipt["status"])
	}

	doc2, err := c.products.ReadProjectFile(ctx, task.ProjectID, "sum.js")
	if err != nil {
		t.Fatalf("ReadProjectFile after: %v", err)
	}
	if doc2.SHA256 != doc.SHA256 {
		t.Fatalf("sum.js SHA256 changed: got %s want %s", doc2.SHA256, doc.SHA256)
	}

	out, err = c.Propose(ctx, ProposalInput{
		AttemptID:  attempt.ID,
		ProviderID: startInput.ProviderID,
		Authority:  authority,
		Files:      []string{"sum.js"},
	})
	if err == nil {
		t.Fatalf("second Propose: expected error, got none")
	}
	if a.proposals != 1 {
		t.Fatalf("a.proposals should be 1, got %d", a.proposals)
	}
}

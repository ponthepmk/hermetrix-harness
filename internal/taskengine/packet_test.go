package taskengine

import (
	"context"
	"strings"
	"testing"
)

func TestBuildNextStepPacketIsStableAndAdvancesWithEvidence(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)

	first, err := service.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Step.Key != "reproduce" || first.Objective != task.Objective {
		t.Fatalf("first packet=%+v", first)
	}
	if first.CanonicalPacketHash == "" || first.CanonicalPacketHash != second.CanonicalPacketHash {
		t.Fatalf("packet hash was not deterministic: %q != %q", first.CanonicalPacketHash, second.CanonicalPacketHash)
	}

	reproduce := task.Plan.Steps[0]
	task, err = service.StartStep(ctx, task.ID, reproduce.Key, task.Revision, reproduce.Revision)
	if err != nil {
		t.Fatal(err)
	}
	reproduce = task.Plan.Steps[0]
	if _, err = service.RecordValidation(ctx, Validation{
		TaskID:          task.ID,
		StepID:          reproduce.ID,
		CheckID:         "failure-captured",
		SubjectRevision: StepSubjectRevision(reproduce),
		Status:          ValidationPass,
		EvidenceRefs:    []string{"artifact:failing-test"},
	}); err != nil {
		t.Fatal(err)
	}
	task, err = service.CompleteStep(ctx, task.ID, reproduce.Key, task.Revision, reproduce.Revision)
	if err != nil {
		t.Fatal(err)
	}

	packet, err := service.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Step.Key != "fix" || len(packet.CompletedSteps) != 1 || packet.CompletedSteps[0] != "reproduce" {
		t.Fatalf("advanced packet=%+v", packet)
	}
	if len(packet.ValidationEvidence) != 1 || packet.ValidationEvidence[0].EvidenceRefs[0] != "artifact:failing-test" {
		t.Fatalf("validation evidence=%+v", packet.ValidationEvidence)
	}
}

func TestBuildNextStepPacketCarriesRecoveryAndUncertainEffects(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "worker-a"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{
		RunID: run.ID, LeaseToken: run.LeaseToken, StepKey: "reproduce",
		ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision,
		InputHash: "sha256:packet",
	})
	if err != nil {
		t.Fatal(err)
	}
	authority := RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}
	effect, err := service.PlanEffect(ctx, authority, attempt.ID, "write", "workspace:test.go", "workspace.write")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.DispatchEffect(ctx, authority, effect.OperationID); err != nil {
		t.Fatal(err)
	}
	if recovered, recoverErr := service.RecoverInterrupted(ctx); recoverErr != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, recoverErr)
	}

	packet, err := service.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Checkpoint == nil || packet.Step.Key != "reproduce" {
		t.Fatalf("recovery packet=%+v", packet)
	}
	if len(packet.UnresolvedEffects) != 1 || packet.UnresolvedEffects[0].State != EffectUncertain {
		t.Fatalf("unresolved effects=%+v", packet.UnresolvedEffects)
	}
}

func TestBuildNextStepPacketRefusesToSilentlyTruncateAuthority(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task, err := service.Create(ctx, CreateTaskInput{
		Title:           "Large task",
		Objective:       "retain every constraint",
		OriginalRequest: strings.Repeat("required detail ", maxStepPacketBytes/8),
		Criteria:        []Criterion{{ID: "AC-1", Description: "all constraints remain authoritative"}},
		Actor:           "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.CreatePlan(ctx, CreatePlanInput{
		TaskID: task.ID, ExpectedTaskRevision: task.Revision, RequirementRevision: task.ActiveRequirementRevision,
		Reason: "bounded execution", Actor: "planner",
		Steps: []StepSpec{{Key: "work", Title: "Work", Instructions: "do not truncate"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.BuildNextStepPacket(ctx, task.ID); err == nil || !strings.Contains(err.Error(), "instead of truncating") {
		t.Fatalf("oversized packet error=%v", err)
	}
}

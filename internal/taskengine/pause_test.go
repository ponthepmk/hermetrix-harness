package taskengine

import (
	"context"
	"testing"
	"time"
)

func TestPauseIdleRunAllowsGoalRevisionWithoutFalseFailure(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, s)
	run, err := s.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "runner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, _ = s.Get(ctx, task.ID)
	input := PauseRunInput{RunID: run.ID, LeaseToken: "wrong", ExpectedTaskRevision: task.Revision, Actor: "owner", Reason: "revise scope"}
	if _, err = s.PauseIdleRun(ctx, input); err == nil {
		t.Fatal("wrong lease paused a run")
	}
	input.LeaseToken = run.LeaseToken
	paused, err := s.PauseIdleRun(ctx, input)
	if err != nil || paused.State != StatePaused {
		t.Fatalf("pause: %+v %v", paused, err)
	}
	stored, _ := s.getRun(ctx, run.ID)
	if stored.State != RunPaused || stored.LeaseGeneration <= run.LeaseGeneration {
		t.Fatal("old execution authority not revoked")
	}
	if paused.Plan.Steps[0].State != StepPending {
		t.Fatal("pause invented a failed step")
	}
	if _, err = s.ReviseRequirements(ctx, ReviseRequirementsInput{TaskID: task.ID, ExpectedTaskRevision: paused.Revision, Actor: "owner", Criteria: paused.Requirement.Criteria}); err != nil {
		t.Fatal("idle pause still prevents steering", err)
	}
}
func TestPauseRefusesAnActiveAttempt(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, s)
	run, err := s.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "runner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, _ = s.Get(ctx, task.ID)
	_, err = s.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken, StepKey: "reproduce", ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision, InputHash: "sha256:packet"})
	if err != nil {
		t.Fatal(err)
	}
	task, _ = s.Get(ctx, task.ID)
	if _, err = s.PauseIdleRun(ctx, PauseRunInput{RunID: run.ID, LeaseToken: run.LeaseToken, ExpectedTaskRevision: task.Revision, Actor: "owner", Reason: "stop"}); err == nil {
		t.Fatal("active work was labelled safely paused")
	}
	stored, _ := s.getRun(ctx, run.ID)
	if stored.State != RunRunning {
		t.Fatal("refusal mutated run")
	}
}

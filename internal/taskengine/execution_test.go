package taskengine

import (
	"context"
	"testing"
	"time"
)

func TestObservedEffectLetsAttemptFinish(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "runner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: "reproduce", ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision, InputHash: "sha256:packet"})
	if err != nil {
		t.Fatal(err)
	}
	authority := RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}
	effect, err := service.PlanEffect(ctx, authority, attempt.ID, "workspace.run", "go test ./...", "task-plan")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.DispatchEffect(ctx, authority, effect.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CompleteAttempt(ctx, authority, attempt.ID, "too early"); err == nil {
		t.Fatal("attempt completed while dispatched effect had no receipt")
	}
	observed, err := service.ObserveEffect(ctx, effect.OperationID, map[string]any{"exit_code": 0, "artifact_id": "artifact:test"})
	if err != nil {
		t.Fatal(err)
	}
	if observed.State != EffectObserved {
		t.Fatalf("effect state=%s", observed.State)
	}
	completed, err := service.CompleteAttempt(ctx, authority, attempt.ID, "test passed")
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != AttemptCompleted {
		t.Fatalf("attempt state=%s", completed.State)
	}
}

func TestEffectOutsideStepScopeIsRejected(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "runner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: "reproduce", ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision, InputHash: "sha256:packet"})
	if err != nil {
		t.Fatal(err)
	}
	authority := RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}
	if _, err = service.PlanEffect(ctx, authority, attempt.ID, "desktop.delete", "window:customer-data", "claimed-authority"); err == nil {
		t.Fatal("effect outside the planned scope was accepted")
	}
	if _, err = service.PlanEffect(ctx, authority, attempt.ID, "desktop.click", "window:checkout/button:pay", "claimed-authority"); err != nil {
		t.Fatalf("effect inside planned scope was rejected: %v", err)
	}
}

func TestExecutionSnapshotRestoresLatestDurableRun(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	packet, err := service.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := service.Execution(ctx, task.ID)
	if err != nil || empty.Task.ID != task.ID || empty.Run != nil || empty.Attempt != nil || empty.Proposal != nil || len(empty.Effects) != 0 {
		t.Fatalf("empty snapshot=%+v err=%v", empty, err)
	}
	run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "runner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: "reproduce", ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision,
		InputHash: packet.CanonicalPacketHash, Packet: &packet})
	if err != nil {
		t.Fatal(err)
	}
	authority := RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}
	effect, err := service.PlanEffect(ctx, authority, attempt.ID, "workspace.run", "go test ./...", "task-plan")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Execution(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Run == nil || snapshot.Run.ID != run.ID || snapshot.Attempt == nil || snapshot.Attempt.ID != attempt.ID ||
		snapshot.Attempt.Packet == nil || snapshot.Attempt.Packet.CanonicalPacketHash != packet.CanonicalPacketHash ||
		snapshot.Proposal != nil || len(snapshot.Effects) != 1 || snapshot.Effects[0].OperationID != effect.OperationID {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestRecoveryMakesDispatchedEffectUncertainAndBlocksReplay(t *testing.T) {
	service, dataStore := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "runner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, _ = service.Get(ctx, task.ID)
	attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: "reproduce", ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision, InputHash: "sha256:packet"})
	if err != nil {
		t.Fatal(err)
	}
	authority := RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}
	effect, err := service.PlanEffect(ctx, authority, attempt.ID, "desktop.click", "window:checkout/button:pay", "approved-scope")
	if err != nil {
		t.Fatal(err)
	}
	notDispatched, err := service.PlanEffect(ctx, authority, attempt.ID, "desktop.type", "window:checkout/field:note", "approved-scope")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.DispatchEffect(ctx, authority, effect.OperationID); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.RecoverInterrupted(ctx)
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	paused, err := service.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.State != StatePaused || paused.Plan.Steps[0].State != StepBlocked {
		t.Fatalf("paused task=%+v", paused)
	}
	storedEffect, err := service.getEffect(ctx, effect.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if storedEffect.State != EffectUncertain {
		t.Fatalf("effect state=%s", storedEffect.State)
	}
	abandoned, err := service.getEffect(ctx, notDispatched.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if abandoned.State != EffectAbandoned {
		t.Fatalf("undispatched effect state=%s", abandoned.State)
	}
	storedAttempt, err := service.getAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedAttempt.State != AttemptInterrupted {
		t.Fatalf("attempt state=%s", storedAttempt.State)
	}
	storedRun, err := service.getRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedRun.State != RunPaused {
		t.Fatalf("run state=%s", storedRun.State)
	}
	if _, err = service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: paused.Revision, Owner: "replacement"}); err == nil {
		t.Fatal("new run started while an effect outcome was uncertain")
	}
	reconciled, err := service.ReconcileEffect(ctx, effect.OperationID, map[string]any{"observed": "button was not activated"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.State != EffectReconciled {
		t.Fatalf("reconciled state=%s", reconciled.State)
	}
	if _, err = service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: paused.Revision, Owner: "replacement"}); err != nil {
		t.Fatalf("reconciled task could not resume: %v", err)
	}
	var checkpoints int
	if err = dataStore.DB.QueryRow(`SELECT COUNT(*) FROM task_checkpoints WHERE task_id=?`, task.ID).Scan(&checkpoints); err != nil || checkpoints != 1 {
		t.Fatalf("checkpoints=%d err=%v", checkpoints, err)
	}
}

func TestExpiredLeaseCannotStartAttemptOrRenew(t *testing.T) {
	service, dataStore := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "runner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dataStore.DB.Exec(`UPDATE task_runs SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?`, run.ID); err != nil {
		t.Fatal(err)
	}
	task, _ = service.Get(ctx, task.ID)
	if _, err = service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: "reproduce", ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision, InputHash: "sha256:packet"}); err == nil {
		t.Fatal("expired run started an attempt")
	}
	if _, err = service.RenewRunLease(ctx, run.ID, run.LeaseToken, time.Minute); err == nil {
		t.Fatal("expired run lease was renewed")
	}
}

func TestRecoverExpiredRunPausesWithoutReplayingEffects(t *testing.T) {
	service, dataStore := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "runner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, _ = service.Get(ctx, task.ID)
	attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: "reproduce", ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision, InputHash: "sha256:packet"})
	if err != nil {
		t.Fatal(err)
	}
	authority := RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}
	planned, err := service.PlanEffect(ctx, authority, attempt.ID, "desktop.type", "window:fixture/field:value", "approved-scope")
	if err != nil {
		t.Fatal(err)
	}
	dispatched, err := service.PlanEffect(ctx, authority, attempt.ID, "desktop.click", "window:fixture/button:save", "approved-scope")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.DispatchEffect(ctx, authority, dispatched.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err = dataStore.DB.Exec(`UPDATE task_runs SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?`, run.ID); err != nil {
		t.Fatal(err)
	}
	task, _ = service.Get(ctx, task.ID)
	recovered, err := service.RecoverExpiredRun(ctx, RecoverExpiredRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != StatePaused || recovered.Plan.Steps[0].State != StepBlocked || recovered.PauseReason == "" {
		t.Fatalf("recovered task=%+v", recovered)
	}
	storedPlanned, _ := service.getEffect(ctx, planned.OperationID)
	storedDispatched, _ := service.getEffect(ctx, dispatched.OperationID)
	storedAttempt, _ := service.getAttempt(ctx, attempt.ID)
	storedRun, _ := service.getRun(ctx, run.ID)
	if storedPlanned.State != EffectAbandoned || storedDispatched.State != EffectUncertain ||
		storedAttempt.State != AttemptInterrupted || storedRun.State != RunPaused {
		t.Fatalf("planned=%s dispatched=%s attempt=%s run=%s", storedPlanned.State, storedDispatched.State, storedAttempt.State, storedRun.State)
	}
	if _, err = service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: recovered.Revision, Owner: "replacement"}); err == nil {
		t.Fatal("new run started before uncertain effect reconciliation")
	}
	if _, err = service.ReconcileEffect(ctx, dispatched.OperationID, map[string]any{"observed": "save did not happen"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: recovered.Revision, Owner: "replacement"}); err != nil {
		t.Fatalf("reconciled task could not resume: %v", err)
	}
	var checkpoints int
	if err = dataStore.DB.QueryRow(`SELECT COUNT(*) FROM task_checkpoints WHERE task_id=?`, task.ID).Scan(&checkpoints); err != nil || checkpoints != 1 {
		t.Fatalf("checkpoints=%d err=%v", checkpoints, err)
	}
}

func TestRecoverExpiredRunRefusesLiveLease(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	if _, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "runner", LeaseDuration: time.Minute}); err != nil {
		t.Fatal(err)
	}
	task, _ = service.Get(ctx, task.ID)
	if _, err := service.RecoverExpiredRun(ctx, RecoverExpiredRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Actor: "owner"}); err == nil {
		t.Fatal("live run was recovered")
	}
}

func TestRecoverExpiredRunRenewsAuthorityWhenEveryEffectHasAReceipt(t *testing.T) {
	service, dataStore := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "runner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, _ = service.Get(ctx, task.ID)
	attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: "reproduce", ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision, InputHash: "sha256:packet"})
	if err != nil {
		t.Fatal(err)
	}
	authority := RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}
	effect, err := service.PlanEffect(ctx, authority, attempt.ID, "desktop.click", "window:fixture/button:save", "approved-scope")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.DispatchEffect(ctx, authority, effect.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ObserveEffect(ctx, effect.OperationID, map[string]any{"status": "completed"}); err != nil {
		t.Fatal(err)
	}
	if _, err = dataStore.DB.Exec(`UPDATE task_runs SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?`, run.ID); err != nil {
		t.Fatal(err)
	}
	task, _ = service.Get(ctx, task.ID)
	recovered, err := service.RecoverExpiredRun(ctx, RecoverExpiredRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Actor: "owner"})
	if err != nil || recovered.State != StateRunning {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	continuedRun, err := service.getRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	continuedAttempt, _ := service.getAttempt(ctx, attempt.ID)
	if continuedRun.State != RunRunning || continuedRun.LeaseGeneration != 2 || continuedRun.LeaseToken == run.LeaseToken ||
		!continuedRun.LeaseExpiresAt.After(time.Now()) || continuedAttempt.State != AttemptRunning {
		t.Fatalf("run=%+v attempt=%+v", continuedRun, continuedAttempt)
	}
	if _, err = service.PlanEffect(ctx, RunAuthority{RunID: continuedRun.ID, LeaseToken: continuedRun.LeaseToken}, attempt.ID,
		"desktop.type", "window:fixture/field:next", "approved-scope"); err != nil {
		t.Fatalf("renewed authority could not continue exact attempt: %v", err)
	}
}

func TestExpiredLeaseCannotPlanDispatchOrCompleteEffects(t *testing.T) {
	service, dataStore := testService(t)
	ctx := context.Background()

	makeAttempt := func(t *testing.T) (Run, StepAttempt, RunAuthority) {
		t.Helper()
		task := createPlannedTask(t, service)
		run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
			Owner: "runner", LeaseDuration: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		task, err = service.Get(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
			StepKey: "reproduce", ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision,
			InputHash: "sha256:packet"})
		if err != nil {
			t.Fatal(err)
		}
		return run, attempt, RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}
	}

	t.Run("plan", func(t *testing.T) {
		run, attempt, authority := makeAttempt(t)
		if _, err := dataStore.DB.Exec(`UPDATE task_runs SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?`, run.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := service.PlanEffect(ctx, authority, attempt.ID, "workspace.run", "go test ./...", "task-plan"); err == nil {
			t.Fatal("expired authority planned an effect")
		}
	})

	t.Run("dispatch and complete", func(t *testing.T) {
		run, attempt, authority := makeAttempt(t)
		effect, err := service.PlanEffect(ctx, authority, attempt.ID, "workspace.run", "go test ./...", "task-plan")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = dataStore.DB.Exec(`UPDATE task_runs SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?`, run.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = service.DispatchEffect(ctx, authority, effect.OperationID); err == nil {
			t.Fatal("expired authority dispatched an effect")
		}
		if _, err = service.AbandonEffect(ctx, effect.OperationID, "lease expired before dispatch"); err != nil {
			t.Fatal(err)
		}
		if _, err = service.CompleteAttempt(ctx, authority, attempt.ID, "done"); err == nil {
			t.Fatal("expired authority completed an attempt")
		}
	})
}

package taskengine

import (
	"context"
	"errors"
	"testing"

	"hermetrix-harness/internal/store"
)

func testService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	return NewService(dataStore), dataStore
}

func createPlannedTask(t *testing.T, service *Service) Task {
	t.Helper()
	ctx := context.Background()
	task, err := service.Create(ctx, CreateTaskInput{Title: "Fix checkout", Objective: "remove duplicate charge",
		OriginalRequest: "customer is charged twice", Constraints: []string{"preserve API compatibility"},
		Criteria: []Criterion{{ID: "AC-1", Description: "regression reproduces once before and passes after"}}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.CreatePlan(ctx, CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Reason: "first evidence-bound plan", Actor: "planner",
		Steps: []StepSpec{
			{Key: "reproduce", Title: "Reproduce", Instructions: "capture the failing case", Checks: []string{"failure-captured"},
				EffectScope: []string{"workspace.run", "desktop.click", "desktop.type", "write"}},
			{Key: "fix", Title: "Fix", Instructions: "apply the smallest patch", Dependencies: []string{"reproduce"}, Checks: []string{"targeted-test"}},
		}})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestPlanAndStepTransitionsRequireDependenciesAndExactEvidence(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	if task.State != StateReady || len(task.Plan.Steps) != 2 {
		t.Fatalf("planned task = %+v", task)
	}
	fix := task.Plan.Steps[1]
	if _, err := service.StartStep(ctx, task.ID, fix.Key, task.Revision, fix.Revision); err == nil {
		t.Fatal("dependent step started before its dependency")
	}
	reproduce := task.Plan.Steps[0]
	task, err := service.StartStep(ctx, task.ID, reproduce.Key, task.Revision, reproduce.Revision)
	if err != nil {
		t.Fatal(err)
	}
	reproduce = task.Plan.Steps[0]
	if _, err = service.CompleteStep(ctx, task.ID, reproduce.Key, task.Revision, reproduce.Revision); err == nil {
		t.Fatal("step completed without a passing check")
	}
	if _, err = service.RecordValidation(ctx, Validation{TaskID: task.ID, StepID: reproduce.ID,
		CheckID: "failure-captured", SubjectRevision: StepSubjectRevision(reproduce), Status: ValidationPass}); err == nil {
		t.Fatal("passing validation without evidence was accepted")
	}
	if _, err = service.RecordValidation(ctx, Validation{TaskID: task.ID, StepID: reproduce.ID,
		CheckID: "failure-captured", SubjectRevision: "stale-step", Status: ValidationPass, EvidenceRefs: []string{"artifact:old"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CompleteStep(ctx, task.ID, reproduce.Key, task.Revision, reproduce.Revision); err == nil {
		t.Fatal("stale validation closed the current step")
	}
	if _, err = service.RecordValidation(ctx, Validation{TaskID: task.ID, StepID: reproduce.ID,
		CheckID: "failure-captured", SubjectRevision: StepSubjectRevision(reproduce), Status: ValidationPass, EvidenceRefs: []string{"artifact:failing-test"}}); err != nil {
		t.Fatal(err)
	}
	task, err = service.CompleteStep(ctx, task.ID, reproduce.Key, task.Revision, reproduce.Revision)
	if err != nil {
		t.Fatal(err)
	}
	fix = task.Plan.Steps[1]
	if _, err = service.StartStep(ctx, task.ID, fix.Key, task.Revision, fix.Revision); err != nil {
		t.Fatalf("dependency did not unlock after evidence-bound completion: %v", err)
	}
}

func TestTaskCannotCompleteUntilEveryStepAndAcceptanceCriterionPass(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	for index := range task.Plan.Steps {
		step := task.Plan.Steps[index]
		var err error
		task, err = service.StartStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
		if err != nil {
			t.Fatal(err)
		}
		step = task.Plan.Steps[index]
		for _, check := range step.Checks {
			if _, err = service.RecordValidation(ctx, Validation{TaskID: task.ID, StepID: step.ID, CheckID: check,
				SubjectRevision: StepSubjectRevision(step), Status: ValidationPass, EvidenceRefs: []string{"receipt:" + check}}); err != nil {
				t.Fatal(err)
			}
		}
		task, err = service.CompleteStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.CompleteTask(ctx, task.ID, task.Revision); err == nil {
		t.Fatal("task completed without requirement-level acceptance evidence")
	}
	if _, err := service.RecordValidation(ctx, Validation{TaskID: task.ID, RequirementID: "AC-1", CheckID: "acceptance",
		SubjectRevision: RequirementSubjectRevision(task), Status: ValidationPass, EvidenceRefs: []string{"receipt:full-regression"}}); err != nil {
		t.Fatal(err)
	}
	completed, err := service.CompleteTask(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != StateCompleted {
		t.Fatalf("state=%s", completed.State)
	}
	if _, err = service.CreatePlan(ctx, CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: completed.Revision,
		RequirementRevision: 1, Reason: "rewrite completed work", Actor: "planner", Steps: []StepSpec{{Key: "again", Title: "Again", Instructions: "repeat"}}}); err == nil {
		t.Fatal("completed task accepted a new plan")
	}
}

func TestOptimisticTaskRevisionRejectsStaleWriter(t *testing.T) {
	service, _ := testService(t)
	task := createPlannedTask(t, service)
	_, err := service.StartStep(context.Background(), task.ID, "reproduce", task.Revision-1, task.Plan.Steps[0].Revision)
	if !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale transition error=%v", err)
	}
}

func TestRequirementRevisionInvalidatesOldPlanAndEvidence(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	revised, err := service.ReviseRequirements(ctx, ReviseRequirementsInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision, Constraints: []string{"preserve API compatibility", "do not retry payments"},
		Criteria: []Criterion{{ID: "AC-2", Description: "payment is idempotent under retry"}}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if revised.ActiveRequirementRevision != 2 || revised.ActivePlanRevision != 0 || revised.State != StateDraft || revised.Requirement.SupersedesID == "" {
		t.Fatalf("revised task=%+v", revised)
	}
	if _, err = service.CompleteTask(ctx, revised.ID, revised.Revision); err == nil {
		t.Fatal("superseded plan/evidence closed revised requirements")
	}
}

func TestPlanRejectsDependencyCycle(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task, err := service.Create(ctx, CreateTaskInput{Title: "Cycle", Objective: "reject cycle", OriginalRequest: "cycle",
		Criteria: []Criterion{{ID: "AC-1", Description: "cycle is rejected"}}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreatePlan(ctx, CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: 1, Reason: "invalid graph", Actor: "planner", Steps: []StepSpec{
			{Key: "a", Title: "A", Instructions: "A", Dependencies: []string{"b"}},
			{Key: "b", Title: "B", Instructions: "B", Dependencies: []string{"a"}},
		}})
	if err == nil {
		t.Fatal("cyclic plan was accepted")
	}
}

func TestPlanRejectsUnknownAcceptanceCriterionMapping(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task, err := service.Create(ctx, CreateTaskInput{Title: "Mapping", Objective: "bind evidence", OriginalRequest: "map checks",
		Criteria: []Criterion{{ID: "AC-1", Description: "known criterion"}}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreatePlan(ctx, CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: 1, Reason: "invalid mapping", Actor: "planner", Steps: []StepSpec{{
			Key: "verify", Title: "Verify", Instructions: "run evidence", RequirementIDs: []string{"AC-missing"}, Checks: []string{"go test ./..."},
		}}})
	if err == nil {
		t.Fatal("plan mapped a step to an unknown acceptance criterion")
	}
}

func TestCheckpointRejectsStaleTaskRevision(t *testing.T) {
	service, _ := testService(t)
	task := createPlannedTask(t, service)
	_, err := service.Checkpoint(context.Background(), CheckpointInput{TaskID: task.ID,
		ExpectedTaskRevision: task.Revision - 1, NextAction: "stale"})
	if !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("checkpoint error=%v", err)
	}
}

func TestRecoveryPausesTaskAndPersistsResumeCheckpoint(t *testing.T) {
	service, dataStore := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	step := task.Plan.Steps[0]
	task, err := service.StartStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
	if err != nil {
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
	if paused.State != StatePaused || paused.PauseReason == "" {
		t.Fatalf("paused task=%+v", paused)
	}
	var checkpoints int
	var nextAction, prerequisites string
	if err = dataStore.DB.QueryRow(`SELECT COUNT(*),next_action,resume_prerequisites_json FROM task_checkpoints WHERE task_id=?`, task.ID).
		Scan(&checkpoints, &nextAction, &prerequisites); err != nil {
		t.Fatal(err)
	}
	if checkpoints != 1 || nextAction == "" || prerequisites == "[]" {
		t.Fatalf("checkpoint count=%d next=%q prerequisites=%s", checkpoints, nextAction, prerequisites)
	}
	second, err := service.RecoverInterrupted(ctx)
	if err != nil || second != 0 {
		t.Fatalf("second recovery=%d err=%v", second, err)
	}
}

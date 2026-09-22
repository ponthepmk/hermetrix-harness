package taskengine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDecisionStateRejectsStaleRevisionAndKeepsActionsBounded(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	if _, err := service.DecideNextAction(ctx, task.ID, task.Revision-1); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale decision error=%v", err)
	}
	result, err := service.DecideNextAction(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if result.State.TaskRevision != task.Revision || result.State.CurrentStep == nil || result.State.CurrentStep.Key != "reproduce" {
		t.Fatalf("state=%+v", result.State)
	}
	if result.Decision.ActionID != "inspect_file" || result.Decision.Backend != RuleDecisionRevision {
		t.Fatalf("decision=%+v", result.Decision)
	}
	allowed := map[string]bool{"inspect_file": true, "search_repository": true, "run_test": true, "retrieve_memory": true}
	for _, candidate := range result.Candidates {
		if !allowed[candidate.Type] {
			t.Fatalf("unexpected candidate=%+v", candidate)
		}
		if candidate.Type == "run_test" && (!candidate.RequiresPolicy || candidate.Risk != "execute") {
			t.Fatalf("run_test lost its execution policy boundary: %+v", candidate)
		}
	}
}

func TestDecisionRequiresReconciliationBeforeAnyNewWork(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	run, err := service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "runner"})
	if err != nil {
		t.Fatal(err)
	}
	task, _ = service.Get(ctx, task.ID)
	attempt, err := service.BeginStepAttempt(ctx, BeginAttemptInput{RunID: run.ID, LeaseToken: run.LeaseToken,
		StepKey: task.Plan.Steps[0].Key, ExpectedTaskRevision: task.Revision,
		ExpectedStepRevision: task.Plan.Steps[0].Revision, InputHash: "sha256:decision"})
	if err != nil {
		t.Fatal(err)
	}
	effect, err := service.PlanEffect(ctx, RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}, attempt.ID,
		"write", "workspace:file.go", "workspace.write")
	if err != nil {
		t.Fatal(err)
	}
	task, _ = service.Get(ctx, task.ID)
	result, err := service.DecideNextAction(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.ActionID != "reconcile_effects" || len(result.Candidates) != 1 ||
		result.State.UnresolvedEffects[0] != effect.OperationID {
		t.Fatalf("result=%+v", result)
	}
}

func TestDecisionOffersFinishOnlyWithCurrentAcceptanceEvidence(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	for {
		step, _, err := nextRunnableStep(task.Plan.Steps)
		if err != nil {
			break
		}
		stepKey := step.Key
		task, err = service.StartStep(ctx, task.ID, stepKey, task.Revision, step.Revision)
		if err != nil {
			t.Fatal(err)
		}
		for _, current := range task.Plan.Steps {
			if current.Key == stepKey {
				step = current
				break
			}
		}
		for _, check := range step.Checks {
			if _, err = service.RecordValidation(ctx, Validation{TaskID: task.ID, StepID: step.ID, CheckID: check,
				SubjectRevision: StepSubjectRevision(step), Status: ValidationPass, EvidenceRefs: []string{"artifact:" + check}}); err != nil {
				t.Fatal(err)
			}
		}
		task, err = service.CompleteStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
		if err != nil {
			t.Fatal(err)
		}
	}
	if task.State != StateVerifying {
		t.Fatalf("task state=%s", task.State)
	}
	before, err := service.DecideNextAction(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if before.Decision.ActionID == "finish" || before.State.CompletionEligible {
		t.Fatalf("finish offered without criterion evidence: %+v", before)
	}
	for _, criterion := range task.Requirement.Criteria {
		if _, err = service.RecordValidation(ctx, Validation{TaskID: task.ID, RequirementID: criterion.ID,
			CheckID: "accept-" + criterion.ID, SubjectRevision: RequirementSubjectRevision(task), Status: ValidationPass,
			EvidenceRefs: []string{"artifact:acceptance"}}); err != nil {
			t.Fatal(err)
		}
	}
	after, err := service.DecideNextAction(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if after.Decision.ActionID != "finish" || !after.State.CompletionEligible || len(after.Candidates) != 1 {
		t.Fatalf("finish decision=%+v", after)
	}
}

func TestDecisionUsesLatestCheckStatus(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, service)
	step := task.Plan.Steps[0]
	for _, status := range []string{ValidationFail, ValidationPass} {
		if _, err := service.RecordValidation(ctx, Validation{TaskID: task.ID, StepID: step.ID,
			CheckID: step.Checks[0], SubjectRevision: StepSubjectRevision(step), Status: status,
			EvidenceRefs: []string{"artifact:" + status}}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := service.DecideNextAction(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.State.FailedChecks) != 0 || result.Decision.ActionID != "inspect_file" {
		t.Fatalf("latest passing check remained failed: %+v", result)
	}
}

func TestBoundedDecisionTextPreservesUTF8(t *testing.T) {
	value := boundedDecisionText(strings.Repeat("ท", maxCompactGoal+1), maxCompactGoal)
	if !utf8.ValidString(value) || len([]rune(value)) != maxCompactGoal+1 {
		t.Fatalf("bounded text is not valid UTF-8 or has the wrong size")
	}
}

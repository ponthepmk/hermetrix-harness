package taskengine

import (
	"context"
	"testing"
	"time"
)

func TestPlanReviewBlocksExecutionAndRejectedPlanCannotBeReused(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task, err := service.Create(ctx, CreateTaskInput{Title: "Reviewed plan", Objective: "Fix navigation",
		OriginalRequest: "Fix navigation", Actor: "owner", Criteria: []Criterion{{ID: "AC-1", Description: "Navigation works"}},
		Contract: GoalContract{RequirePlanReview: true}})
	if err != nil {
		t.Fatal(err)
	}
	createPlan := func(task Task) Task {
		t.Helper()
		planned, planErr := service.CreatePlan(ctx, CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
			RequirementRevision: task.ActiveRequirementRevision, Actor: "planner", Reason: "Check navigation",
			Steps: []StepSpec{{Key: "verify", Title: "Verify", Instructions: "Run smoke",
				RequirementIDs: []string{"AC-1"}, Checks: []string{"smoke"}}}})
		if planErr != nil {
			t.Fatal(planErr)
		}
		return planned
	}
	task = createPlan(task)
	if task.State != StateWaitingForInput || task.Plan.ReviewStatus != "pending" || task.Plan.SubjectHash == "" {
		t.Fatalf("new plan did not wait for exact review: %+v", task)
	}
	step := task.Plan.Steps[0]
	if _, err = service.StartStep(ctx, task.ID, step.Key, task.Revision, step.Revision); err == nil {
		t.Fatal("step started without plan review")
	}
	if _, err = service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		Owner: "runner", LeaseDuration: time.Minute}); err == nil {
		t.Fatal("run started without plan review")
	}
	decision, err := service.DecideNextAction(ctx, task.ID, task.Revision)
	if err != nil || decision.Decision.ActionID != "review_plan" {
		t.Fatalf("next action did not request plan review: %+v %v", decision, err)
	}
	if _, err = service.ReviewPlan(ctx, task.ID, task.Revision, "wrong-hash", "owner", "approved", "reviewed"); err != ErrStaleRevision {
		t.Fatalf("stale plan approval = %v", err)
	}
	task, err = service.ReviewPlan(ctx, task.ID, task.Revision, task.Plan.SubjectHash, "owner", "rejected", "Missing browser check")
	if err != nil || task.State != StateDraft || task.Plan.ReviewStatus != "rejected" {
		t.Fatalf("rejected plan state = %+v err=%v", task, err)
	}
	if _, err = service.StartStep(ctx, task.ID, step.Key, task.Revision, step.Revision); err == nil {
		t.Fatal("rejected plan started")
	}
	task = createPlan(task)
	if task.Plan.Revision != 2 || task.Plan.ReviewStatus != "pending" {
		t.Fatalf("new plan reused rejected revision or approval: %+v", task.Plan)
	}
	previous, err := service.GetPlanRevision(ctx, task.ID, 1)
	if err != nil || previous.Revision != 1 || previous.SubjectHash == "" || len(previous.Steps) != 1 {
		t.Fatalf("historical plan is not readable: %+v err=%v", previous, err)
	}
	if _, err := service.GetPlanRevision(ctx, task.ID, 3); err != ErrNotFound {
		t.Fatalf("future plan revision should be absent: %v", err)
	}
	task, err = service.ReviewPlan(ctx, task.ID, task.Revision, task.Plan.SubjectHash, "owner", "approved", "Steps and checks reviewed")
	if err != nil || task.State != StateReady || task.Plan.ReviewStatus != "approved" {
		t.Fatalf("approved plan state = %+v err=%v", task, err)
	}
	step = task.Plan.Steps[0]
	if _, err = service.StartStep(ctx, task.ID, step.Key, task.Revision, step.Revision); err != nil {
		t.Fatalf("approved plan could not start: %v", err)
	}
}

func TestExperiencePreviewRevisionRequiresAReviewedNewPlan(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task, err := service.Create(ctx, CreateTaskInput{Title: "Design start flow", Objective: "Review before work",
		OriginalRequest: "Show the proposed UX before changing files", Actor: "owner",
		Criteria: []Criterion{{ID: "AC-1", Description: "The review screen shows the intended flow"}},
		Contract: GoalContract{RequirePlanReview: true, ExperiencePreview: "A draft screen shows goal, flow and checks"}})
	if err != nil {
		t.Fatal(err)
	}
	if task.Requirement.Contract.ExperiencePreview != "A draft screen shows goal, flow and checks" {
		t.Fatalf("experience preview was not saved: %+v", task.Requirement.Contract)
	}
	task, err = service.CreatePlan(ctx, CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Actor: "planner", Reason: "Show the proposed start screen",
		Steps: []StepSpec{{Key: "screen", Title: "Build start screen", Instructions: "Add review UI",
			RequirementIDs: []string{"AC-1"}, Checks: []string{"browser"}}}})
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.ReviewPlan(ctx, task.ID, task.Revision, task.Plan.SubjectHash, "owner", "approved", "Preview and checks reviewed")
	if err != nil {
		t.Fatal(err)
	}
	newContract := task.Requirement.Contract
	newContract.ExperiencePreview = "The draft screen also shows a return for edits action"
	task, err = service.ReviseRequirements(ctx, ReviseRequirementsInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		Actor: "owner", Criteria: task.Requirement.Criteria, Contract: &newContract})
	if err != nil {
		t.Fatal(err)
	}
	if task.ActivePlanRevision != 0 || task.Requirement.Contract.ExperiencePreview != newContract.ExperiencePreview {
		t.Fatalf("revised preview reused old plan: %+v", task)
	}
	if _, err = service.BeginRun(ctx, BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		Owner: "runner", LeaseDuration: time.Minute}); err == nil {
		t.Fatal("run started after preview changed without a reviewed new plan")
	}
}

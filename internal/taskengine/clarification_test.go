package taskengine

import (
	"context"
	"testing"
)

func TestAnsweringUnknownCreatesGoalRevisionAndInvalidatesPlan(t *testing.T) {
	service, _ := testService(t)
	ctx := context.Background()
	task, err := service.Create(ctx, CreateTaskInput{Title: "Content pilot", Objective: "prepare a reviewed post",
		OriginalRequest: "prepare a post", Unknowns: []string{"Which Page should receive it?"},
		Criteria: []Criterion{{ID: "AC-1", Description: "the draft is reviewed"}}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = service.CreatePlan(ctx, CreatePlanInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Reason: "draft and review", Actor: "planner",
		Steps: []StepSpec{{Key: "draft", Title: "Draft", Instructions: "prepare the post",
			RequirementIDs: []string{"AC-1"}, Checks: []string{"reviewed"}}}})
	if err != nil {
		t.Fatal(err)
	}
	contract := task.Requirement.Contract
	contract.Clarifications = []GoalClarification{{Question: "Which Page should receive it?", Answer: "The existing company Page"}}
	input := ReviseRequirementsInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Actor: "owner",
		Criteria: task.Requirement.Criteria, Constraints: task.Requirement.Constraints,
		Unknowns: task.Requirement.Unknowns, Contract: &contract}
	if _, err := service.ReviseRequirements(ctx, input); err == nil {
		t.Fatal("answered question remained in current unknowns")
	}
	input.Unknowns = nil
	contract.Clarifications[0].Question = "a question that was not open"
	if _, err := service.ReviseRequirements(ctx, input); err == nil {
		t.Fatal("invented clarification was accepted")
	}
	contract.Clarifications[0].Question = "Which Page should receive it?"
	revised, err := service.ReviseRequirements(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if revised.ActiveRequirementRevision != 2 || revised.ActivePlanRevision != 0 || revised.State != StateDraft ||
		len(revised.Requirement.Unknowns) != 0 || len(revised.Requirement.Contract.Clarifications) != 1 ||
		revised.Requirement.Contract.Clarifications[0].Answer != "The existing company Page" {
		t.Fatalf("answer did not revise Goal and invalidate plan: %+v", revised)
	}
	if _, err := service.CompleteTask(ctx, revised.ID, revised.Revision); err == nil {
		t.Fatal("old plan closed a Goal after clarification")
	}
	activity, err := service.ListActivity(ctx, revised.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	var currentGoal, oldGoal int
	for _, item := range activity {
		if item.Kind != "goal_revision" {
			continue
		}
		if item.Current {
			currentGoal++
		} else {
			oldGoal++
		}
	}
	if currentGoal != 1 || oldGoal != 1 {
		t.Fatalf("Goal revision activity current=%d old=%d: %+v", currentGoal, oldGoal, activity)
	}
	contract = revised.Requirement.Contract
	contract.Clarifications = nil
	_, err = service.ReviseRequirements(ctx, ReviseRequirementsInput{TaskID: revised.ID,
		ExpectedTaskRevision: revised.Revision, Actor: "owner", Criteria: revised.Requirement.Criteria, Contract: &contract})
	if err == nil {
		t.Fatal("clarification history was silently removed")
	}
}

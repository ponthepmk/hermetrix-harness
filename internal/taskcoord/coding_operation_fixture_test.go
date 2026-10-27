package taskcoord

import (
	"testing"

	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

type codingOperationFixture struct {
	operations    *CodingOperations
	coordinator   *Service
	tasks         *taskengine.Service
	data          *store.Store
	input         CodingOperationInput
	root          string
	adapter       *goalAdapter
	initialTaskID string
}

func newCodingOperationFixture(t *testing.T, a *goalAdapter) codingOperationFixture {
	if a == nil {
		a = &goalAdapter{}
	}
	coordinator, tasks, existingTask, goalInput, root, db := newGoalFixtureWithStore(t, a, taskengine.GoalContract{})

	newTaskInput := taskengine.CreateTaskInput{
		ProjectID:       existingTask.ProjectID,
		Title:           existingTask.Title,
		Objective:       existingTask.Objective,
		OriginalRequest: existingTask.OriginalRequest,
		Constraints:     existingTask.Requirement.Constraints,
		Unknowns:        existingTask.Requirement.Unknowns,
		Criteria:        existingTask.Requirement.Criteria,
		Contract:        existingTask.Requirement.Contract,
		Actor:           goalInput.Actor,
		EgressPolicy:    existingTask.EgressPolicy,
	}

	clearedGoal := goalInput
	clearedGoal.GoalID = ""
	clearedGoal.TaskID = ""
	clearedGoal.ExpectedTaskRevision = 0

	input := CodingOperationInput{
		OperationID: "coding-op-fixture",
		Task:        newTaskInput,
		Goal:        clearedGoal,
	}

	operations := NewCodingOperations(db, coordinator)

	return codingOperationFixture{
		operations:    operations,
		coordinator:   coordinator,
		tasks:         tasks,
		data:          db,
		input:         input,
		root:          root,
		adapter:       a,
		initialTaskID: existingTask.ID,
	}
}

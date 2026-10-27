package taskcoord

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"hermetrix-harness/internal/taskengine"
)

func (c *CodingOperations) Get(ctx context.Context, id string) (CodingOperationResult, error) {
	if err := c.configured(); err != nil {
		return CodingOperationResult{}, err
	}
	if id != strings.TrimSpace(id) {
		return CodingOperationResult{}, errors.New("coding operations: id must not contain leading or trailing whitespace")
	}
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 160 || !utf8.ValidString(id) {
		return CodingOperationResult{}, errors.New("coding operation id must be nonempty, at most 160 bytes, and valid UTF-8")
	}
	owner, err := codingOperationOwner(ctx, c.dataStore.DB)
	if err != nil {
		return CodingOperationResult{}, err
	}
	op, err := loadCodingOperation(ctx, c.dataStore.DB, owner, id)
	if err != nil {
		return CodingOperationResult{}, err
	}
	var req CodingOperationInput
	if err := json.Unmarshal(op.Request, &req); err != nil {
		return CodingOperationResult{}, err
	}
	task, err := c.coordinator.tasks.Get(ctx, op.TaskID)
	if err != nil {
		return CodingOperationResult{}, err
	}
	if task.OwnerPrincipalID != op.OwnerPrincipalID || task.ProjectID != req.Task.ProjectID {
		return CodingOperationResult{}, taskengine.ErrStaleRevision
	}
	goal, err := c.coordinator.tasks.Goal(ctx, op.GoalID)
	if err == taskengine.ErrNotFound {
		return CodingOperationResult{Operation: op, Task: task, Goal: nil}, nil
	}
	if err != nil {
		return CodingOperationResult{}, err
	}
	if goal.TaskID != op.TaskID || goal.Actor != req.Goal.Actor {
		return CodingOperationResult{}, errors.New("coding operation goal task or actor mismatch")
	}
	copy := goal.Config
	copy.StartInputSHA256 = ""
	if !reflect.DeepEqual(copy, op.GoalConfig) {
		return CodingOperationResult{}, errors.New("coding operation goal config mismatch")
	}
	return CodingOperationResult{Operation: op, Task: task, Goal: &goal}, nil
}

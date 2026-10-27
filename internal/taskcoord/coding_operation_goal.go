package taskcoord

import (
	"context"
	"encoding/json"
	"reflect"

	"errors"

	"hermetrix-harness/internal/taskengine"
)

// startCodingOperationGoal starts the frozen goal for a stored coding operation.
func (c *CodingOperations) startCodingOperationGoal(ctx context.Context, op CodingOperation) (CodingOperationResult, error) {
	if err := c.configured(); err != nil {
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
	_, err = c.coordinator.tasks.Goal(ctx, op.GoalID)
	if err == nil {
		if _, err := c.coordinator.tasks.CreateGoalAtID(ctx, op.GoalID, op.TaskID, op.InitialTaskRevision, req.Goal.Actor, op.GoalConfig); err != nil {
			return CodingOperationResult{}, err
		}
		return c.Get(ctx, op.ID)
	}
	if !errors.Is(err, taskengine.ErrNotFound) {
		return CodingOperationResult{}, err
	}
	cfg, err := c.coordinator.resolveCodingGoalConfig(ctx, task, req.Goal)
	if err != nil {
		return CodingOperationResult{}, err
	}
	if !reflect.DeepEqual(cfg, op.GoalConfig) {
		return CodingOperationResult{}, taskengine.ErrStaleRevision
	}
	if _, err := c.coordinator.tasks.CreateGoalAtID(ctx, op.GoalID, op.TaskID, op.InitialTaskRevision, req.Goal.Actor, op.GoalConfig); err != nil {
		return CodingOperationResult{}, err
	}
	return c.Get(ctx, op.ID)
}

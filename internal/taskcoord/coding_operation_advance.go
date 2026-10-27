package taskcoord

import (
	"context"

	"hermetrix-harness/internal/taskengine"
)

func (c *CodingOperations) Advance(ctx context.Context, id string) (CodingOperationResult, error) {
	res, err := c.Get(ctx, id)
	if err != nil {
		return CodingOperationResult{}, err
	}
	if res.Goal == nil {
		res, err = c.startCodingOperationGoal(ctx, res.Operation)
		if err != nil {
			return CodingOperationResult{}, err
		}
	}
	if res.Goal.State == taskengine.GoalActive && res.Goal.ClaimToken == "" {
		_, err = c.coordinator.AdvanceGoal(ctx, res.Goal.ID)
		if err != nil {
			return CodingOperationResult{}, err
		}
	}
	return c.Get(ctx, id)
}

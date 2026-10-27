package taskcoord

import (
	"bytes"
	"context"
	"errors"

	"hermetrix-harness/internal/taskengine"
)

func (c *CodingOperations) Start(ctx context.Context, in CodingOperationInput) (CodingOperationResult, error) {
	if err := c.configured(); err != nil {
		return CodingOperationResult{}, err
	}

	normalized, raw, sha, err := canonicalCodingInput(in)
	if err != nil {
		return CodingOperationResult{}, err
	}

	owner, err := codingOperationOwner(ctx, c.dataStore.DB)
	if err != nil {
		return CodingOperationResult{}, err
	}

	existing, err := loadCodingOperation(ctx, c.dataStore.DB, owner, normalized.OperationID)
	if err != nil {
		if !errors.Is(err, taskengine.ErrNotFound) {
			return CodingOperationResult{}, err
		}

		cfg, err := c.coordinator.resolveCodingGoalConfig(ctx, taskengine.Task{
			ProjectID:    normalized.Task.ProjectID,
			EgressPolicy: normalized.Task.EgressPolicy,
		}, normalized.Goal)
		if err != nil {
			return CodingOperationResult{}, err
		}

		op, err := c.createCodingOperation(ctx, normalized, raw, sha, owner, cfg)
		if err != nil {
			createErr := err
			re, readErr := loadCodingOperation(ctx, c.dataStore.DB, owner, normalized.OperationID)
			if readErr != nil {
				if errors.Is(readErr, taskengine.ErrNotFound) {
					return CodingOperationResult{}, createErr
				}
				return CodingOperationResult{}, readErr
			}
			if re.RequestSHA256 != sha || !bytes.Equal(re.Request, raw) {
				return CodingOperationResult{}, taskengine.ErrStaleRevision
			}
			return c.startCodingOperationGoal(ctx, re)
		}

		return c.startCodingOperationGoal(ctx, op)
	}

	if existing.RequestSHA256 != sha || !bytes.Equal(existing.Request, raw) {
		return CodingOperationResult{}, taskengine.ErrStaleRevision
	}

	return c.startCodingOperationGoal(ctx, existing)
}

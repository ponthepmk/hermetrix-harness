package taskcoord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"hermetrix-harness/internal/taskengine"
)

func (c *CodingOperations) createCodingOperation(ctx context.Context, in CodingOperationInput, raw []byte, sha, owner string, cfg taskengine.GoalConfig) (CodingOperation, error) {
	if err := c.configured(); err != nil {
		return CodingOperation{}, err
	}

	tx, err := c.dataStore.DB.BeginTx(ctx, nil)
	if err != nil {
		return CodingOperation{}, err
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()

	resolvedOwner, err := codingOperationOwner(ctx, tx)
	if err != nil {
		return CodingOperation{}, err
	}
	if resolvedOwner != owner {
		return CodingOperation{}, errors.New("coding operation owner mismatch")
	}

	existing, err := loadCodingOperation(ctx, tx, owner, in.OperationID)
	if err != nil {
		if err != taskengine.ErrNotFound {
			return CodingOperation{}, err
		}
	} else {
		if existing.RequestSHA256 != sha || !bytes.Equal(existing.Request, raw) {
			return CodingOperation{}, taskengine.ErrStaleRevision
		}
		return existing, nil
	}

	createdTask, err := c.coordinator.tasks.CreateInTx(ctx, tx, in.Task)
	if err != nil {
		return CodingOperation{}, err
	}

	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return CodingOperation{}, err
	}

	now := time.Now().UTC()

	result, err := tx.ExecContext(ctx, "INSERT INTO coding_goal_operations ("+codingOperationColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		in.OperationID, owner, sha, string(raw), createdTask.TaskID, codingGoalID(owner, in.OperationID), createdTask.TaskRevision, string(cfgJSON), now.Format(time.RFC3339Nano))
	if err != nil {
		return CodingOperation{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return CodingOperation{}, err
	}
	if rowsAffected != 1 {
		return CodingOperation{}, errors.New("coding operation insert affected 0 rows")
	}

	if err := tx.Commit(); err != nil {
		return CodingOperation{}, err
	}

	return CodingOperation{
		ID:                  in.OperationID,
		OwnerPrincipalID:    owner,
		RequestSHA256:       sha,
		TaskID:              createdTask.TaskID,
		GoalID:              codingGoalID(owner, in.OperationID),
		Request:             append([]byte(nil), raw...),
		InitialTaskRevision: createdTask.TaskRevision,
		GoalConfig:          cfg,
		CreatedAt:           now,
	}, nil
}

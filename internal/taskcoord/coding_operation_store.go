package taskcoord

import (
	"context"
	"database/sql"
	"errors"

	"hermetrix-harness/internal/taskengine"
)

const codingOperationColumns = "id,owner_principal_id,request_sha256,request_json,task_id,goal_id,initial_task_revision,goal_config_json,created_at"

func loadCodingOperation(ctx context.Context, q codingOperationQuery, owner, id string) (CodingOperation, error) {
	if owner == "" || id == "" {
		return CodingOperation{}, errors.New("coding operation: owner and id are required")
	}
	row := q.QueryRowContext(ctx, "SELECT "+codingOperationColumns+" FROM coding_goal_operations WHERE owner_principal_id=? AND id=?", owner, id)
	op, err := scanCodingOperation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CodingOperation{}, taskengine.ErrNotFound
		}
		return CodingOperation{}, err
	}
	return op, nil
}

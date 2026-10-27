package taskcoord

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/taskengine"
)

type codingOperationQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func codingOperationOwner(ctx context.Context, q codingOperationQuery) (string, error) {
	principal := identity.Principal(ctx)
	var query string
	var args []any
	if principal == "" {
		query = "SELECT id FROM local_principals WHERE kind='local' ORDER BY created_at LIMIT 1"
	} else {
		query = "SELECT id FROM local_principals WHERE id=?"
		args = []any{principal}
	}
	row := q.QueryRowContext(ctx, query, args...)
	var id string
	switch err := row.Scan(&id); err {
	case sql.ErrNoRows:
		return "", taskengine.ErrNotFound
	case nil:
		return id, nil
	default:
		return "", err
	}
}

func codingGoalID(owner, id string) string {
	values := []string{owner, id}
	data, err := json.Marshal(values)
	if err != nil {
		// json.Marshal cannot fail for []string; return a stable fallback
		// rather than panicking.
		return "goalop_"
	}
	hash := sha256.Sum256(data)
	return "goalop_" + hex.EncodeToString(hash[:])
}

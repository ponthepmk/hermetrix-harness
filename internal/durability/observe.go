// Package durability records failures from best-effort state reconciliation.
// These writes usually run from cleanup/error paths where there is no caller
// left to return an error to. Ignoring them makes the database claim work is
// still running forever; a structured log is the minimum honest outcome.
package durability

import (
	"database/sql"
	"log/slog"
)

// Exec names a best-effort reconciliation write. Observe then accepts
// sql.DB.ExecContext's two return values directly:
//
//	durability.Exec("mark job failed").Observe(db.ExecContext(...))
//
// The result is intentionally unused; callers that need a RowsAffected
// invariant must handle the execution themselves and return the error.
type Exec string

func (operation Exec) Observe(_ sql.Result, err error) {
	if err != nil {
		slog.Error("durability write failed", "operation", string(operation), "error", err)
	}
}

package taskengine

import (
	"context"
	"database/sql"
)

// CompletionStage writes only through the supplied transaction. It must not
// dispatch inference, perform an external effect or reopen the store connection.
type CompletionStage func(context.Context, *sql.Tx, Task, CompletionReport) error

// CompletionObserver prepares immutable local evidence before the transaction.
// Its stage commits an outbox fact atomically with completion, including when
// the user accepts results after the original reviewer has already returned.
type CompletionObserver interface {
	PrepareCompletion(context.Context, Task) (CompletionStage, error)
}

// WithCompletionObserver is composition-time wiring; it does not run an observer.
func (s *Service) WithCompletionObserver(observer CompletionObserver) *Service {
	s.completionObserver = observer
	return s
}

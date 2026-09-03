package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Deleting a session removes the conversation, not the record that it happened.
//
// The transcript, its context snapshots, its step bindings and its pending
// approvals belong to the chat and go with it. What was learned from that chat
// does not: Skill activations, Skill events, review jobs and token observations
// are evidence about Skills and models, and they outlive the conversation that
// produced them. An artifact keeps existing too -- a document the user asked
// for is theirs, and deleting the chat that requested it must not delete the
// file -- so it is detached rather than dropped.
//
// A session mid-turn is refused. Deleting the rows a running turn is writing to
// would leave that turn writing into nothing.

// ErrSessionRunning reports a delete refused because a turn is in flight.
var ErrSessionRunning = errors.New("this session has a turn running; wait for it to finish or cancel it first")

func (s *Service) DeleteSession(ctx context.Context, id string) error {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var state, activeTurn string
	err = tx.QueryRowContext(ctx, `SELECT state,COALESCE(active_turn_id,'') FROM agent_sessions WHERE id=?`,
		id).Scan(&state, &activeTurn)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("session %s does not exist", id)
	}
	if err != nil {
		return err
	}
	if state == "running" || activeTurn != "" {
		return ErrSessionRunning
	}

	// An artifact is a real output the user may still want. Detach it from the
	// conversation instead of deleting the file behind it.
	if _, err := tx.ExecContext(ctx, `UPDATE artifacts SET session_id=NULL WHERE session_id=?`, id); err != nil {
		return fmt.Errorf("detach artifacts: %w", err)
	}
	for _, statement := range []struct{ label, query string }{
		{"queued reviews", `DELETE FROM learning_trigger_outbox WHERE session_id=?`},
		{"retrieval index", `DELETE FROM event_embeddings WHERE session_id=?`},
		{"approvals", `DELETE FROM tool_approvals WHERE session_id=?`},
		{"step bindings", `DELETE FROM step_bindings WHERE session_id=?`},
		{"context snapshots", `DELETE FROM context_snapshots WHERE session_id=?`},
		{"transcript", `DELETE FROM agent_events WHERE session_id=?`},
		{"session", `DELETE FROM agent_sessions WHERE id=?`},
	} {
		if _, err := tx.ExecContext(ctx, statement.query, id); err != nil {
			return fmt.Errorf("delete %s: %w", statement.label, err)
		}
	}
	return tx.Commit()
}

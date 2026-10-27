package skills

import (
	"context"
	"database/sql"
	"fmt"
)

func validateBehavioralRunScope(ctx context.Context, tx *sql.Tx, candidate Candidate, bindings BehavioralRunBindings, ownerID string) error {
	if ownerID == "" {
		return fmt.Errorf("%w: ownerID must be nonempty", ErrBehavioralRunBindings)
	}
	if candidate.OwnerPrincipalID != ownerID {
		return fmt.Errorf("%w: candidate owner mismatch", ErrBehavioralRunBindings)
	}
	switch candidate.ScopeKind {
	case "user":
		if candidate.ScopeRef != "" {
			return fmt.Errorf("%w: user scope requires empty ScopeRef", ErrBehavioralRunBindings)
		}
	case "workspace":
		if bindings.ProjectID == "" || candidate.ScopeRef != bindings.ProjectID {
			return fmt.Errorf("%w: workspace scope requires ProjectID and matching ScopeRef", ErrBehavioralRunBindings)
		}
	default:
		return fmt.Errorf("%w: unsupported ScopeKind %q", ErrBehavioralRunBindings, candidate.ScopeKind)
	}
	if bindings.SessionID != "" && bindings.ProjectID == "" {
		return fmt.Errorf("%w: SessionID requires ProjectID", ErrBehavioralRunBindings)
	}
	if bindings.ProjectID != "" {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE id = ? AND owner_principal_id = ? AND state = 'active'`, bindings.ProjectID, ownerID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("%w: project not active for owner", ErrBehavioralRunBindings)
		}
	}
	if bindings.SessionID != "" {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_sessions WHERE id = ? AND owner_principal_id = ? AND project_id = ? AND state = 'active'`, bindings.SessionID, ownerID, bindings.ProjectID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("%w: session not active for project", ErrBehavioralRunBindings)
		}
	}
	return nil
}

package learning

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Scope is authority owned by the host. A model may propose a procedure, but
// project evidence cannot authorize learning that applies to every project.
func (s *Service) boundSuggestionToSource(ctx context.Context, sessionID string, suggestion *SuggestedSkill) error {
	var projectID string
	var err error
	if strings.HasPrefix(sessionID, "task:") {
		err = s.store.DB.QueryRowContext(ctx, `SELECT COALESCE(t.project_id,'') FROM durable_tasks t WHERE t.id=?`, strings.TrimPrefix(sessionID, "task:")).Scan(&projectID)
		if err != nil {
			return fmt.Errorf("resolve learning task scope: %w", err)
		}
	} else {
		err = s.store.DB.QueryRowContext(ctx, `SELECT COALESCE(project_id,'') FROM agent_sessions WHERE id=?`, sessionID).Scan(&projectID)
		// Existing external review clients use milestone IDs without an agent session.
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("resolve learning session scope: %w", err)
		}
	}
	kind, ref := "user", ""
	if projectID != "" {
		var active string
		if err = s.store.DB.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=?`, projectID).Scan(&active); err != nil || active != "active" {
			return fmt.Errorf("learning source project is unavailable")
		}
		kind, ref = "workspace", projectID
	}
	if suggestion.TargetSkillID != "" && (suggestion.ScopeKind != kind || suggestion.ScopeRef != ref) {
		return fmt.Errorf("learning source cannot modify a skill outside its scope")
	}
	suggestion.ScopeKind = kind
	suggestion.ScopeRef = ref
	suggestion.Owner = "user"
	return nil
}

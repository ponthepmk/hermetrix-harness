package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	toolruntime "hermetrix-harness/internal/tools"
)

// ErrSessionHasNoRoot is returned when a session's project has no code folder.
// The file tools then refuse rather than falling back to some other directory:
// silently reading a tree the user did not open is worse than an honest no.
var ErrSessionHasNoRoot = errors.New("this project has no code folder, so workspace tools are unavailable")

// scopedTools binds the registry to the session's project root.
//
// The project row is read with SQL rather than through internal/product on
// purpose. internal/agent does not import internal/product and must not start:
// the dependency runs the other way, with product holding an interface that
// *agent.Service satisfies.
func (s *Service) scopedTools(ctx context.Context, session Session) (*toolruntime.Registry, error) {
	if strings.TrimSpace(session.ProjectID) == "" {
		return nil, ErrSessionHasNoRoot
	}
	var root string
	err := s.store.DB.QueryRowContext(ctx,
		`SELECT COALESCE(root_path,'') FROM projects WHERE id=? AND state='active'`, session.ProjectID).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionHasNoRoot
	}
	if err != nil {
		return nil, fmt.Errorf("load project root: %w", err)
	}
	if strings.TrimSpace(root) == "" {
		return nil, ErrSessionHasNoRoot
	}
	return s.tools.For(root)
}

package product

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

// UpdateVisibility is the only product path that can make an object eligible
// for a project share. It changes metadata under optimistic concurrency and
// writes the audit in the same transaction.
func (s *Service) UpdateVisibility(ctx context.Context, input VisibilityInput) (VisibilityReceipt, error) {
	input.ObjectKind = strings.TrimSpace(input.ObjectKind)
	input.ObjectID = strings.TrimSpace(input.ObjectID)
	input.Actor = strings.TrimSpace(input.Actor)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Visibility != "private" && input.Visibility != "project_shared" {
		return VisibilityReceipt{}, fmt.Errorf("visibility must be private or project_shared")
	}
	if input.ExportPolicy != "deny" && input.ExportPolicy != "explicit_selection" {
		return VisibilityReceipt{}, fmt.Errorf("export_policy must be deny or explicit_selection")
	}
	if input.ExpectedRevision < 1 || input.Actor == "" || input.Reason == "" {
		return VisibilityReceipt{}, fmt.Errorf("expected_revision, actor and reason are required")
	}
	table, projectPredicate := "", ""
	switch input.ObjectKind {
	case "project":
		table, projectPredicate = "projects", "1=1"
	case "artifact":
		table, projectPredicate = "artifacts", "project_id IS NOT NULL"
	case "memory":
		table, projectPredicate = "memories", "scope_kind='project' AND scope_ref<>''"
	case "task":
		table, projectPredicate = "durable_tasks", "project_id<>''"
	case "skill":
		table, projectPredicate = "skills", "1=1"
	default:
		return VisibilityReceipt{}, fmt.Errorf("unsupported share object kind %q", input.ObjectKind)
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return VisibilityReceipt{}, err
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return VisibilityReceipt{}, err
	}
	defer tx.Rollback()
	var currentVisibility string
	var revision int
	query := fmt.Sprintf("SELECT visibility,sharing_revision FROM %s WHERE id=? AND owner_principal_id=? AND (%s)", table, projectPredicate)
	if err = tx.QueryRowContext(ctx, query, input.ObjectID, ownerID).Scan(&currentVisibility, &revision); err != nil {
		if err == sql.ErrNoRows {
			return VisibilityReceipt{}, fmt.Errorf("share object is unavailable or has no project scope")
		}
		return VisibilityReceipt{}, err
	}
	if revision != input.ExpectedRevision {
		return VisibilityReceipt{}, fmt.Errorf("stale sharing revision")
	}
	now := time.Now().UTC()
	update := fmt.Sprintf("UPDATE %s SET visibility=?,export_policy=?,sharing_revision=sharing_revision+1 WHERE id=? AND owner_principal_id=? AND sharing_revision=?", table)
	result, err := tx.ExecContext(ctx, update, input.Visibility, input.ExportPolicy, input.ObjectID, ownerID, revision)
	if err != nil {
		return VisibilityReceipt{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return VisibilityReceipt{}, fmt.Errorf("stale sharing revision")
	}
	auditID := identity.New("shareaudit")
	if _, err = tx.ExecContext(ctx, `INSERT INTO share_policy_audits(id,principal_id,object_kind,object_id,from_visibility,to_visibility,
		expected_revision,actor,reason,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, auditID, ownerID, input.ObjectKind,
		input.ObjectID, currentVisibility, input.Visibility, revision, input.Actor, input.Reason, formatTime(now)); err != nil {
		return VisibilityReceipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return VisibilityReceipt{}, err
	}
	return VisibilityReceipt{AuditID: auditID, ObjectKind: input.ObjectKind, ObjectID: input.ObjectID,
		Visibility: input.Visibility, ExportPolicy: input.ExportPolicy, Revision: revision + 1, Actor: input.Actor, CreatedAt: now}, nil
}

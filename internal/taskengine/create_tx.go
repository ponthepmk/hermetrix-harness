package taskengine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

// CreatedTask identifies a task inserted in a caller-owned transaction.
// The caller must commit before exposing the task as durable.
type CreatedTask struct {
	TaskID              string
	RequirementID       string
	TaskRevision        int
	RequirementRevision int
}

// CreateInTx applies the same validation and inserts as Create without taking
// ownership of the transaction. It exists so an ingress adapter can atomically
// bind an external identity to the new local task.
func (s *Service) CreateInTx(ctx context.Context, tx *sql.Tx, input CreateTaskInput) (CreatedTask, error) {
	if tx == nil {
		return CreatedTask{}, fmt.Errorf("task creation transaction is required")
	}
	if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Objective) == "" || strings.TrimSpace(input.OriginalRequest) == "" {
		return CreatedTask{}, fmt.Errorf("task title, objective and original request are required")
	}
	if strings.TrimSpace(input.Actor) == "" {
		return CreatedTask{}, fmt.Errorf("task actor is required")
	}
	if err := validateCriteria(input.Criteria); err != nil {
		return CreatedTask{}, err
	}
	egressPolicy := strings.TrimSpace(input.EgressPolicy)
	if egressPolicy == "" {
		egressPolicy = "local_only"
	}
	if egressPolicy != "local_only" && egressPolicy != "remote_allowed" {
		return CreatedTask{}, fmt.Errorf("egress_policy must be local_only or remote_allowed")
	}
	if egressPolicy == "remote_allowed" && (input.RemoteEgressApproval == nil ||
		strings.TrimSpace(input.RemoteEgressApproval.Actor) == "" || strings.TrimSpace(input.RemoteEgressApproval.Reason) == "") {
		return CreatedTask{}, fmt.Errorf("remote_allowed requires explicit egress approval with actor and reason")
	}
	var ownerID string
	principalID := identity.Principal(ctx)
	ownerQuery := `SELECT id FROM local_principals WHERE kind='local' ORDER BY created_at LIMIT 1`
	var ownerArgs []any
	if principalID != "" {
		ownerQuery = `SELECT id FROM local_principals WHERE id=?`
		ownerArgs = []any{principalID}
	}
	if err := tx.QueryRowContext(ctx, ownerQuery, ownerArgs...).Scan(&ownerID); err != nil {
		return CreatedTask{}, fmt.Errorf("resolve task owner: %w", err)
	}
	if strings.TrimSpace(input.ProjectID) != "" {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id=? AND owner_principal_id=?`, input.ProjectID, ownerID).Scan(&exists); err != nil {
			return CreatedTask{}, fmt.Errorf("task project is unavailable to this principal")
		}
	}
	now := time.Now().UTC()
	created := CreatedTask{TaskID: identity.New("task"), RequirementID: identity.New("reqrev"), TaskRevision: 1, RequirementRevision: 1}
	if _, err := tx.ExecContext(ctx, `INSERT INTO durable_tasks
		(id,project_id,title,objective,original_request,state,created_at,updated_at,owner_principal_id,egress_policy)
		VALUES(?,?,?,?,?,'draft',?,?,?,?)`, created.TaskID, nullable(input.ProjectID), strings.TrimSpace(input.Title),
		strings.TrimSpace(input.Objective), input.OriginalRequest, formatTime(now), formatTime(now), ownerID, egressPolicy); err != nil {
		return CreatedTask{}, err
	}
	constraints, _ := json.Marshal(cleanStrings(input.Constraints))
	unknowns, _ := json.Marshal(cleanStrings(input.Unknowns))
	criteria, _ := json.Marshal(input.Criteria)
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_requirement_revisions
		(id,task_id,revision,constraints_json,unknowns_json,criteria_json,actor,created_at)
		VALUES(?,?,1,?,?,?,?,?)`, created.RequirementID, created.TaskID, string(constraints), string(unknowns), string(criteria),
		strings.TrimSpace(input.Actor), formatTime(now)); err != nil {
		return CreatedTask{}, err
	}
	return created, nil
}

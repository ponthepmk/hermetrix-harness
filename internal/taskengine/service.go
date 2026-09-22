package taskengine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/store"
)

var (
	ErrNotFound      = errors.New("task not found")
	ErrStaleRevision = errors.New("stale task revision")
)

type Service struct{ store *store.Store }

func NewService(dataStore *store.Store) *Service { return &Service{store: dataStore} }

func (s *Service) Create(ctx context.Context, input CreateTaskInput) (Task, error) {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	created, err := s.CreateInTx(ctx, tx, input)
	if err != nil {
		return Task{}, err
	}
	if err = tx.Commit(); err != nil {
		return Task{}, err
	}
	return s.Get(ctx, created.TaskID)
}

// ReviseRequirements appends an immutable requirement revision and invalidates
// the active plan. Existing plans and evidence remain inspectable but cannot
// close work against a contract that superseded them.
func (s *Service) ReviseRequirements(ctx context.Context, input ReviseRequirementsInput) (Task, error) {
	if strings.TrimSpace(input.Actor) == "" {
		return Task{}, fmt.Errorf("requirement revision actor is required")
	}
	if err := validateCriteria(input.Criteria); err != nil {
		return Task{}, err
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	var taskRevision, activeRequirement int
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT revision,active_requirement_revision,state FROM durable_tasks WHERE id=?`, input.TaskID).
		Scan(&taskRevision, &activeRequirement, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, err
	}
	if taskRevision != input.ExpectedTaskRevision {
		return Task{}, ErrStaleRevision
	}
	if state == StateCompleted || state == StateCancelled {
		return Task{}, fmt.Errorf("cannot revise terminal task in state %s", state)
	}
	var supersedesID string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM task_requirement_revisions WHERE task_id=? AND revision=?`, input.TaskID, activeRequirement).Scan(&supersedesID); err != nil {
		return Task{}, err
	}
	nextRevision := activeRequirement + 1
	now := time.Now().UTC()
	constraints, _ := json.Marshal(cleanStrings(input.Constraints))
	unknowns, _ := json.Marshal(cleanStrings(input.Unknowns))
	criteria, _ := json.Marshal(input.Criteria)
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_requirement_revisions
		(id,task_id,revision,constraints_json,unknowns_json,criteria_json,supersedes_id,actor,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, identity.New("reqrev"), input.TaskID, nextRevision, string(constraints),
		string(unknowns), string(criteria), supersedesID, strings.TrimSpace(input.Actor), formatTime(now)); err != nil {
		return Task{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE durable_tasks SET active_requirement_revision=?,active_plan_revision=0,state='draft',
		pause_reason='requirements changed; create a new plan',revision=revision+1,updated_at=? WHERE id=? AND revision=?`,
		nextRevision, formatTime(now), input.TaskID, input.ExpectedTaskRevision)
	if err != nil {
		return Task{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Task{}, ErrStaleRevision
	}
	if err = tx.Commit(); err != nil {
		return Task{}, err
	}
	return s.Get(ctx, input.TaskID)
}

func (s *Service) CreatePlan(ctx context.Context, input CreatePlanInput) (Task, error) {
	if len(input.Steps) == 0 || strings.TrimSpace(input.Actor) == "" || strings.TrimSpace(input.Reason) == "" {
		return Task{}, fmt.Errorf("plan requires steps, actor and reason")
	}
	if err := validateSteps(input.Steps); err != nil {
		return Task{}, err
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	var taskRevision, activeRequirement, nextPlan int
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT revision,active_requirement_revision,active_plan_revision,state
		FROM durable_tasks WHERE id=?`, input.TaskID).Scan(&taskRevision, &activeRequirement, &nextPlan, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, err
	}
	if taskRevision != input.ExpectedTaskRevision {
		return Task{}, ErrStaleRevision
	}
	if input.RequirementRevision != activeRequirement {
		return Task{}, fmt.Errorf("plan requirement revision %d is not active revision %d", input.RequirementRevision, activeRequirement)
	}
	if state == StateCompleted || state == StateCancelled {
		return Task{}, fmt.Errorf("cannot replan terminal task in state %s", state)
	}
	var criteriaJSON string
	if err = tx.QueryRowContext(ctx, `SELECT criteria_json FROM task_requirement_revisions WHERE task_id=? AND revision=?`, input.TaskID, activeRequirement).Scan(&criteriaJSON); err != nil {
		return Task{}, err
	}
	var criteria []Criterion
	if err = json.Unmarshal([]byte(criteriaJSON), &criteria); err != nil {
		return Task{}, fmt.Errorf("decode active acceptance criteria: %w", err)
	}
	criterionIDs := map[string]bool{}
	for _, criterion := range criteria {
		criterionIDs[criterion.ID] = true
	}
	for _, spec := range input.Steps {
		for _, requirementID := range cleanStrings(spec.RequirementIDs) {
			if !criterionIDs[requirementID] {
				return Task{}, fmt.Errorf("step %s references unknown acceptance criterion %s", spec.Key, requirementID)
			}
		}
	}
	nextPlan++
	now := time.Now().UTC()
	planID := identity.New("planrev")
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_plan_revisions
		(id,task_id,revision,requirement_revision,reason,actor,created_at) VALUES(?,?,?,?,?,?,?)`,
		planID, input.TaskID, nextPlan, input.RequirementRevision, strings.TrimSpace(input.Reason), strings.TrimSpace(input.Actor), formatTime(now)); err != nil {
		return Task{}, err
	}
	for index, spec := range input.Steps {
		requirements, _ := json.Marshal(cleanStrings(spec.RequirementIDs))
		dependencies, _ := json.Marshal(cleanStrings(spec.Dependencies))
		checks, _ := json.Marshal(cleanStrings(spec.Checks))
		effects, _ := json.Marshal(cleanStrings(spec.EffectScope))
		if _, err = tx.ExecContext(ctx, `INSERT INTO task_steps
			(id,task_id,plan_revision,step_key,title,instructions,requirement_ids_json,dependencies_json,checks_json,effect_scope_json,state,sort_order,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,'pending',?,?)`, identity.New("taskstep"), input.TaskID, nextPlan,
			spec.Key, spec.Title, spec.Instructions, string(requirements), string(dependencies), string(checks), string(effects), index, formatTime(now)); err != nil {
			return Task{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE durable_tasks SET active_plan_revision=?,state='ready',pause_reason='',revision=revision+1,updated_at=?
		WHERE id=? AND revision=?`, nextPlan, formatTime(now), input.TaskID, input.ExpectedTaskRevision)
	if err != nil {
		return Task{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Task{}, ErrStaleRevision
	}
	if err = tx.Commit(); err != nil {
		return Task{}, err
	}
	return s.Get(ctx, input.TaskID)
}

func (s *Service) StartStep(ctx context.Context, taskID, stepKey string, expectedTaskRevision, expectedStepRevision int) (Task, error) {
	return s.transitionStep(ctx, taskID, stepKey, expectedTaskRevision, expectedStepRevision, StepRunning, "", false)
}

func (s *Service) CompleteStep(ctx context.Context, taskID, stepKey string, expectedTaskRevision, expectedStepRevision int) (Task, error) {
	return s.transitionStep(ctx, taskID, stepKey, expectedTaskRevision, expectedStepRevision, StepCompleted, "", true)
}

func (s *Service) FailStep(ctx context.Context, taskID, stepKey string, expectedTaskRevision, expectedStepRevision int, reason string) (Task, error) {
	if strings.TrimSpace(reason) == "" {
		return Task{}, fmt.Errorf("failure reason is required")
	}
	return s.transitionStep(ctx, taskID, stepKey, expectedTaskRevision, expectedStepRevision, StepFailed, reason, false)
}

func (s *Service) transitionStep(ctx context.Context, taskID, stepKey string, expectedTaskRevision, expectedStepRevision int, next, failure string, enforceChecks bool) (Task, error) {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	var taskRevision, planRevision int
	var taskState string
	if err = tx.QueryRowContext(ctx, `SELECT revision,active_plan_revision,state FROM durable_tasks WHERE id=?`, taskID).Scan(&taskRevision, &planRevision, &taskState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, err
	}
	if taskRevision != expectedTaskRevision {
		return Task{}, ErrStaleRevision
	}
	if planRevision == 0 {
		return Task{}, fmt.Errorf("task has no active plan")
	}
	var stepID, state, dependenciesJSON, checksJSON string
	var stepRevision int
	if err = tx.QueryRowContext(ctx, `SELECT id,state,revision,dependencies_json,checks_json FROM task_steps
		WHERE task_id=? AND plan_revision=? AND step_key=?`, taskID, planRevision, stepKey).
		Scan(&stepID, &state, &stepRevision, &dependenciesJSON, &checksJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, fmt.Errorf("step %q not found", stepKey)
		}
		return Task{}, err
	}
	if stepRevision != expectedStepRevision {
		return Task{}, fmt.Errorf("stale step revision")
	}
	if next == StepRunning {
		if state != StepPending && state != StepBlocked && state != StepFailed {
			return Task{}, fmt.Errorf("step %s cannot start from %s", stepKey, state)
		}
		var dependencies []string
		_ = json.Unmarshal([]byte(dependenciesJSON), &dependencies)
		for _, dependency := range dependencies {
			var dependencyState string
			if err = tx.QueryRowContext(ctx, `SELECT state FROM task_steps WHERE task_id=? AND plan_revision=? AND step_key=?`, taskID, planRevision, dependency).Scan(&dependencyState); err != nil || dependencyState != StepCompleted {
				return Task{}, fmt.Errorf("dependency %s is not complete", dependency)
			}
		}
	} else if next == StepCompleted {
		if state != StepRunning {
			return Task{}, fmt.Errorf("step %s cannot complete from %s", stepKey, state)
		}
		if enforceChecks {
			var checks []string
			_ = json.Unmarshal([]byte(checksJSON), &checks)
			subjectRevision := fmt.Sprintf("step:%s:r%d", stepID, stepRevision)
			for _, check := range checks {
				var count int
				if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_validations WHERE task_id=? AND step_id=? AND check_id=? AND subject_revision=? AND status='pass'`, taskID, stepID, check, subjectRevision).Scan(&count); err != nil || count == 0 {
					return Task{}, fmt.Errorf("step check %s has no passing validation", check)
				}
			}
		}
	}
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE task_steps SET state=?,revision=revision+1,last_error=?,updated_at=? WHERE id=? AND revision=?`, next, failure, formatTime(now), stepID, expectedStepRevision)
	if err != nil {
		return Task{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Task{}, fmt.Errorf("stale step revision")
	}
	nextTaskState := StateRunning
	if next == StepCompleted {
		var remaining int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_steps WHERE task_id=? AND plan_revision=? AND state NOT IN ('completed','skipped')`, taskID, planRevision).Scan(&remaining); err != nil {
			return Task{}, err
		}
		if remaining == 0 {
			nextTaskState = StateVerifying
		}
	} else if next == StepFailed {
		nextTaskState = StatePaused
	}
	result, err = tx.ExecContext(ctx, `UPDATE durable_tasks SET state=?,pause_reason=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, nextTaskState, failure, formatTime(now), taskID, expectedTaskRevision)
	if err != nil {
		return Task{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Task{}, ErrStaleRevision
	}
	if err = tx.Commit(); err != nil {
		return Task{}, err
	}
	return s.Get(ctx, taskID)
}

func (s *Service) RecordValidation(ctx context.Context, item Validation) (Validation, error) {
	if strings.TrimSpace(item.TaskID) == "" || strings.TrimSpace(item.CheckID) == "" || strings.TrimSpace(item.SubjectRevision) == "" {
		return Validation{}, fmt.Errorf("validation task, check and subject revision are required")
	}
	if item.Status != ValidationPass && item.Status != ValidationFail && item.Status != ValidationBlocked && item.Status != ValidationUnknown {
		return Validation{}, fmt.Errorf("invalid validation status %q", item.Status)
	}
	if item.Status == ValidationPass && len(cleanStrings(item.EvidenceRefs)) == 0 {
		return Validation{}, fmt.Errorf("passing validation requires evidence")
	}
	if item.StepID != "" {
		var count int
		if err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_steps WHERE id=? AND task_id=?`, item.StepID, item.TaskID).Scan(&count); err != nil || count != 1 {
			return Validation{}, fmt.Errorf("validation step does not belong to task")
		}
	}
	if item.RequirementID != "" {
		task, err := s.Get(ctx, item.TaskID)
		if err != nil {
			return Validation{}, err
		}
		found := false
		for _, criterion := range task.Requirement.Criteria {
			if criterion.ID == item.RequirementID {
				found = true
				break
			}
		}
		if !found {
			return Validation{}, fmt.Errorf("validation requirement is not active for task")
		}
	}
	item.ID, item.EvidenceRefs, item.CreatedAt = identity.New("validation"), cleanStrings(item.EvidenceRefs), time.Now().UTC()
	evidence, _ := json.Marshal(item.EvidenceRefs)
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO task_validations
		(id,task_id,step_id,requirement_id,check_id,subject_revision,status,expected,actual,evidence_refs_json,severity,confidence,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.TaskID, item.StepID, item.RequirementID, item.CheckID,
		item.SubjectRevision, item.Status, item.Expected, item.Actual, string(evidence), item.Severity, item.Confidence, formatTime(item.CreatedAt))
	return item, err
}

func (s *Service) CompleteTask(ctx context.Context, taskID string, expectedRevision int) (Task, error) {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	if task.Revision != expectedRevision {
		return Task{}, ErrStaleRevision
	}
	if task.State != StateVerifying {
		return Task{}, fmt.Errorf("task cannot complete from state %s", task.State)
	}
	if task.ActivePlanRevision == 0 {
		return Task{}, fmt.Errorf("task has no active plan")
	}
	for _, step := range task.Plan.Steps {
		if step.State != StepCompleted && step.State != StepSkipped {
			return Task{}, fmt.Errorf("step %s is %s", step.Key, step.State)
		}
	}
	for _, criterion := range task.Requirement.Criteria {
		var count int
		if err = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_validations WHERE task_id=? AND requirement_id=? AND subject_revision=? AND status='pass'`, taskID, criterion.ID, RequirementSubjectRevision(task)).Scan(&count); err != nil || count == 0 {
			return Task{}, fmt.Errorf("acceptance criterion %s has no passing validation", criterion.ID)
		}
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE durable_tasks SET state='completed',pause_reason='',revision=revision+1,updated_at=? WHERE id=? AND revision=?`, formatTime(time.Now().UTC()), taskID, expectedRevision)
	if err != nil {
		return Task{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Task{}, ErrStaleRevision
	}
	return s.Get(ctx, taskID)
}

func (s *Service) Checkpoint(ctx context.Context, input CheckpointInput) (Checkpoint, error) {
	task, err := s.Get(ctx, input.TaskID)
	if err != nil {
		return Checkpoint{}, err
	}
	if task.Revision != input.ExpectedTaskRevision {
		return Checkpoint{}, ErrStaleRevision
	}
	return s.writeCheckpoint(ctx, task, input)
}

func (s *Service) writeCheckpoint(ctx context.Context, task Task, input CheckpointInput) (Checkpoint, error) {
	item := Checkpoint{ID: identity.New("checkpoint"), TaskID: task.ID, PlanRevision: task.ActivePlanRevision,
		TaskRevision: task.Revision, EvidenceRefs: cleanStrings(input.EvidenceRefs), UnresolvedEffects: cleanStrings(input.UnresolvedEffects),
		NextAction: strings.TrimSpace(input.NextAction), ResumePrerequisites: cleanStrings(input.ResumePrerequisites), Reason: strings.TrimSpace(input.Reason), CreatedAt: time.Now().UTC()}
	for _, step := range task.Plan.Steps {
		if step.State == StepCompleted || step.State == StepSkipped {
			item.CompletedSteps = append(item.CompletedSteps, step.Key)
		} else {
			item.PendingSteps = append(item.PendingSteps, step.Key)
		}
	}
	completed, _ := json.Marshal(item.CompletedSteps)
	pending, _ := json.Marshal(item.PendingSteps)
	evidence, _ := json.Marshal(item.EvidenceRefs)
	unresolved, _ := json.Marshal(item.UnresolvedEffects)
	prerequisites, _ := json.Marshal(item.ResumePrerequisites)
	result, err := s.store.DB.ExecContext(ctx, `INSERT INTO task_checkpoints
		(id,task_id,plan_revision,task_revision,completed_steps_json,pending_steps_json,evidence_refs_json,unresolved_effects_json,next_action,resume_prerequisites_json,reason,created_at)
		SELECT ?,id,active_plan_revision,revision,?,?,?,?,?,?,?,? FROM durable_tasks WHERE id=? AND revision=?`,
		item.ID, string(completed), string(pending), string(evidence), string(unresolved), item.NextAction,
		string(prerequisites), item.Reason, formatTime(item.CreatedAt), item.TaskID, item.TaskRevision)
	if err != nil {
		return item, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return item, ErrStaleRevision
	}
	return item, nil
}

// RecoverInterrupted never retries a model/tool effect. It pauses active tasks
// and records an explicit checkpoint so a later owner must reconcile evidence
// and uncertain effects before continuing.
func (s *Service) RecoverInterrupted(ctx context.Context) (int, error) {
	now := formatTime(time.Now().UTC())
	if _, err := s.store.DB.ExecContext(ctx, `UPDATE task_planner_runs SET state='abandoned',error='process interrupted before provider dispatch',updated_at=? WHERE state='planned'`, now); err != nil {
		return 0, err
	}
	if _, err := s.store.DB.ExecContext(ctx, `UPDATE task_planner_runs SET state='uncertain',error='process interrupted after provider dispatch; response was not observed and was not retried',updated_at=? WHERE state='dispatched'`, now); err != nil {
		return 0, err
	}
	if _, err := s.store.DB.ExecContext(ctx, `UPDATE task_effect_intents SET state='abandoned',error='process interrupted before dispatch; intent was not executed',updated_at=? WHERE state='planned'`, now); err != nil {
		return 0, err
	}
	if _, err := s.store.DB.ExecContext(ctx, `UPDATE task_effect_intents SET state='uncertain',error='process interrupted after dispatch; inspect target before retry',updated_at=? WHERE state='dispatched'`, now); err != nil {
		return 0, err
	}
	if _, err := s.store.DB.ExecContext(ctx, `UPDATE task_steps SET state='blocked',revision=revision+1,
		last_error='process interrupted; reconcile effects before retry',updated_at=? WHERE id IN
		(SELECT step_id FROM task_step_attempts WHERE state='running')`, now); err != nil {
		return 0, err
	}
	if _, err := s.store.DB.ExecContext(ctx, `UPDATE task_step_attempts SET state='interrupted',error='process interrupted; effects were not replayed',completed_at=? WHERE state='running'`, now); err != nil {
		return 0, err
	}
	if _, err := s.store.DB.ExecContext(ctx, `UPDATE task_runs SET state='paused',stop_reason='process interrupted; reconcile effects before resume',updated_at=? WHERE state='running'`, now); err != nil {
		return 0, err
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id FROM durable_tasks WHERE state IN ('running','verifying') ORDER BY created_at`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		task, getErr := s.Get(ctx, id)
		if getErr != nil {
			return 0, getErr
		}
		now := time.Now().UTC()
		result, updateErr := s.store.DB.ExecContext(ctx, `UPDATE durable_tasks SET state='paused',pause_reason='process interrupted; reconcile effects before resume',revision=revision+1,updated_at=? WHERE id=? AND revision=?`, formatTime(now), id, task.Revision)
		if updateErr != nil {
			return 0, updateErr
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return 0, ErrStaleRevision
		}
		paused, getErr := s.Get(ctx, id)
		if getErr != nil {
			return 0, getErr
		}
		if _, checkpointErr := s.writeCheckpoint(ctx, paused, CheckpointInput{TaskID: id, ExpectedTaskRevision: paused.Revision,
			NextAction: "inspect the last attempt and reconcile every uncertain effect before resuming", ResumePrerequisites: []string{"effect reconciliation"}, Reason: "process interruption"}); checkpointErr != nil {
			return 0, checkpointErr
		}
	}
	return len(ids), nil
}

func (s *Service) Get(ctx context.Context, id string) (Task, error) {
	var item Task
	var project sql.NullString
	var created, updated string
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return Task{}, err
	}
	err = s.store.DB.QueryRowContext(ctx, `SELECT id,project_id,title,objective,original_request,state,active_requirement_revision,active_plan_revision,revision,pause_reason,created_at,updated_at,
		owner_principal_id,visibility,export_policy,sharing_revision,egress_policy FROM durable_tasks WHERE id=? AND owner_principal_id=?`, id, ownerID).
		Scan(&item.ID, &project, &item.Title, &item.Objective, &item.OriginalRequest, &item.State, &item.ActiveRequirementRevision, &item.ActivePlanRevision, &item.Revision, &item.PauseReason, &created, &updated,
			&item.OwnerPrincipalID, &item.Visibility, &item.ExportPolicy, &item.SharingRevision, &item.EgressPolicy)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	item.ProjectID = project.String
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	item.Requirement, err = s.requirement(ctx, id, item.ActiveRequirementRevision)
	if err != nil {
		return Task{}, err
	}
	if item.ActivePlanRevision > 0 {
		item.Plan, err = s.plan(ctx, id, item.ActivePlanRevision)
		if err != nil {
			return Task{}, err
		}
	}
	return item, nil
}

func (s *Service) List(ctx context.Context, projectID string, limit int) ([]Task, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT id FROM durable_tasks WHERE owner_principal_id=?`
	args := []any{ownerID}
	if strings.TrimSpace(projectID) != "" {
		query += ` AND project_id=?`
		args = append(args, strings.TrimSpace(projectID))
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.store.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	items := make([]Task, 0, len(ids))
	for _, id := range ids {
		item, getErr := s.Get(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) requirement(ctx context.Context, taskID string, revision int) (RequirementRevision, error) {
	var item RequirementRevision
	var constraints, unknowns, criteria, created string
	err := s.store.DB.QueryRowContext(ctx, `SELECT id,task_id,revision,constraints_json,unknowns_json,criteria_json,supersedes_id,actor,created_at FROM task_requirement_revisions WHERE task_id=? AND revision=?`, taskID, revision).
		Scan(&item.ID, &item.TaskID, &item.Revision, &constraints, &unknowns, &criteria, &item.SupersedesID, &item.Actor, &created)
	if err != nil {
		return item, err
	}
	_ = json.Unmarshal([]byte(constraints), &item.Constraints)
	_ = json.Unmarshal([]byte(unknowns), &item.Unknowns)
	_ = json.Unmarshal([]byte(criteria), &item.Criteria)
	item.CreatedAt, _ = parseTime(created)
	return item, nil
}

func (s *Service) plan(ctx context.Context, taskID string, revision int) (PlanRevision, error) {
	var item PlanRevision
	var created string
	err := s.store.DB.QueryRowContext(ctx, `SELECT id,task_id,revision,requirement_revision,reason,actor,created_at FROM task_plan_revisions WHERE task_id=? AND revision=?`, taskID, revision).
		Scan(&item.ID, &item.TaskID, &item.Revision, &item.RequirementRevision, &item.Reason, &item.Actor, &created)
	if err != nil {
		return item, err
	}
	item.CreatedAt, _ = parseTime(created)
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,step_key,title,instructions,requirement_ids_json,dependencies_json,checks_json,effect_scope_json,state,revision,sort_order,last_error,updated_at FROM task_steps WHERE task_id=? AND plan_revision=? ORDER BY sort_order`, taskID, revision)
	if err != nil {
		return item, err
	}
	defer rows.Close()
	for rows.Next() {
		var step Step
		var requirements, dependencies, checks, effects, updated string
		step.TaskID, step.PlanRevision = taskID, revision
		if err = rows.Scan(&step.ID, &step.Key, &step.Title, &step.Instructions, &requirements, &dependencies, &checks, &effects, &step.State, &step.Revision, &step.SortOrder, &step.LastError, &updated); err != nil {
			return item, err
		}
		_ = json.Unmarshal([]byte(requirements), &step.RequirementIDs)
		_ = json.Unmarshal([]byte(dependencies), &step.Dependencies)
		_ = json.Unmarshal([]byte(checks), &step.Checks)
		_ = json.Unmarshal([]byte(effects), &step.EffectScope)
		step.UpdatedAt, _ = parseTime(updated)
		item.Steps = append(item.Steps, step)
	}
	return item, rows.Err()
}

func validateCriteria(items []Criterion) error {
	if len(items) == 0 {
		return fmt.Errorf("at least one acceptance criterion is required")
	}
	seen := map[string]bool{}
	for _, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" || strings.TrimSpace(item.Description) == "" {
			return fmt.Errorf("acceptance criteria require stable ids and descriptions")
		}
		if seen[item.ID] {
			return fmt.Errorf("duplicate acceptance criterion %s", item.ID)
		}
		seen[item.ID] = true
	}
	return nil
}

func validateSteps(items []StepSpec) error {
	seen := map[string]bool{}
	for _, item := range items {
		if strings.TrimSpace(item.Key) == "" || strings.TrimSpace(item.Title) == "" || strings.TrimSpace(item.Instructions) == "" {
			return fmt.Errorf("steps require key, title and instructions")
		}
		if seen[item.Key] {
			return fmt.Errorf("duplicate step %s", item.Key)
		}
		seen[item.Key] = true
	}
	for _, item := range items {
		for _, dependency := range item.Dependencies {
			if dependency == item.Key || !seen[dependency] {
				return fmt.Errorf("step %s has invalid dependency %s", item.Key, dependency)
			}
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	dependencies := map[string][]string{}
	for _, item := range items {
		dependencies[item.Key] = cleanStrings(item.Dependencies)
	}
	var visit func(string) error
	visit = func(key string) error {
		if visiting[key] {
			return fmt.Errorf("plan dependency cycle includes %s", key)
		}
		if visited[key] {
			return nil
		}
		visiting[key] = true
		for _, dependency := range dependencies[key] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		visiting[key], visited[key] = false, true
		return nil
	}
	for key := range dependencies {
		if err := visit(key); err != nil {
			return err
		}
	}
	return nil
}

func cleanStrings(items []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}

// StepSubjectRevision and RequirementSubjectRevision are the exact evidence
// bindings validation writers must use. A pass for an earlier edit or an old
// requirement revision cannot close the current task accidentally.
func StepSubjectRevision(step Step) string {
	return fmt.Sprintf("step:%s:r%d", step.ID, step.Revision)
}

func RequirementSubjectRevision(task Task) string {
	return fmt.Sprintf("requirement:%s:r%d", task.ID, task.ActiveRequirementRevision)
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}
func formatTime(value time.Time) string         { return value.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }

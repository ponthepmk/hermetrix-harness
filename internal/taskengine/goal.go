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

const (
	GoalActive          = "active"
	GoalWaiting         = "waiting"
	GoalCompleted       = "completed"
	GoalStopping        = "stopping"
	GoalStopped         = "stopped"
	GoalBudgetExhausted = "budget_exhausted"
)

// GoalConfig is frozen consent, not model-generated authority. Limits are
// cumulative across pauses and resumes. A new requirement needs a new run.
type GoalConfig struct {
	StartInputSHA256           string `json:"start_input_sha256,omitempty"`
	VerificationTimeoutSeconds int    `json:"verification_timeout_seconds"`
	ProjectRoot                string `json:"project_root,omitempty"`
	ProviderID                 string `json:"provider_id"`
	ProviderRevision           string `json:"provider_revision"`
	ReviewerProviderID         string `json:"reviewer_provider_id"`
	ReviewerProviderRevision   string `json:"reviewer_provider_revision"`
	ApplyChanges               bool   `json:"apply_changes"`
	MaxActions                 int    `json:"max_actions"`
	MaxAttempts                int    `json:"max_attempts"`
	MaxPlans                   int    `json:"max_plans"`
	MaxSeconds                 int    `json:"max_seconds"`
}

type GoalRun struct {
	ID                   string     `json:"id"`
	TaskID               string     `json:"task_id"`
	RequirementRevision  int        `json:"requirement_revision"`
	ExpectedTaskRevision int        `json:"expected_task_revision"`
	Actor                string     `json:"actor"`
	Config               GoalConfig `json:"config"`
	State                string     `json:"state"`
	Reason               string     `json:"reason,omitempty"`
	ActionsUsed          int        `json:"actions_used"`
	AttemptsUsed         int        `json:"attempts_used"`
	PlansUsed            int        `json:"plans_used"`
	ClaimToken           string     `json:"-"`
	Deadline             time.Time  `json:"deadline"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type GoalAction struct {
	ID           string         `json:"id"`
	GoalID       string         `json:"goal_id"`
	Ordinal      int            `json:"ordinal"`
	Action       string         `json:"action"`
	TaskRevision int            `json:"task_revision"`
	State        string         `json:"state"`
	Receipt      map[string]any `json:"receipt"`
	CreatedAt    time.Time      `json:"created_at"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
}

func (s *Service) createGoalAtID(ctx context.Context, goalID string, taskID string, expectedRevision int, actor string, config GoalConfig) (GoalRun, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" || len([]rune(actor)) > 200 || config.ProviderID == "" || config.ProviderRevision == "" ||
		config.ReviewerProviderID == "" || config.ReviewerProviderRevision == "" || config.ProviderID == config.ReviewerProviderID {
		return GoalRun{}, fmt.Errorf("goal requires a bounded actor and two distinct frozen provider profiles")
	}
	if config.MaxActions < 1 || config.MaxActions > 10000 || config.MaxAttempts < 1 || config.MaxAttempts > 128 ||
		config.MaxPlans < 1 || config.MaxPlans > 32 || config.MaxSeconds < 1 || config.MaxSeconds > 86400 || config.VerificationTimeoutSeconds < 0 || config.VerificationTimeoutSeconds > 86400 {
		return GoalRun{}, fmt.Errorf("goal limits out of range: actions 1–10000, attempts 1–128, plans 1–32, seconds 1–86400")
	}
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return GoalRun{}, err
	}
	if task.Revision != expectedRevision {
		return GoalRun{}, ErrStaleRevision
	}
	if task.State == StateCompleted || task.State == StateCancelled || task.State == StateFailed {
		return GoalRun{}, fmt.Errorf("cannot start a goal for a terminal task")
	}
	if config.ApplyChanges && len(task.Requirement.Contract.WritablePaths) == 0 {
		return GoalRun{}, fmt.Errorf("automatic apply requires explicit writable_paths in the goal contract")
	}
	now := time.Now().UTC()
	goal := GoalRun{ID: goalID, TaskID: taskID, RequirementRevision: task.ActiveRequirementRevision,
		ExpectedTaskRevision: expectedRevision, Actor: actor, Config: config, State: GoalActive,
		Deadline: now.Add(time.Duration(config.MaxSeconds) * time.Second), CreatedAt: now, UpdatedAt: now}
	encoded, err := json.Marshal(config)
	if err != nil {
		return GoalRun{}, err
	}
	result, err := s.store.DB.ExecContext(ctx, `INSERT INTO task_goal_runs
		(id,task_id,requirement_revision,expected_task_revision,actor,config_json,state,deadline,created_at,updated_at)
		SELECT ?,id,active_requirement_revision,revision,?,?, 'active',?,?,? FROM durable_tasks WHERE id=? AND revision=?`,
		goal.ID, actor, string(encoded), formatTime(goal.Deadline), formatTime(now), formatTime(now), taskID, expectedRevision)
	if err != nil {
		return GoalRun{}, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return GoalRun{}, ErrStaleRevision
	}
	return goal, nil
}

const goalColumns = `id,task_id,requirement_revision,expected_task_revision,actor,config_json,state,reason,
	actions_used,attempts_used,plans_used,claim_token,deadline,created_at,updated_at`

func scanGoal(row interface{ Scan(...any) error }) (GoalRun, error) {
	var g GoalRun
	var config, deadline, created, updated string
	err := row.Scan(&g.ID, &g.TaskID, &g.RequirementRevision, &g.ExpectedTaskRevision, &g.Actor, &config, &g.State, &g.Reason,
		&g.ActionsUsed, &g.AttemptsUsed, &g.PlansUsed, &g.ClaimToken, &deadline, &created, &updated)
	if err != nil {
		return g, err
	}
	if err = json.Unmarshal([]byte(config), &g.Config); err != nil {
		return g, err
	}
	g.Deadline, err = parseTime(deadline)
	if err != nil {
		return g, err
	}
	g.CreatedAt, _ = parseTime(created)
	g.UpdatedAt, _ = parseTime(updated)
	return g, nil
}

func (s *Service) Goal(ctx context.Context, id string) (GoalRun, error) {
	g, err := scanGoal(s.store.DB.QueryRowContext(ctx, `SELECT `+goalColumns+` FROM task_goal_runs WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return g, ErrNotFound
	}
	if err != nil {
		return g, err
	}
	_, err = s.Get(ctx, g.TaskID) // apply the ordinary task owner boundary
	return g, err
}

func (s *Service) GoalsForTask(ctx context.Context, taskID string) ([]GoalRun, error) {
	if _, err := s.Get(ctx, taskID); err != nil {
		return nil, err
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT `+goalColumns+` FROM task_goal_runs WHERE task_id=? ORDER BY created_at DESC,id DESC LIMIT 20`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []GoalRun{}
	for rows.Next() {
		g, scanErr := scanGoal(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, g)
	}
	return items, rows.Err()
}

// ReadyGoals is a bounded scheduler query. Claims still arbitrate dispatch.
func (s *Service) ReadyGoals(ctx context.Context, limit int) ([]string, error) {
	if limit < 1 || limit > 16 {
		limit = 2
	}
	owner, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT g.id FROM task_goal_runs g JOIN durable_tasks t ON t.id=g.task_id
		WHERE g.state='active' AND g.claim_token='' AND t.owner_principal_id=? AND (g.poll_after<=? OR g.deadline<=?) ORDER BY g.updated_at,g.id LIMIT ?`, owner, formatTime(time.Now().UTC()), formatTime(time.Now().UTC()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ClaimGoalAction commits an intent and charges budgets BEFORE dispatch. The
// claim is never stolen or expired into automatic replay after a crash.
func (s *Service) ClaimGoalAction(ctx context.Context, id, action string) (GoalRun, GoalAction, error) {
	g, err := s.Goal(ctx, id)
	if err != nil {
		return g, GoalAction{}, err
	}
	if g.State != GoalActive || g.ClaimToken != "" {
		return g, GoalAction{}, fmt.Errorf("goal is not idle and active")
	}
	task, err := s.Get(ctx, g.TaskID)
	if err != nil {
		return g, GoalAction{}, err
	}
	if task.Revision != g.ExpectedTaskRevision || task.ActiveRequirementRevision != g.RequirementRevision {
		return g, GoalAction{}, ErrStaleRevision
	}
	if g.ActionsUsed >= g.Config.MaxActions || action == "attempt" && g.AttemptsUsed >= g.Config.MaxAttempts ||
		action == "plan" && g.PlansUsed >= g.Config.MaxPlans || !g.Deadline.After(time.Now().UTC()) {
		_, err = s.store.DB.ExecContext(ctx, `UPDATE task_goal_runs SET state='budget_exhausted',reason='goal budget exhausted; objective remains unconfirmed',updated_at=? WHERE id=? AND state='active' AND claim_token=''`, formatTime(time.Now().UTC()), id)
		if err != nil {
			return g, GoalAction{}, err
		}
		return g, GoalAction{}, fmt.Errorf("goal budget exhausted")
	}
	allowed := map[string]bool{"plan": true, "begin_run": true, "attempt": true, "propose": true, "approve": true, "apply": true, "verify": true, "verify_collect": true, "review": true, "finish": true, "recover_review": true, "recover_approved_review": true}
	if !allowed[action] {
		return g, GoalAction{}, fmt.Errorf("unsupported goal action %q", action)
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return g, GoalAction{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	item := GoalAction{ID: identity.New("goalaction"), GoalID: id, Ordinal: g.ActionsUsed + 1, Action: action, TaskRevision: task.Revision, State: "dispatched", CreatedAt: now}
	result, err := tx.ExecContext(ctx, `UPDATE task_goal_runs SET claim_token=?,actions_used=actions_used+1,
		attempts_used=attempts_used+?,plans_used=plans_used+?,updated_at=? WHERE id=? AND state='active' AND claim_token=''
		AND actions_used=? AND expected_task_revision=? AND EXISTS(SELECT 1 FROM durable_tasks WHERE id=? AND revision=? AND active_requirement_revision=?)`,
		item.ID, boolInt(action == "attempt"), boolInt(action == "plan"), formatTime(now), id, g.ActionsUsed, task.Revision, task.ID, task.Revision, g.RequirementRevision)
	if err != nil {
		return g, item, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return g, item, ErrStaleRevision
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_goal_actions(id,goal_id,ordinal,action,task_revision,state,created_at) VALUES(?,?,?,?,?,'dispatched',?)`, item.ID, id, item.Ordinal, action, task.Revision, formatTime(now))
	if err != nil {
		return g, item, err
	}
	if err = tx.Commit(); err != nil {
		return g, item, err
	}
	g.ClaimToken = item.ID
	return g, item, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Service) FinishGoalAction(ctx context.Context, id, token, state, reason string, receipt map[string]any) (GoalRun, error) {
	g, err := s.Goal(ctx, id)
	if err != nil {
		return g, err
	}
	if token == "" || token != g.ClaimToken {
		return g, fmt.Errorf("goal action claim is stale")
	}
	if state != GoalActive && state != GoalWaiting && state != GoalCompleted && state != GoalStopped {
		return g, fmt.Errorf("invalid goal outcome")
	}
	task, err := s.Get(ctx, g.TaskID)
	if err != nil {
		return g, err
	}
	if g.State == GoalStopping {
		state, reason = GoalStopped, "stopped by owner"
	}
	if state != GoalCompleted && state != GoalStopped && !g.Deadline.After(time.Now().UTC()) {
		state, reason = GoalBudgetExhausted, "goal deadline reached; objective remains unconfirmed"
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return g, err
	}
	if len(encoded) > 32<<10 {
		return g, fmt.Errorf("goal receipt exceeds 32 KiB")
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return g, err
	}
	defer tx.Rollback()
	// Completion and its authoritative evidence must share the transaction.
	// A validation can be appended without changing the Task revision, so a
	// revision check alone cannot protect this boundary.
	var currentRevision, currentRequirement int
	var currentGoalState string
	if err = tx.QueryRowContext(ctx, `SELECT t.revision,t.active_requirement_revision,g.state
		FROM durable_tasks t JOIN task_goal_runs g ON g.task_id=t.id WHERE g.id=? AND g.claim_token=?`, id, token).
		Scan(&currentRevision, &currentRequirement, &currentGoalState); err != nil {
		return g, err
	}
	commitReceipt := make(map[string]any, len(receipt)+3)
	for key, value := range receipt {
		commitReceipt[key] = value
	}
	var fenceErr error
	if currentGoalState == GoalStopping {
		state, reason = GoalStopped, "stopped by owner"
		commitReceipt["outcome"] = "stopped"
	} else if currentRevision != task.Revision || currentRequirement != g.RequirementRevision {
		state, reason = GoalWaiting, "task or requirements changed before commit; inspect current authority"
		fenceErr = ErrStaleRevision
	} else if state == GoalCompleted {
		report, checkErr := completionReport(ctx, tx, task)
		if checkErr != nil {
			return g, checkErr
		}
		passing := report.Completed
		for _, gate := range report.Gates {
			passing = passing && gate.Status == ValidationPass
		}
		if !passing {
			state, reason = GoalWaiting, "completion evidence changed before commit; inspect current gates"
			fenceErr = ErrStaleRevision
		} else {
			commitReceipt["completion_subject_hash"] = report.SubjectHash
			commitReceipt["task_revision"] = task.Revision
			commitReceipt["requirement_revision"] = task.ActiveRequirementRevision
		}
	}
	if fenceErr != nil {
		commitReceipt["outcome"] = "requires_inspection"
	}
	if state != GoalCompleted {
		delete(commitReceipt, "completion_subject_hash")
	}
	encoded, err = json.Marshal(commitReceipt)
	if err != nil {
		return g, err
	}
	if len(encoded) > 32<<10 {
		return g, fmt.Errorf("goal receipt exceeds 32 KiB")
	}
	now := formatTime(time.Now().UTC())
	result, err := tx.ExecContext(ctx, `UPDATE task_goal_runs SET state=CASE WHEN state='stopping' THEN 'stopped' ELSE ? END,
		reason=CASE WHEN state='stopping' THEN 'stopped by owner' ELSE ? END,claim_token='',expected_task_revision=?,updated_at=? WHERE id=? AND claim_token=? AND state IN ('active','stopping')`, state, reason, currentRevision, now, id, token)
	if err != nil {
		return g, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return g, ErrStaleRevision
	}
	_, err = tx.ExecContext(ctx, `UPDATE task_goal_actions SET state='observed',receipt_json=?,completed_at=? WHERE id=? AND goal_id=? AND state='dispatched'`, string(encoded), now, token, id)
	if err != nil {
		return g, err
	}
	if err = tx.Commit(); err != nil {
		return g, err
	}
	finished, err := s.Goal(ctx, id)
	if err != nil {
		return finished, err
	}
	return finished, fenceErr
}

// WaitGoal can only stop dispatch at an idle boundary.
func (s *Service) WaitGoal(ctx context.Context, id, reason string) (GoalRun, error) {
	if _, err := s.Goal(ctx, id); err != nil {
		return GoalRun{}, err
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE task_goal_runs SET state='waiting',reason=?,updated_at=? WHERE id=? AND state='active' AND claim_token=''`, reason, formatTime(time.Now().UTC()), id)
	if err != nil {
		return GoalRun{}, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return GoalRun{}, ErrStaleRevision
	}
	return s.Goal(ctx, id)
}

func (s *Service) StopGoal(ctx context.Context, id string) (GoalRun, error) {
	if _, err := s.Goal(ctx, id); err != nil {
		return GoalRun{}, err
	}
	_, err := s.store.DB.ExecContext(ctx, `UPDATE task_goal_runs SET state=CASE WHEN claim_token='' THEN 'stopped' ELSE 'stopping' END,
		reason='stopped by owner',updated_at=? WHERE id=? AND state IN ('active','waiting','stopping')`, formatTime(time.Now().UTC()), id)
	if err != nil {
		return GoalRun{}, err
	}
	return s.Goal(ctx, id)
}

// InterruptGoalExecution relinquishes only this goal's run. Potentially
// dispatched effects stay uncertain, and an applied proposal is retained for
// inspection rather than silently discarded or replayed after cancellation.
func (s *Service) InterruptGoalExecution(ctx context.Context, id string) error {
	g, err := s.Goal(ctx, id)
	if err != nil {
		return err
	}
	if g.ClaimToken != "" || g.State == GoalActive {
		return fmt.Errorf("finish goal dispatch before interrupting execution")
	}
	task, err := s.Get(ctx, g.TaskID)
	if err != nil {
		return err
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var runID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM task_runs WHERE task_id=? AND owner=? AND state='running' ORDER BY created_at DESC LIMIT 1`, task.ID, g.ID).Scan(&runID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	now := formatTime(time.Now().UTC())
	reason := "goal interrupted; inspect retained proposal and effect evidence before resuming"
	for _, q := range []string{
		`UPDATE task_effect_intents SET state='abandoned',error='goal stopped before dispatch',updated_at=? WHERE run_id=? AND state='planned'`,
		`UPDATE task_effect_intents SET state='uncertain',error='goal interrupted after dispatch; reconcile before retry',updated_at=? WHERE run_id=? AND state='dispatched'`,
		`UPDATE task_steps SET state='blocked',revision=revision+1,last_error='goal interrupted; inspect evidence',updated_at=? WHERE id IN (SELECT step_id FROM task_step_attempts WHERE run_id=? AND state='running')`,
		`UPDATE task_step_attempts SET state='interrupted',error='goal interrupted; effects were not replayed',completed_at=? WHERE run_id=? AND state='running'`,
	} {
		if _, err = tx.ExecContext(ctx, q, now, runID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE task_runs SET state='paused',lease_generation=lease_generation+1,lease_expires_at=?,stop_reason=?,updated_at=? WHERE id=? AND state='running'`, now, reason, now, runID); err != nil {
		return err
	}
	if task.State == StateRunning {
		result, updateErr := tx.ExecContext(ctx, `UPDATE durable_tasks SET state='paused',pause_reason=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, reason, now, task.ID, task.Revision)
		if updateErr != nil {
			return updateErr
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return ErrStaleRevision
		}
	}
	return tx.Commit()
}

func (s *Service) ResumeGoal(ctx context.Context, id string, expectedRevision int) (GoalRun, error) {
	g, err := s.Goal(ctx, id)
	if err != nil {
		return g, err
	}
	task, err := s.Get(ctx, g.TaskID)
	if err != nil {
		return g, err
	}
	if task.Revision != expectedRevision {
		return g, ErrStaleRevision
	}
	if task.ActiveRequirementRevision != g.RequirementRevision {
		return g, fmt.Errorf("requirements changed; start a new goal")
	}
	if !g.Deadline.After(time.Now().UTC()) || g.ActionsUsed >= g.Config.MaxActions {
		return g, fmt.Errorf("goal budget exhausted")
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE task_goal_runs SET state='active',reason='',expected_task_revision=?,updated_at=? WHERE id=? AND state='waiting' AND claim_token=''`, task.Revision, formatTime(time.Now().UTC()), id)
	if err != nil {
		return g, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return g, fmt.Errorf("only an idle waiting goal can resume")
	}
	return s.Goal(ctx, id)
}

// RecoverGoals runs once during server startup, before workers start. An
// interrupted dispatch is retained for inspection, never automatically retried.
func (s *Service) RecoverGoals(ctx context.Context) error {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := formatTime(time.Now().UTC())
	if _, err = tx.ExecContext(ctx, `UPDATE task_goal_actions SET state='interrupted',completed_at=? WHERE state='dispatched'`, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE task_goal_runs SET state=CASE WHEN state='stopping' THEN 'stopped' ELSE 'waiting' END,
		reason='process interrupted; inspect task effects and saved evidence before resuming',claim_token='',updated_at=? WHERE claim_token<>'' OR (state='active' AND EXISTS(SELECT 1 FROM task_goal_verifications v WHERE v.goal_id=task_goal_runs.id AND v.state='active'))`, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE task_goal_verifications SET state='interrupted',updated_at=? WHERE state='active'`, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) GoalActions(ctx context.Context, id string, limit int) ([]GoalAction, error) {
	if _, err := s.Goal(ctx, id); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,goal_id,ordinal,action,task_revision,state,receipt_json,created_at,completed_at FROM task_goal_actions WHERE goal_id=? ORDER BY ordinal DESC LIMIT ?`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []GoalAction{}
	for rows.Next() {
		var a GoalAction
		var body, created string
		var completed sql.NullString
		if err = rows.Scan(&a.ID, &a.GoalID, &a.Ordinal, &a.Action, &a.TaskRevision, &a.State, &body, &created, &completed); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(body), &a.Receipt); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = parseTime(created)
		if completed.Valid {
			v, _ := parseTime(completed.String)
			a.CompletedAt = &v
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

package taskengine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

const (
	RunRunning   = "running"
	RunPaused    = "paused"
	RunCompleted = "completed"
	RunFailed    = "failed"

	AttemptRunning     = "running"
	AttemptCompleted   = "completed"
	AttemptFailed      = "failed"
	AttemptInterrupted = "interrupted"

	EffectPlanned    = "planned"
	EffectDispatched = "dispatched"
	EffectObserved   = "observed"
	EffectUncertain  = "uncertain"
	EffectReconciled = "reconciled"
	EffectAbandoned  = "abandoned"
)

type Run struct {
	ID                  string     `json:"id"`
	TaskID              string     `json:"task_id"`
	PlanRevision        int        `json:"plan_revision"`
	RequirementRevision int        `json:"requirement_revision"`
	State               string     `json:"state"`
	Owner               string     `json:"owner"`
	LeaseToken          string     `json:"lease_token,omitempty"`
	LeaseExpiresAt      time.Time  `json:"lease_expires_at"`
	StopReason          string     `json:"stop_reason,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
}

type StepAttempt struct {
	ID           string      `json:"id"`
	RunID        string      `json:"run_id"`
	TaskID       string      `json:"task_id"`
	StepID       string      `json:"step_id"`
	StepRevision int         `json:"step_revision"`
	State        string      `json:"state"`
	InputHash    string      `json:"input_hash"`
	Packet       *StepPacket `json:"packet,omitempty"`
	Output       string      `json:"output,omitempty"`
	Error        string      `json:"error,omitempty"`
	StartedAt    time.Time   `json:"started_at"`
	CompletedAt  *time.Time  `json:"completed_at,omitempty"`
}

type EffectIntent struct {
	ID          string         `json:"id"`
	AttemptID   string         `json:"attempt_id"`
	TaskID      string         `json:"task_id"`
	OperationID string         `json:"operation_id"`
	Action      string         `json:"action"`
	Target      string         `json:"target"`
	Authority   string         `json:"authority"`
	State       string         `json:"state"`
	Receipt     map[string]any `json:"receipt,omitempty"`
	Error       string         `json:"error,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// ExecutionSnapshot is the durable resume view for one task. It intentionally
// exposes the latest run/attempt/proposal plus every effect attached to that
// attempt; a client can reconstruct the next safe action after a UI or process
// restart without guessing from transient browser state.
type ExecutionSnapshot struct {
	Task     Task           `json:"task"`
	Run      *Run           `json:"run,omitempty"`
	Attempt  *StepAttempt   `json:"attempt,omitempty"`
	Proposal *CodeProposal  `json:"proposal,omitempty"`
	Effects  []EffectIntent `json:"effects"`
}

type BeginRunInput struct {
	TaskID               string        `json:"task_id"`
	ExpectedTaskRevision int           `json:"expected_task_revision"`
	Owner                string        `json:"owner"`
	LeaseDuration        time.Duration `json:"-"`
}

type BeginAttemptInput struct {
	RunID                string      `json:"run_id"`
	LeaseToken           string      `json:"lease_token"`
	StepKey              string      `json:"step_key"`
	ExpectedTaskRevision int         `json:"expected_task_revision"`
	ExpectedStepRevision int         `json:"expected_step_revision"`
	InputHash            string      `json:"input_hash"`
	Packet               *StepPacket `json:"packet,omitempty"`
}

func (s *Service) BeginRun(ctx context.Context, input BeginRunInput) (Run, error) {
	if strings.TrimSpace(input.Owner) == "" {
		return Run{}, fmt.Errorf("run owner is required")
	}
	duration := input.LeaseDuration
	if duration < 15*time.Second || duration > 15*time.Minute {
		duration = 2 * time.Minute
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback()
	var taskRevision, planRevision, requirementRevision int
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT revision,active_plan_revision,active_requirement_revision,state FROM durable_tasks WHERE id=?`, input.TaskID).
		Scan(&taskRevision, &planRevision, &requirementRevision, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, ErrNotFound
		}
		return Run{}, err
	}
	if taskRevision != input.ExpectedTaskRevision {
		return Run{}, ErrStaleRevision
	}
	if state != StateReady && state != StatePaused {
		return Run{}, fmt.Errorf("task cannot start from state %s", state)
	}
	if planRevision == 0 {
		return Run{}, fmt.Errorf("task has no active plan")
	}
	var uncertain int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_effect_intents WHERE task_id=? AND state='uncertain'`, input.TaskID).Scan(&uncertain); err != nil {
		return Run{}, err
	}
	if uncertain > 0 {
		return Run{}, fmt.Errorf("task has %d uncertain effects to reconcile", uncertain)
	}
	now := time.Now().UTC()
	run := Run{ID: identity.New("taskrun"), TaskID: input.TaskID, PlanRevision: planRevision,
		RequirementRevision: requirementRevision, State: RunRunning, Owner: strings.TrimSpace(input.Owner),
		LeaseToken: identity.New("lease"), LeaseExpiresAt: now.Add(duration), CreatedAt: now, UpdatedAt: now}
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_runs
		(id,task_id,plan_revision,requirement_revision,state,owner,lease_token,lease_expires_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, run.ID, run.TaskID, run.PlanRevision, run.RequirementRevision, run.State,
		run.Owner, run.LeaseToken, formatTime(run.LeaseExpiresAt), formatTime(now), formatTime(now)); err != nil {
		return Run{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE durable_tasks SET state='running',pause_reason='',revision=revision+1,updated_at=? WHERE id=? AND revision=?`, formatTime(now), input.TaskID, input.ExpectedTaskRevision)
	if err != nil {
		return Run{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Run{}, ErrStaleRevision
	}
	if err = tx.Commit(); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *Service) RenewRunLease(ctx context.Context, runID, leaseToken string, duration time.Duration) (Run, error) {
	if duration < 15*time.Second || duration > 15*time.Minute {
		duration = 2 * time.Minute
	}
	now, expires := time.Now().UTC(), time.Now().UTC().Add(duration)
	result, err := s.store.DB.ExecContext(ctx, `UPDATE task_runs SET lease_expires_at=?,updated_at=? WHERE id=? AND lease_token=? AND state='running' AND lease_expires_at>?`, formatTime(expires), formatTime(now), runID, leaseToken, formatTime(now))
	if err != nil {
		return Run{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Run{}, fmt.Errorf("run lease is stale or expired")
	}
	return s.getRun(ctx, runID)
}

func (s *Service) BeginStepAttempt(ctx context.Context, input BeginAttemptInput) (StepAttempt, error) {
	if strings.TrimSpace(input.InputHash) == "" {
		return StepAttempt{}, fmt.Errorf("attempt input hash is required")
	}
	packetJSON := ""
	if input.Packet != nil {
		if err := ValidateStepPacket(*input.Packet); err != nil {
			return StepAttempt{}, err
		}
		if input.Packet.CanonicalPacketHash != input.InputHash {
			return StepAttempt{}, fmt.Errorf("attempt packet does not match input hash")
		}
		encoded, err := json.Marshal(input.Packet)
		if err != nil {
			return StepAttempt{}, err
		}
		packetJSON = string(encoded)
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return StepAttempt{}, err
	}
	defer tx.Rollback()
	var taskID, runState, storedLease, leaseExpiry string
	var planRevision, requirementRevision int
	if err = tx.QueryRowContext(ctx, `SELECT task_id,plan_revision,requirement_revision,state,lease_token,lease_expires_at FROM task_runs WHERE id=?`, input.RunID).
		Scan(&taskID, &planRevision, &requirementRevision, &runState, &storedLease, &leaseExpiry); err != nil {
		return StepAttempt{}, err
	}
	expires, _ := parseTime(leaseExpiry)
	if runState != RunRunning || storedLease != input.LeaseToken || !expires.After(time.Now().UTC()) {
		return StepAttempt{}, fmt.Errorf("run lease is stale or expired")
	}
	var taskRevision int
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM durable_tasks WHERE id=? AND active_plan_revision=?`, taskID, planRevision).Scan(&taskRevision); err != nil {
		return StepAttempt{}, err
	}
	if taskRevision != input.ExpectedTaskRevision {
		return StepAttempt{}, ErrStaleRevision
	}
	var step Step
	var dependenciesJSON string
	if err = tx.QueryRowContext(ctx, `SELECT id,state,revision,dependencies_json FROM task_steps WHERE task_id=? AND plan_revision=? AND step_key=?`, taskID, planRevision, input.StepKey).
		Scan(&step.ID, &step.State, &step.Revision, &dependenciesJSON); err != nil {
		return StepAttempt{}, err
	}
	if step.Revision != input.ExpectedStepRevision {
		return StepAttempt{}, fmt.Errorf("stale step revision")
	}
	if step.State != StepPending && step.State != StepBlocked && step.State != StepFailed {
		return StepAttempt{}, fmt.Errorf("step cannot start from state %s", step.State)
	}
	if input.Packet != nil && (input.Packet.TaskID != taskID || input.Packet.Step.ID != step.ID ||
		input.Packet.PlanRevision != planRevision || input.Packet.Requirement.Revision != requirementRevision) {
		return StepAttempt{}, fmt.Errorf("attempt packet does not match the selected task plan, requirement and step")
	}
	var dependencies []string
	_ = json.Unmarshal([]byte(dependenciesJSON), &dependencies)
	for _, dependency := range dependencies {
		var dependencyState string
		if err = tx.QueryRowContext(ctx, `SELECT state FROM task_steps WHERE task_id=? AND plan_revision=? AND step_key=?`, taskID, planRevision, dependency).Scan(&dependencyState); err != nil || dependencyState != StepCompleted {
			return StepAttempt{}, fmt.Errorf("dependency %s is not complete", dependency)
		}
	}
	now := time.Now().UTC()
	attempt := StepAttempt{ID: identity.New("attempt"), RunID: input.RunID, TaskID: taskID, StepID: step.ID,
		StepRevision: step.Revision + 1, State: AttemptRunning, InputHash: input.InputHash, Packet: input.Packet, StartedAt: now}
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_step_attempts(id,run_id,task_id,step_id,step_revision,state,input_hash,packet_json,started_at)
		VALUES(?,?,?,?,?,'running',?,?,?)`, attempt.ID, attempt.RunID, attempt.TaskID, attempt.StepID, attempt.StepRevision, attempt.InputHash, packetJSON, formatTime(now)); err != nil {
		return StepAttempt{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE task_steps SET state='running',revision=revision+1,last_error='',updated_at=? WHERE id=? AND revision=?`, formatTime(now), step.ID, step.Revision)
	if err != nil {
		return StepAttempt{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return StepAttempt{}, fmt.Errorf("stale step revision")
	}
	result, err = tx.ExecContext(ctx, `UPDATE durable_tasks SET state='running',revision=revision+1,updated_at=? WHERE id=? AND revision=?`, formatTime(now), taskID, input.ExpectedTaskRevision)
	if err != nil {
		return StepAttempt{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return StepAttempt{}, ErrStaleRevision
	}
	if err = tx.Commit(); err != nil {
		return StepAttempt{}, err
	}
	return attempt, nil
}

func (s *Service) PlanEffect(ctx context.Context, attemptID, action, target, authority string) (EffectIntent, error) {
	if strings.TrimSpace(action) == "" || strings.TrimSpace(target) == "" || strings.TrimSpace(authority) == "" {
		return EffectIntent{}, fmt.Errorf("effect action, target and authority are required")
	}
	var taskID, attemptState, effectScopeJSON string
	if err := s.store.DB.QueryRowContext(ctx, `SELECT a.task_id,a.state,s.effect_scope_json
		FROM task_step_attempts a JOIN task_steps s ON s.id=a.step_id WHERE a.id=?`, attemptID).
		Scan(&taskID, &attemptState, &effectScopeJSON); err != nil {
		return EffectIntent{}, err
	}
	if attemptState != AttemptRunning {
		return EffectIntent{}, fmt.Errorf("attempt is not running")
	}
	var effectScope []string
	if err := json.Unmarshal([]byte(effectScopeJSON), &effectScope); err != nil {
		return EffectIntent{}, fmt.Errorf("decode step effect scope: %w", err)
	}
	action = strings.TrimSpace(action)
	allowed := false
	for _, item := range effectScope {
		if strings.TrimSpace(item) == action {
			allowed = true
			break
		}
	}
	if !allowed {
		return EffectIntent{}, fmt.Errorf("effect action %q is outside the step effect scope", action)
	}
	now := time.Now().UTC()
	item := EffectIntent{ID: identity.New("effect"), AttemptID: attemptID, TaskID: taskID, OperationID: identity.New("operation"),
		Action: action, Target: strings.TrimSpace(target), Authority: strings.TrimSpace(authority), State: EffectPlanned, CreatedAt: now, UpdatedAt: now}
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO task_effect_intents(id,attempt_id,task_id,operation_id,action,target,authority,state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,'planned',?,?)`, item.ID, item.AttemptID, item.TaskID, item.OperationID, item.Action, item.Target, item.Authority, formatTime(now), formatTime(now))
	return item, err
}

func (s *Service) DispatchEffect(ctx context.Context, operationID string) (EffectIntent, error) {
	return s.transitionEffect(ctx, operationID, []string{EffectPlanned}, EffectDispatched, nil, "")
}

func (s *Service) AbandonEffect(ctx context.Context, operationID, reason string) (EffectIntent, error) {
	if strings.TrimSpace(reason) == "" {
		return EffectIntent{}, fmt.Errorf("abandoned effect requires a reason")
	}
	return s.transitionEffect(ctx, operationID, []string{EffectPlanned}, EffectAbandoned, nil, reason)
}

func (s *Service) ObserveEffect(ctx context.Context, operationID string, receipt map[string]any) (EffectIntent, error) {
	if len(receipt) == 0 {
		return EffectIntent{}, fmt.Errorf("observed effect requires a receipt")
	}
	return s.transitionEffect(ctx, operationID, []string{EffectDispatched}, EffectObserved, receipt, "")
}

func (s *Service) ReconcileEffect(ctx context.Context, operationID string, receipt map[string]any, reconciliationError string) (EffectIntent, error) {
	if len(receipt) == 0 && strings.TrimSpace(reconciliationError) == "" {
		return EffectIntent{}, fmt.Errorf("reconciliation requires a receipt or explicit error")
	}
	return s.transitionEffect(ctx, operationID, []string{EffectUncertain}, EffectReconciled, receipt, reconciliationError)
}

func (s *Service) transitionEffect(ctx context.Context, operationID string, from []string, to string, receipt map[string]any, message string) (EffectIntent, error) {
	receiptJSON, _ := json.Marshal(receipt)
	placeholders := strings.TrimRight(strings.Repeat("?,", len(from)), ",")
	args := []any{to, string(receiptJSON), strings.TrimSpace(message), formatTime(time.Now().UTC()), operationID}
	for _, state := range from {
		args = append(args, state)
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE task_effect_intents SET state=?,receipt_json=?,error=?,updated_at=? WHERE operation_id=? AND state IN (`+placeholders+`)`, args...)
	if err != nil {
		return EffectIntent{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return EffectIntent{}, fmt.Errorf("effect transition is stale or invalid")
	}
	return s.getEffect(ctx, operationID)
}

func (s *Service) CompleteAttempt(ctx context.Context, attemptID, output string) (StepAttempt, error) {
	var unresolved int
	if err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_effect_intents WHERE attempt_id=? AND state IN ('planned','dispatched','uncertain')`, attemptID).Scan(&unresolved); err != nil {
		return StepAttempt{}, err
	}
	if unresolved > 0 {
		return StepAttempt{}, fmt.Errorf("attempt has %d unresolved effects", unresolved)
	}
	now := time.Now().UTC()
	result, err := s.store.DB.ExecContext(ctx, `UPDATE task_step_attempts SET state='completed',output=?,completed_at=? WHERE id=? AND state='running'`, output, formatTime(now), attemptID)
	if err != nil {
		return StepAttempt{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return StepAttempt{}, fmt.Errorf("attempt is not running")
	}
	return s.getAttempt(ctx, attemptID)
}

func (s *Service) FailAttempt(ctx context.Context, attemptID, reason string) (StepAttempt, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return StepAttempt{}, fmt.Errorf("attempt failure reason is required")
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return StepAttempt{}, err
	}
	defer tx.Rollback()
	var runID string
	if err = tx.QueryRowContext(ctx, `SELECT run_id FROM task_step_attempts WHERE id=? AND state='running'`, attemptID).Scan(&runID); err != nil {
		return StepAttempt{}, fmt.Errorf("attempt is not running")
	}
	now := formatTime(time.Now().UTC())
	if _, err = tx.ExecContext(ctx, `UPDATE task_step_attempts SET state='failed',error=?,completed_at=? WHERE id=? AND state='running'`, reason, now, attemptID); err != nil {
		return StepAttempt{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE task_runs SET state='failed',stop_reason=?,updated_at=?,completed_at=? WHERE id=? AND state='running'`, reason, now, now, runID); err != nil {
		return StepAttempt{}, err
	}
	if err = tx.Commit(); err != nil {
		return StepAttempt{}, err
	}
	return s.getAttempt(ctx, attemptID)
}

func (s *Service) getRun(ctx context.Context, id string) (Run, error) {
	var item Run
	var expires, created, updated string
	var completed sql.NullString
	err := s.store.DB.QueryRowContext(ctx, `SELECT id,task_id,plan_revision,requirement_revision,state,owner,lease_token,lease_expires_at,stop_reason,created_at,updated_at,completed_at FROM task_runs WHERE id=?`, id).
		Scan(&item.ID, &item.TaskID, &item.PlanRevision, &item.RequirementRevision, &item.State, &item.Owner, &item.LeaseToken, &expires, &item.StopReason, &created, &updated, &completed)
	if err != nil {
		return item, err
	}
	item.LeaseExpiresAt, _ = parseTime(expires)
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	if completed.Valid {
		value, _ := parseTime(completed.String)
		item.CompletedAt = &value
	}
	return item, nil
}

func (s *Service) getAttempt(ctx context.Context, id string) (StepAttempt, error) {
	var item StepAttempt
	var packetJSON, started string
	var completed sql.NullString
	err := s.store.DB.QueryRowContext(ctx, `SELECT id,run_id,task_id,step_id,step_revision,state,input_hash,packet_json,output,error,started_at,completed_at FROM task_step_attempts WHERE id=?`, id).
		Scan(&item.ID, &item.RunID, &item.TaskID, &item.StepID, &item.StepRevision, &item.State, &item.InputHash, &packetJSON, &item.Output, &item.Error, &started, &completed)
	if err != nil {
		return item, err
	}
	item.StartedAt, _ = parseTime(started)
	if packetJSON != "" {
		var packet StepPacket
		if err = json.Unmarshal([]byte(packetJSON), &packet); err != nil {
			return item, fmt.Errorf("decode attempt packet: %w", err)
		}
		item.Packet = &packet
	}
	if completed.Valid {
		value, _ := parseTime(completed.String)
		item.CompletedAt = &value
	}
	return item, nil
}

func (s *Service) Attempt(ctx context.Context, id string) (StepAttempt, error) {
	return s.getAttempt(ctx, id)
}

func (s *Service) Effect(ctx context.Context, operationID string) (EffectIntent, error) {
	return s.getEffect(ctx, operationID)
}

// AttemptEffects returns a stable snapshot without performing nested SQLite
// reads while its cursor is open. This matters because the local store keeps a
// deliberately small connection pool.
func (s *Service) AttemptEffects(ctx context.Context, attemptID string) ([]EffectIntent, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT operation_id FROM task_effect_intents WHERE attempt_id=? ORDER BY created_at,id`, attemptID)
	if err != nil {
		return nil, err
	}
	operationIDs := []string{}
	for rows.Next() {
		var operationID string
		if err = rows.Scan(&operationID); err != nil {
			rows.Close()
			return nil, err
		}
		operationIDs = append(operationIDs, operationID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	items := make([]EffectIntent, 0, len(operationIDs))
	for _, operationID := range operationIDs {
		item, getErr := s.getEffect(ctx, operationID)
		if getErr != nil {
			return nil, getErr
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) Execution(ctx context.Context, taskID string) (ExecutionSnapshot, error) {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	result := ExecutionSnapshot{Task: task, Effects: []EffectIntent{}}
	var runID string
	err = s.store.DB.QueryRowContext(ctx, `SELECT id FROM task_runs WHERE task_id=? ORDER BY created_at DESC,id DESC LIMIT 1`, taskID).Scan(&runID)
	if err == sql.ErrNoRows {
		return result, nil
	}
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	run, err := s.getRun(ctx, runID)
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	result.Run = &run
	var attemptID string
	err = s.store.DB.QueryRowContext(ctx, `SELECT id FROM task_step_attempts WHERE run_id=? ORDER BY started_at DESC,id DESC LIMIT 1`, runID).Scan(&attemptID)
	if err == sql.ErrNoRows {
		return result, nil
	}
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	attempt, err := s.getAttempt(ctx, attemptID)
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	result.Attempt = &attempt
	var proposalID string
	err = s.store.DB.QueryRowContext(ctx, `SELECT id FROM task_code_proposals WHERE attempt_id=? ORDER BY created_at DESC,id DESC LIMIT 1`, attemptID).Scan(&proposalID)
	if err != nil && err != sql.ErrNoRows {
		return ExecutionSnapshot{}, err
	}
	if err == nil {
		proposal, getErr := s.GetCodeProposal(ctx, proposalID)
		if getErr != nil {
			return ExecutionSnapshot{}, getErr
		}
		result.Proposal = &proposal
	}
	result.Effects, err = s.AttemptEffects(ctx, attemptID)
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	return result, nil
}

func (s *Service) UncertainEffects(ctx context.Context, limit int) ([]EffectIntent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,attempt_id,task_id,operation_id,action,target,authority,state,
		receipt_json,error,created_at,updated_at FROM task_effect_intents WHERE state='uncertain' ORDER BY created_at LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []EffectIntent{}
	for rows.Next() {
		var item EffectIntent
		var receiptJSON, created, updated string
		if err = rows.Scan(&item.ID, &item.AttemptID, &item.TaskID, &item.OperationID, &item.Action, &item.Target,
			&item.Authority, &item.State, &receiptJSON, &item.Error, &created, &updated); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(receiptJSON), &item.Receipt)
		item.CreatedAt, _ = parseTime(created)
		item.UpdatedAt, _ = parseTime(updated)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) getEffect(ctx context.Context, operationID string) (EffectIntent, error) {
	var item EffectIntent
	var receiptJSON, created, updated string
	err := s.store.DB.QueryRowContext(ctx, `SELECT id,attempt_id,task_id,operation_id,action,target,authority,state,receipt_json,error,created_at,updated_at FROM task_effect_intents WHERE operation_id=?`, operationID).
		Scan(&item.ID, &item.AttemptID, &item.TaskID, &item.OperationID, &item.Action, &item.Target, &item.Authority, &item.State, &receiptJSON, &item.Error, &created, &updated)
	if err != nil {
		return item, err
	}
	_ = json.Unmarshal([]byte(receiptJSON), &item.Receipt)
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

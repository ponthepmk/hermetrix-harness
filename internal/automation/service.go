// Package automation durably schedules scoped task intake. A schedule creates a
// draft for review; it never grants execution authority, promotes a plan or runs
// a model/tool. Task creation and the deduplicated receipt commit together.
package automation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

type Service struct {
	store *store.Store
	tasks *taskengine.Service
}

var ErrUnavailable = errors.New("automation disabled or changed")

func New(s *store.Store, t *taskengine.Service) *Service { return &Service{s, t} }

type Input struct {
	RequestID string                     `json:"request_id,omitempty"`
	ID        string                     `json:"id,omitempty"`
	Revision  int                        `json:"revision"`
	Name      string                     `json:"name"`
	Template  taskengine.CreateTaskInput `json:"template"`
	Interval  int                        `json:"interval_seconds"`
	Timezone  string                     `json:"timezone"`
	Enabled   bool                       `json:"enabled"`
}
type Schedule struct {
	Input
	Owner string    `json:"-"`
	Next  time.Time `json:"next_run_at"`
}
type Run struct {
	ID           string `json:"id"`
	AutomationID string `json:"automation_id"`
	TaskID       string `json:"task_id"`
	State        string `json:"state"`
	Created      string `json:"created_at"`
}

const selectSchedule = `SELECT id,owner_principal_id,name,template_json,interval_seconds,timezone,enabled,next_run_at,revision FROM automations`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Schedule, error) {
	var s Schedule
	var body, next string
	err := row.Scan(&s.ID, &s.Owner, &s.Name, &body, &s.Interval, &s.Timezone, &s.Enabled, &next, &s.Revision)
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal([]byte(body), &s.Template); err != nil {
		return s, err
	}
	s.Next, err = time.Parse(time.RFC3339Nano, next)
	return s, err
}
func (s *Service) List(ctx context.Context) ([]Schedule, error) {
	owner, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.DB.QueryContext(ctx, selectSchedule+` WHERE owner_principal_id=? ORDER BY created_at DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Schedule{}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (s *Service) Save(ctx context.Context, in Input) (Schedule, error) {
	owner, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return Schedule{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Timezone == "" {
		in.Timezone = "Asia/Bangkok"
	}
	if _, err = time.LoadLocation(in.Timezone); err != nil {
		return Schedule{}, fmt.Errorf("invalid timezone")
	}
	if in.Name == "" || len(in.Name) > 120 || in.Template.ProjectID == "" || in.Interval != 0 && (in.Interval < 300 || in.Interval > 2592000) {
		return Schedule{}, fmt.Errorf("invalid automation name, project or interval")
	}
	in.Template.Actor = owner
	in.Template.EgressPolicy = "local_only"
	in.Template.RemoteEgressApproval = nil
	body, err := json.Marshal(in.Template)
	if err != nil || len(body) > 16384 {
		return Schedule{}, fmt.Errorf("invalid automation template size")
	}
	// Reuse the real task validator in a rollback-only transaction; no draft is
	// persisted merely by saving an automation, and no inference is dispatched.
	validation, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Schedule{}, err
	}
	_, validationErr := s.tasks.CreateInTx(identity.WithPrincipal(ctx, owner), validation, in.Template)
	rollbackErr := validation.Rollback()
	if validationErr != nil {
		return Schedule{}, validationErr
	}
	if rollbackErr != nil {
		return Schedule{}, rollbackErr
	}
	now := time.Now().UTC()
	next := now.Add(time.Duration(in.Interval) * time.Second).Format(time.RFC3339Nano)
	if in.ID == "" {
		if in.RequestID == "" || len(in.RequestID) > 128 {
			return Schedule{}, fmt.Errorf("request_id is required for automation creation")
		}
		digest := sha256.Sum256([]byte(owner + "\x00" + in.RequestID))
		in.ID = fmt.Sprintf("automation_%x", digest[:16])
		prior, lookupErr := scan(s.store.DB.QueryRowContext(ctx, selectSchedule+` WHERE id=? AND owner_principal_id=?`, in.ID, owner))
		if lookupErr == nil {
			priorBody, _ := json.Marshal(prior.Template)
			if prior.Name != in.Name || prior.Interval != in.Interval || prior.Timezone != in.Timezone || string(priorBody) != string(body) {
				return Schedule{}, taskengine.ErrStaleRevision
			}
			return prior, nil
		}
		if lookupErr != sql.ErrNoRows {
			return Schedule{}, lookupErr
		}
		_, err = s.store.DB.ExecContext(ctx, `INSERT INTO automations(id,owner_principal_id,name,template_json,interval_seconds,timezone,enabled,next_run_at,created_at,updated_at) SELECT ?,?,?,?,?,?,?,?,?,? WHERE (SELECT count(*) FROM automations WHERE owner_principal_id=?)<100 ON CONFLICT(id) DO NOTHING`, in.ID, owner, in.Name, string(body), in.Interval, in.Timezone, in.Enabled, next, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), owner)
	} else {
		var result sql.Result
		result, err = s.store.DB.ExecContext(ctx, `UPDATE automations SET name=?,template_json=?,interval_seconds=?,timezone=?,enabled=?,next_run_at=?,revision=revision+1,updated_at=? WHERE id=? AND owner_principal_id=? AND revision=?`, in.Name, string(body), in.Interval, in.Timezone, in.Enabled, next, now.Format(time.RFC3339Nano), in.ID, owner, in.Revision)
		if err == nil {
			n, e := result.RowsAffected()
			if e != nil {
				return Schedule{}, e
			}
			if n != 1 {
				return Schedule{}, taskengine.ErrStaleRevision
			}
		}
	}
	if err != nil {
		return Schedule{}, err
	}
	stored, err := scan(s.store.DB.QueryRowContext(ctx, selectSchedule+` WHERE id=? AND owner_principal_id=?`, in.ID, owner))
	if err == sql.ErrNoRows {
		return Schedule{}, fmt.Errorf("invalid automation count: maximum 100 schedules per owner")
	}
	if err != nil {
		return Schedule{}, err
	}
	storedBody, _ := json.Marshal(stored.Template)
	if stored.Name != in.Name || stored.Interval != in.Interval || stored.Timezone != in.Timezone || string(storedBody) != string(body) {
		return Schedule{}, taskengine.ErrStaleRevision
	}
	return stored, nil
}
func (s *Service) Trigger(ctx context.Context, id, key string) (Run, error) {
	owner, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return Run{}, err
	}
	schedule, err := scan(s.store.DB.QueryRowContext(ctx, selectSchedule+` WHERE id=? AND owner_principal_id=?`, id, owner))
	if err != nil {
		return Run{}, err
	}
	if key == "" || len(key) > 128 {
		return Run{}, fmt.Errorf("invalid request_id")
	}
	return s.dispatch(ctx, schedule, "event:"+key, time.Now().UTC(), false)
}
func (s *Service) Runs(ctx context.Context, id string) ([]Run, error) {
	owner, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return nil, err
	}
	var exists int
	if err = s.store.DB.QueryRowContext(ctx, `SELECT 1 FROM automations WHERE id=? AND owner_principal_id=?`, id, owner).Scan(&exists); err != nil {
		return nil, err
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,automation_id,COALESCE(task_id,''),state,created_at FROM automation_runs WHERE automation_id=? ORDER BY created_at DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		if err = rows.Scan(&r.ID, &r.AutomationID, &r.TaskID, &r.State, &r.Created); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Service) RunDue(ctx context.Context, now time.Time) (int, error) {
	rows, err := s.store.DB.QueryContext(ctx, selectSchedule+` WHERE enabled=1 AND interval_seconds>0 AND julianday(next_run_at)<=julianday(?) ORDER BY next_run_at LIMIT 20`, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	pending := []Schedule{}
	for rows.Next() {
		a, e := scan(rows)
		if e != nil {
			rows.Close()
			return 0, e
		}
		pending = append(pending, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, a := range pending {
		if _, err = s.dispatch(identity.WithPrincipal(ctx, a.Owner), a, "due:"+a.Next.Format(time.RFC3339Nano), now, true); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
func (s *Service) dispatch(ctx context.Context, a Schedule, key string, now time.Time, due bool) (Run, error) {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback()
	current, err := scan(tx.QueryRowContext(ctx, selectSchedule+` WHERE id=? AND owner_principal_id=?`, a.ID, a.Owner))
	if err != nil {
		return Run{}, err
	}
	var prior Run
	err = tx.QueryRowContext(ctx, `SELECT id,automation_id,COALESCE(task_id,''),state,created_at FROM automation_runs WHERE automation_id=? AND dedup_key=?`, a.ID, key).Scan(&prior.ID, &prior.AutomationID, &prior.TaskID, &prior.State, &prior.Created)
	if err == nil {
		return prior, nil
	}
	if err != sql.ErrNoRows {
		return Run{}, err
	}
	if !current.Enabled || current.Revision != a.Revision {
		return Run{}, ErrUnavailable
	}
	if due && (!current.Next.Equal(a.Next) || current.Next.After(now)) {
		return Run{}, taskengine.ErrStaleRevision
	}
	r := Run{ID: identity.New("autorun"), AutomationID: a.ID, State: "awaiting_review", Created: now.UTC().Format(time.RFC3339Nano)}
	var open int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM automation_runs r JOIN durable_tasks t ON t.id=r.task_id WHERE r.automation_id=? AND t.state NOT IN ('completed','cancelled','failed')`, a.ID).Scan(&open); err != nil {
		return Run{}, err
	}
	if open > 0 {
		r.State = "skipped_overlap"
	} else {
		task, err := s.tasks.CreateInTx(identity.WithPrincipal(ctx, a.Owner), tx, current.Template)
		if err != nil {
			return Run{}, err
		}
		r.TaskID = task.TaskID
	}
	var taskID any
	if r.TaskID != "" {
		taskID = r.TaskID
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO automation_runs(id,automation_id,dedup_key,task_id,state,created_at) VALUES(?,?,?,?,?,?)`, r.ID, a.ID, key, taskID, r.State, r.Created); err != nil {
		return Run{}, err
	}
	if due {
		if _, err = tx.ExecContext(ctx, `UPDATE automations SET next_run_at=?,updated_at=? WHERE id=?`, now.UTC().Add(time.Duration(current.Interval)*time.Second).Format(time.RFC3339Nano), r.Created, a.ID); err != nil {
			return Run{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Run{}, err
	}
	return r, nil
}

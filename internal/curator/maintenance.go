package curator

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"hermetrix-harness/internal/durability"
	"hermetrix-harness/internal/identity"
)

var blobRefPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type BlobCandidate struct {
	Ref   string `json:"ref"`
	Bytes int64  `json:"bytes"`
}

type GCRun struct {
	ID               string          `json:"id"`
	State            string          `json:"state"`
	Mode             string          `json:"mode"`
	SnapshotRevision string          `json:"snapshot_revision"`
	ReachableCount   int             `json:"reachable_count"`
	UnreachableCount int             `json:"unreachable_count"`
	ReclaimableBytes int64           `json:"reclaimable_bytes"`
	Candidates       []BlobCandidate `json:"candidates"`
	QuarantinePath   string          `json:"quarantine_path,omitempty"`
	Actor            string          `json:"actor,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	CompletedAt      *time.Time      `json:"completed_at,omitempty"`
}

type Schedule struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	TaskKind        string     `json:"task_kind"`
	IntervalSeconds int        `json:"interval_seconds"`
	Enabled         bool       `json:"enabled"`
	RequireIdle     bool       `json:"require_idle"`
	RequireACPower  bool       `json:"require_ac_power"`
	NextRunAt       time.Time  `json:"next_run_at"`
	LastRunAt       *time.Time `json:"last_run_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type ScheduleInput struct {
	ID              string    `json:"id,omitempty"`
	Name            string    `json:"name"`
	TaskKind        string    `json:"task_kind"`
	IntervalSeconds int       `json:"interval_seconds"`
	Enabled         bool      `json:"enabled"`
	RequireIdle     bool      `json:"require_idle"`
	RequireACPower  bool      `json:"require_ac_power"`
	NextRunAt       time.Time `json:"next_run_at"`
}

type SystemState struct {
	Idle    bool `json:"idle"`
	OnPower bool `json:"on_ac_power"`
}

// DetectSystemState is deliberately conservative. Unsupported platforms or a
// failed probe are reported as busy/on-battery so policy-gated maintenance is
// skipped rather than run at the wrong time.
func DetectSystemState(ctx context.Context) SystemState {
	state := SystemState{}
	if runtime.GOOS != "darwin" {
		return state
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(probeCtx, "pmset", "-g", "batt").Output(); err == nil {
		state.OnPower = strings.Contains(string(output), "AC Power")
	}
	if output, err := exec.CommandContext(probeCtx, "ioreg", "-c", "IOHIDSystem", "-d", "4").Output(); err == nil {
		match := regexp.MustCompile(`"HIDIdleTime" = ([0-9]+)`).FindSubmatch(output)
		if len(match) == 2 {
			nanoseconds, _ := strconv.ParseUint(string(match[1]), 10, 64)
			state.Idle = time.Duration(nanoseconds) >= 5*time.Minute
		}
	}
	return state
}

type ScheduleExecution struct {
	ScheduleID string `json:"schedule_id"`
	TaskKind   string `json:"task_kind"`
	ResultID   string `json:"result_id,omitempty"`
	Skipped    string `json:"skipped,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (s *Service) DryRunGC(ctx context.Context) (GCRun, error) {
	reachable, candidates, snapshot, err := s.gcSnapshot(ctx)
	if err != nil {
		return GCRun{}, err
	}
	now := time.Now().UTC()
	run := GCRun{ID: identity.New("gc"), State: "planned", Mode: "dry_run", SnapshotRevision: snapshot,
		ReachableCount: len(reachable), UnreachableCount: len(candidates), Candidates: candidates, CreatedAt: now}
	for _, candidate := range candidates {
		run.ReclaimableBytes += candidate.Bytes
	}
	encoded, _ := json.Marshal(candidates)
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO gc_runs(id,state,mode,snapshot_revision,reachable_count,
    unreachable_count,reclaimable_bytes,candidates_json,created_at) VALUES(?,'planned','dry_run',?,?,?,?,?,?)`,
		run.ID, snapshot, run.ReachableCount, run.UnreachableCount, run.ReclaimableBytes, string(encoded), formatTime(now))
	return run, err
}

func (s *Service) ApplyGC(ctx context.Context, runID, actor string) (GCRun, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return GCRun{}, fmt.Errorf("GC actor is required")
	}
	run, err := s.GetGCRun(ctx, runID)
	if err != nil {
		return GCRun{}, err
	}
	if run.State != "planned" || run.Mode != "dry_run" {
		return GCRun{}, fmt.Errorf("GC run is not an unapplied dry-run")
	}
	_, candidates, snapshot, err := s.gcSnapshot(ctx)
	if err != nil {
		return GCRun{}, err
	}
	if snapshot != run.SnapshotRevision || !sameCandidates(candidates, run.Candidates) {
		return GCRun{}, fmt.Errorf("GC snapshot is stale; run a new dry-run")
	}
	quarantineRoot := filepath.Join(s.store.Root, "blobs", "quarantine", run.ID)
	moved := []BlobCandidate{}
	for _, candidate := range run.Candidates {
		if _, err := s.store.Blobs.Quarantine(candidate.Ref, quarantineRoot); err != nil {
			rollbackFailed := false
			for _, prior := range moved {
				if _, restoreErr := s.store.Blobs.RestoreFromQuarantine(prior.Ref, quarantineRoot); restoreErr != nil {
					rollbackFailed = true
				}
			}
			state := "planned"
			if rollbackFailed {
				state = "partial_quarantine"
			}
			durability.Exec("record GC quarantine failure").Observe(s.store.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE gc_runs SET state=?,actor=?,quarantine_path=? WHERE id=?`,
				state, actor, quarantineRoot, run.ID))
			return GCRun{}, err
		}
		moved = append(moved, candidate)
	}
	completed := time.Now().UTC()
	result, err := s.store.DB.ExecContext(ctx, `UPDATE gc_runs SET state='quarantined',mode='apply',actor=?,quarantine_path=?,
    completed_at=? WHERE id=? AND state='planned'`, actor, quarantineRoot, formatTime(completed), run.ID)
	changed := int64(0)
	if err == nil {
		changed, err = result.RowsAffected()
		if err == nil && changed != 1 {
			err = fmt.Errorf("GC run state changed while committing quarantine")
		}
	}
	if err != nil {
		rollbackFailed := false
		for _, prior := range moved {
			if _, restoreErr := s.store.Blobs.RestoreFromQuarantine(prior.Ref, quarantineRoot); restoreErr != nil {
				rollbackFailed = true
			}
		}
		if rollbackFailed {
			durability.Exec("record partial GC rollback").Observe(s.store.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE gc_runs SET state='partial_quarantine',actor=?,quarantine_path=?
				WHERE id=? AND state='planned'`, actor, quarantineRoot, run.ID))
		}
		return GCRun{}, err
	}
	run.State, run.Mode, run.Actor, run.QuarantinePath, run.CompletedAt = "quarantined", "apply", actor, quarantineRoot, &completed
	return run, nil
}

func (s *Service) RestoreGC(ctx context.Context, runID, actor string) (GCRun, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return GCRun{}, fmt.Errorf("restore actor is required")
	}
	run, err := s.GetGCRun(ctx, runID)
	if err != nil {
		return GCRun{}, err
	}
	if (run.State != "quarantined" && run.State != "partial_quarantine") || run.QuarantinePath == "" {
		return GCRun{}, fmt.Errorf("GC run has no recoverable quarantine")
	}
	for _, candidate := range run.Candidates {
		if _, err := s.store.Blobs.RestoreFromQuarantine(candidate.Ref, run.QuarantinePath); err != nil {
			return GCRun{}, err
		}
	}
	completed := time.Now().UTC()
	result, err := s.store.DB.ExecContext(ctx, `UPDATE gc_runs SET state='restored',actor=?,completed_at=?
		WHERE id=? AND state IN ('quarantined','partial_quarantine')`, actor, formatTime(completed), run.ID)
	if err != nil {
		return GCRun{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return GCRun{}, fmt.Errorf("GC restore state changed concurrently")
	}
	run.State, run.Actor, run.CompletedAt = "restored", actor, &completed
	return run, nil
}

func (s *Service) ListGCRuns(ctx context.Context, limit int) ([]GCRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.store.DB.QueryContext(ctx, gcRunSelect+` ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GCRun{}
	for rows.Next() {
		item, err := scanGCRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) GetGCRun(ctx context.Context, id string) (GCRun, error) {
	return scanGCRun(s.store.DB.QueryRowContext(ctx, gcRunSelect+` WHERE id=?`, id))
}

const gcRunSelect = `SELECT id,state,mode,snapshot_revision,reachable_count,unreachable_count,reclaimable_bytes,
  candidates_json,quarantine_path,actor,created_at,completed_at FROM gc_runs`

func (s *Service) gcSnapshot(ctx context.Context) (map[string]bool, []BlobCandidate, string, error) {
	reachable, err := s.databaseBlobReferences(ctx)
	if err != nil {
		return nil, nil, "", err
	}
	blobs, err := s.store.Blobs.List()
	if err != nil {
		return nil, nil, "", err
	}
	candidates := []BlobCandidate{}
	var revision strings.Builder
	references := make([]string, 0, len(reachable))
	for ref := range reachable {
		references = append(references, ref)
	}
	sort.Strings(references)
	for _, ref := range references {
		revision.WriteString("R:")
		revision.WriteString(ref)
		revision.WriteByte('\n')
	}
	for _, item := range blobs {
		revision.WriteString(fmt.Sprintf("B:%s:%d\n", item.Ref, item.Bytes))
		if !reachable[item.Ref] {
			candidates = append(candidates, BlobCandidate{Ref: item.Ref, Bytes: item.Bytes})
		}
	}
	sum := sha256.Sum256([]byte(revision.String()))
	return reachable, candidates, hex.EncodeToString(sum[:]), nil
}

func (s *Service) databaseBlobReferences(ctx context.Context) (map[string]bool, error) {
	references := map[string]bool{}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	tables := []string{}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			return nil, err
		}
		if table != "gc_runs" {
			tables = append(tables, table)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, table := range tables {
		if !regexp.MustCompile(`^[a-z_]+$`).MatchString(table) {
			continue
		}
		values, err := s.store.DB.QueryContext(ctx, `SELECT * FROM `+table)
		if err != nil {
			return nil, err
		}
		columns, _ := values.Columns()
		for values.Next() {
			raw := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for index := range raw {
				pointers[index] = &raw[index]
			}
			if err := values.Scan(pointers...); err != nil {
				values.Close()
				return nil, err
			}
			for _, value := range raw {
				collectReferences(value, references)
			}
		}
		if err := values.Close(); err != nil {
			return nil, err
		}
	}
	return references, nil
}

func collectReferences(value any, references map[string]bool) {
	if bytes, ok := value.([]byte); ok {
		value = string(bytes)
	}
	text, ok := value.(string)
	if !ok {
		return
	}
	if blobRefPattern.MatchString(text) {
		references[text] = true
		return
	}
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var nested any
		if json.Unmarshal([]byte(trimmed), &nested) == nil {
			collectNestedReferences(nested, references)
		}
	}
}

func collectNestedReferences(value any, references map[string]bool) {
	switch typed := value.(type) {
	case string:
		collectReferences(typed, references)
	case []any:
		for _, nested := range typed {
			collectNestedReferences(nested, references)
		}
	case map[string]any:
		for _, nested := range typed {
			collectNestedReferences(nested, references)
		}
	}
}

func sameCandidates(left, right []BlobCandidate) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type gcScanner interface{ Scan(...any) error }

func scanGCRun(row gcScanner) (GCRun, error) {
	var item GCRun
	var candidates, created string
	var completed sql.NullString
	if err := row.Scan(&item.ID, &item.State, &item.Mode, &item.SnapshotRevision, &item.ReachableCount,
		&item.UnreachableCount, &item.ReclaimableBytes, &candidates, &item.QuarantinePath, &item.Actor,
		&created, &completed); err != nil {
		return GCRun{}, err
	}
	_ = json.Unmarshal([]byte(candidates), &item.Candidates)
	item.CreatedAt, _ = parseTime(created)
	if completed.Valid {
		value, _ := parseTime(completed.String)
		item.CompletedAt = &value
	}
	return item, nil
}

func (s *Service) SaveSchedule(ctx context.Context, input ScheduleInput) (Schedule, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || (input.TaskKind != "curator" && input.TaskKind != "gc_dry_run") ||
		input.IntervalSeconds < 300 || input.IntervalSeconds > 30*24*60*60 {
		return Schedule{}, fmt.Errorf("schedule requires a name, supported task and interval between 5 minutes and 30 days")
	}
	now := time.Now().UTC()
	if input.NextRunAt.IsZero() {
		input.NextRunAt = now.Add(time.Duration(input.IntervalSeconds) * time.Second)
	}
	if input.ID == "" {
		input.ID = identity.New("schedule")
		_, err := s.store.DB.ExecContext(ctx, `INSERT INTO maintenance_schedules(id,name,task_kind,interval_seconds,enabled,
      require_idle,require_ac_power,next_run_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, input.ID, input.Name,
			input.TaskKind, input.IntervalSeconds, input.Enabled, input.RequireIdle, input.RequireACPower,
			formatTime(input.NextRunAt), formatTime(now), formatTime(now))
		if err != nil {
			return Schedule{}, err
		}
	} else {
		result, err := s.store.DB.ExecContext(ctx, `UPDATE maintenance_schedules SET name=?,task_kind=?,interval_seconds=?,
      enabled=?,require_idle=?,require_ac_power=?,next_run_at=?,updated_at=? WHERE id=?`, input.Name, input.TaskKind,
			input.IntervalSeconds, input.Enabled, input.RequireIdle, input.RequireACPower, formatTime(input.NextRunAt),
			formatTime(now), input.ID)
		if err != nil {
			return Schedule{}, err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return Schedule{}, sql.ErrNoRows
		}
	}
	return s.GetSchedule(ctx, input.ID)
}

func (s *Service) GetSchedule(ctx context.Context, id string) (Schedule, error) {
	return scanSchedule(s.store.DB.QueryRowContext(ctx, scheduleSelect+` WHERE id=?`, id))
}

func (s *Service) ListSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.store.DB.QueryContext(ctx, scheduleSelect+` ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Schedule{}
	for rows.Next() {
		item, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

const scheduleSelect = `SELECT id,name,task_kind,interval_seconds,enabled,require_idle,require_ac_power,next_run_at,
  last_run_at,created_at,updated_at FROM maintenance_schedules`

func (s *Service) RunDue(ctx context.Context, state SystemState) ([]ScheduleExecution, error) {
	schedules, err := s.ListSchedules(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := []ScheduleExecution{}
	for _, schedule := range schedules {
		if !schedule.Enabled || schedule.NextRunAt.After(now) {
			continue
		}
		execution := ScheduleExecution{ScheduleID: schedule.ID, TaskKind: schedule.TaskKind}
		if schedule.RequireIdle && !state.Idle {
			execution.Skipped = "system is not idle"
			out = append(out, execution)
			continue
		}
		if schedule.RequireACPower && !state.OnPower {
			execution.Skipped = "AC power is required"
			out = append(out, execution)
			continue
		}
		switch schedule.TaskKind {
		case "curator":
			run, runErr := s.RunReportOnly(ctx)
			execution.ResultID, err = run.ID, runErr
			if runErr == nil {
				_, err = s.ApplyConfiguredAuthority(ctx, run.ID)
			}
		case "gc_dry_run":
			run, runErr := s.DryRunGC(ctx)
			execution.ResultID, err = run.ID, runErr
		}
		if err != nil {
			execution.Error = err.Error()
		}
		next := now.Add(time.Duration(schedule.IntervalSeconds) * time.Second)
		_, updateErr := s.store.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE maintenance_schedules SET last_run_at=?,
      next_run_at=?,updated_at=? WHERE id=?`, formatTime(now), formatTime(next), formatTime(now), schedule.ID)
		if updateErr != nil {
			return out, updateErr
		}
		out = append(out, execution)
	}
	return out, nil
}

type scheduleScanner interface{ Scan(...any) error }

func scanSchedule(row scheduleScanner) (Schedule, error) {
	var item Schedule
	var next, created, updated string
	var last sql.NullString
	if err := row.Scan(&item.ID, &item.Name, &item.TaskKind, &item.IntervalSeconds, &item.Enabled, &item.RequireIdle,
		&item.RequireACPower, &next, &last, &created, &updated); err != nil {
		return Schedule{}, err
	}
	item.NextRunAt, _ = parseTime(next)
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	if last.Valid {
		value, _ := parseTime(last.String)
		item.LastRunAt = &value
	}
	return item, nil
}

package taskengine

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

const (
	PlannerPlanned    = "planned"
	PlannerDispatched = "dispatched"
	PlannerObserved   = "observed"
	PlannerRejected   = "rejected"
	PlannerFailed     = "failed"
	PlannerAbandoned  = "abandoned"
	PlannerUncertain  = "uncertain"
)

type PlannerRun struct {
	ID                   string    `json:"id"`
	TaskID               string    `json:"task_id"`
	RequirementRevision  int       `json:"requirement_revision"`
	ExpectedTaskRevision int       `json:"expected_task_revision"`
	ProviderID           string    `json:"provider_id"`
	ProviderRevision     string    `json:"provider_revision"`
	InputHash            string    `json:"input_hash"`
	State                string    `json:"state"`
	ArtifactID           string    `json:"artifact_id,omitempty"`
	Error                string    `json:"error,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (s *Service) BeginPlannerRun(ctx context.Context, taskID string, expectedTaskRevision int, providerID, providerRevision, inputHash string) (PlannerRun, error) {
	if strings.TrimSpace(providerID) == "" || strings.TrimSpace(providerRevision) == "" || strings.TrimSpace(inputHash) == "" {
		return PlannerRun{}, fmt.Errorf("planner provider, revision and input hash are required")
	}
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return PlannerRun{}, err
	}
	if task.Revision != expectedTaskRevision {
		return PlannerRun{}, ErrStaleRevision
	}
	if task.State == StateCompleted || task.State == StateCancelled {
		return PlannerRun{}, fmt.Errorf("cannot plan terminal task")
	}
	now := time.Now().UTC()
	item := PlannerRun{ID: identity.New("planner"), TaskID: task.ID, RequirementRevision: task.ActiveRequirementRevision,
		ExpectedTaskRevision: task.Revision, ProviderID: providerID, ProviderRevision: providerRevision,
		InputHash: inputHash, State: PlannerPlanned, CreatedAt: now, UpdatedAt: now}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO task_planner_runs(id,task_id,requirement_revision,expected_task_revision,
		provider_id,provider_revision,input_hash,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'planned',?,?)`,
		item.ID, item.TaskID, item.RequirementRevision, item.ExpectedTaskRevision, item.ProviderID, item.ProviderRevision,
		item.InputHash, formatTime(now), formatTime(now))
	return item, err
}

func (s *Service) DispatchPlannerRun(ctx context.Context, id string) (PlannerRun, error) {
	return s.transitionPlannerRun(ctx, id, PlannerPlanned, PlannerDispatched, "", "")
}

func (s *Service) ObservePlannerRun(ctx context.Context, id, artifactID string) (PlannerRun, error) {
	if strings.TrimSpace(artifactID) == "" {
		return PlannerRun{}, fmt.Errorf("planner observation requires an artifact")
	}
	return s.transitionPlannerRun(ctx, id, PlannerDispatched, PlannerObserved, artifactID, "")
}

func (s *Service) FailPlannerRun(ctx context.Context, id, from, reason string) (PlannerRun, error) {
	if from != PlannerPlanned && from != PlannerDispatched {
		return PlannerRun{}, fmt.Errorf("invalid planner failure source")
	}
	if strings.TrimSpace(reason) == "" {
		return PlannerRun{}, fmt.Errorf("planner failure requires a reason")
	}
	return s.transitionPlannerRun(ctx, id, from, PlannerFailed, "", reason)
}

func (s *Service) RejectPlannerRun(ctx context.Context, id, reason string) (PlannerRun, error) {
	if strings.TrimSpace(reason) == "" {
		return PlannerRun{}, fmt.Errorf("planner rejection requires a reason")
	}
	return s.transitionPlannerRun(ctx, id, PlannerObserved, PlannerRejected, "", reason)
}

func (s *Service) transitionPlannerRun(ctx context.Context, id, from, to, artifactID, message string) (PlannerRun, error) {
	result, err := s.store.DB.ExecContext(ctx, `UPDATE task_planner_runs SET state=?,artifact_id=COALESCE(NULLIF(?,''),artifact_id),error=?,updated_at=? WHERE id=? AND state=?`,
		to, artifactID, strings.TrimSpace(message), formatTime(time.Now().UTC()), id, from)
	if err != nil {
		return PlannerRun{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return PlannerRun{}, fmt.Errorf("planner transition is stale or invalid")
	}
	return s.PlannerRun(ctx, id)
}

func (s *Service) PlannerRun(ctx context.Context, id string) (PlannerRun, error) {
	var item PlannerRun
	var artifact sql.NullString
	var created, updated string
	err := s.store.DB.QueryRowContext(ctx, `SELECT id,task_id,requirement_revision,expected_task_revision,provider_id,
		provider_revision,input_hash,state,artifact_id,error,created_at,updated_at FROM task_planner_runs WHERE id=?`, id).
		Scan(&item.ID, &item.TaskID, &item.RequirementRevision, &item.ExpectedTaskRevision, &item.ProviderID,
			&item.ProviderRevision, &item.InputHash, &item.State, &artifact, &item.Error, &created, &updated)
	if err != nil {
		return item, err
	}
	item.ArtifactID = artifact.String
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

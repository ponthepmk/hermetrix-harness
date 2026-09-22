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

// DecisionShadowRun is an immutable evaluation receipt. It records model
// behavior but grants no authority to execute the selected action.
type DecisionShadowRun struct {
	ID               string            `json:"id"`
	TaskID           string            `json:"task_id"`
	TaskRevision     int               `json:"task_revision"`
	ProviderID       string            `json:"provider_id"`
	ProviderRevision string            `json:"provider_revision"`
	ModelName        string            `json:"model_name"`
	State            CompactState      `json:"state"`
	Candidates       []CandidateAction `json:"candidates"`
	Baseline         DecisionResult    `json:"baseline"`
	Model            *DecisionResult   `json:"model,omitempty"`
	Agreement        bool              `json:"agreement"`
	Valid            bool              `json:"valid"`
	LatencyMS        int64             `json:"latency_ms"`
	PromptTokens     int               `json:"prompt_tokens"`
	CompletionTokens int               `json:"completion_tokens"`
	ModelError       string            `json:"model_error,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
}

type DecisionShadowMetrics struct {
	TotalRuns        int     `json:"total_runs"`
	ValidRuns        int     `json:"valid_runs"`
	AgreementRuns    int     `json:"agreement_runs"`
	AgreementRate    float64 `json:"agreement_rate"`
	InvalidRate      float64 `json:"invalid_rate"`
	AverageLatencyMS float64 `json:"average_latency_ms"`
}

func (s *Service) RecordDecisionShadow(ctx context.Context, item DecisionShadowRun) (DecisionShadowRun, error) {
	if s == nil || s.store == nil || item.TaskID == "" || item.TaskRevision < 1 || item.ProviderID == "" ||
		item.ProviderRevision == "" || item.ModelName == "" || len(item.Candidates) == 0 || item.Baseline.ActionID == "" {
		return DecisionShadowRun{}, fmt.Errorf("decision shadow receipt is incomplete")
	}
	if item.State.TaskID != item.TaskID || item.State.TaskRevision != item.TaskRevision {
		return DecisionShadowRun{}, fmt.Errorf("decision shadow state does not match task snapshot")
	}
	item.Valid = item.Model != nil
	item.Agreement = item.Model != nil && item.Model.ActionID == item.Baseline.ActionID
	if item.Model == nil && strings.TrimSpace(item.ModelError) == "" {
		return DecisionShadowRun{}, fmt.Errorf("invalid decision shadow receipt requires an error")
	}
	stateJSON, stateErr := json.Marshal(item.State)
	candidatesJSON, candidatesErr := json.Marshal(item.Candidates)
	baselineJSON, baselineErr := json.Marshal(item.Baseline)
	if stateErr != nil || candidatesErr != nil || baselineErr != nil {
		return DecisionShadowRun{}, fmt.Errorf("encode decision shadow receipt")
	}
	var modelJSON any
	if item.Model != nil {
		encoded, err := json.Marshal(item.Model)
		if err != nil {
			return DecisionShadowRun{}, fmt.Errorf("encode model decision: %w", err)
		}
		modelJSON = string(encoded)
	}
	item.ID, item.CreatedAt = identity.New("decision-shadow"), time.Now().UTC()
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO task_decision_shadow_runs(id,task_id,task_revision,provider_id,
		provider_revision,model_name,state_json,candidates_json,baseline_json,model_json,agreement,valid,latency_ms,
		prompt_tokens,completion_tokens,model_error,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.TaskID, item.TaskRevision, item.ProviderID, item.ProviderRevision, item.ModelName,
		string(stateJSON), string(candidatesJSON), string(baselineJSON), modelJSON, item.Agreement, item.Valid,
		item.LatencyMS, item.PromptTokens, item.CompletionTokens, item.ModelError, formatTime(item.CreatedAt))
	if err != nil {
		return DecisionShadowRun{}, fmt.Errorf("record decision shadow: %w", err)
	}
	return item, nil
}

func (s *Service) ListDecisionShadows(ctx context.Context, taskID string, limit int) ([]DecisionShadowRun, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("task id is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,task_id,task_revision,provider_id,provider_revision,model_name,
		state_json,candidates_json,baseline_json,model_json,agreement,valid,latency_ms,prompt_tokens,completion_tokens,
		model_error,created_at FROM task_decision_shadow_runs WHERE task_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DecisionShadowRun{}
	for rows.Next() {
		var item DecisionShadowRun
		var stateJSON, candidatesJSON, baselineJSON string
		var modelJSON sql.NullString
		var agreement, valid bool
		var createdAt string
		if err = rows.Scan(&item.ID, &item.TaskID, &item.TaskRevision, &item.ProviderID, &item.ProviderRevision,
			&item.ModelName, &stateJSON, &candidatesJSON, &baselineJSON, &modelJSON, &agreement, &valid,
			&item.LatencyMS, &item.PromptTokens, &item.CompletionTokens, &item.ModelError, &createdAt); err != nil {
			return nil, err
		}
		item.Agreement, item.Valid = agreement, valid
		if item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(stateJSON), &item.State); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(candidatesJSON), &item.Candidates); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(baselineJSON), &item.Baseline); err != nil {
			return nil, err
		}
		if modelJSON.Valid {
			item.Model = &DecisionResult{}
			if err = json.Unmarshal([]byte(modelJSON.String), item.Model); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) DecisionShadowMetrics(ctx context.Context, taskID, providerID string) (DecisionShadowMetrics, error) {
	if strings.TrimSpace(taskID) == "" {
		return DecisionShadowMetrics{}, fmt.Errorf("task id is required")
	}
	query := `SELECT COUNT(*),COALESCE(SUM(valid),0),COALESCE(SUM(agreement),0),COALESCE(AVG(latency_ms),0)
		FROM task_decision_shadow_runs WHERE task_id=?`
	args := []any{taskID}
	if providerID = strings.TrimSpace(providerID); providerID != "" {
		query += ` AND provider_id=?`
		args = append(args, providerID)
	}
	var metrics DecisionShadowMetrics
	if err := s.store.DB.QueryRowContext(ctx, query, args...).Scan(&metrics.TotalRuns, &metrics.ValidRuns,
		&metrics.AgreementRuns, &metrics.AverageLatencyMS); err != nil {
		return DecisionShadowMetrics{}, err
	}
	if metrics.TotalRuns > 0 {
		metrics.InvalidRate = float64(metrics.TotalRuns-metrics.ValidRuns) / float64(metrics.TotalRuns)
	}
	if metrics.ValidRuns > 0 {
		metrics.AgreementRate = float64(metrics.AgreementRuns) / float64(metrics.ValidRuns)
	}
	return metrics, nil
}

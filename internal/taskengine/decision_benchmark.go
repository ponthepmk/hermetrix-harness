package taskengine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

type DecisionFixture struct {
	ID             string            `json:"id"`
	Description    string            `json:"description"`
	State          CompactState      `json:"state"`
	Candidates     []CandidateAction `json:"candidates"`
	ExpectedAction string            `json:"expected_action"`
}

type DecisionBenchmarkCase struct {
	FixtureID    string `json:"fixture_id"`
	Expected     string `json:"expected"`
	Selected     string `json:"selected,omitempty"`
	Valid        bool   `json:"valid"`
	Correct      bool   `json:"correct"`
	LatencyMS    int64  `json:"latency_ms"`
	PromptTokens int    `json:"prompt_tokens"`
	OutputTokens int    `json:"output_tokens"`
	Error        string `json:"error,omitempty"`
}

type DecisionBenchmarkRun struct {
	ID               string                  `json:"id"`
	ProviderID       string                  `json:"provider_id"`
	ProviderRevision string                  `json:"provider_revision"`
	ModelName        string                  `json:"model_name"`
	DecisionRevision string                  `json:"decision_revision"`
	Results          []DecisionBenchmarkCase `json:"results"`
	TotalCases       int                     `json:"total_cases"`
	ValidCases       int                     `json:"valid_cases"`
	CorrectCases     int                     `json:"correct_cases"`
	Accuracy         float64                 `json:"accuracy"`
	InvalidRate      float64                 `json:"invalid_rate"`
	AverageLatencyMS float64                 `json:"average_latency_ms"`
	Passed           bool                    `json:"passed"`
	Error            string                  `json:"error,omitempty"`
	CreatedAt        time.Time               `json:"created_at"`
}

type DecisionAdmissionPolicy struct {
	ProviderID       string    `json:"provider_id"`
	ProviderRevision string    `json:"provider_revision"`
	BenchmarkRunID   string    `json:"benchmark_run_id"`
	Enabled          bool      `json:"enabled"`
	AllowedRisks     []string  `json:"allowed_risks"`
	Actor            string    `json:"actor"`
	Reason           string    `json:"reason"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type DecisionAdmissionThreshold struct {
	MinimumCases            int     `json:"minimum_cases"`
	MinimumAccuracy         float64 `json:"minimum_accuracy"`
	MaximumInvalidRate      float64 `json:"maximum_invalid_rate"`
	MaximumAverageLatencyMS float64 `json:"maximum_average_latency_ms"`
}

func CurrentDecisionAdmissionThreshold() DecisionAdmissionThreshold {
	return DecisionAdmissionThreshold{MinimumCases: len(DecisionFixtures()), MinimumAccuracy: 0.80,
		MaximumInvalidRate: 0, MaximumAverageLatencyMS: 10_000}
}

// DecisionFixtures returns immutable-value copies of the admission corpus.
// It deliberately covers the boundary states that can change the safe action.
func DecisionFixtures() []DecisionFixture {
	step := &CompactStep{ID: "fixture-step", Key: "fix", Revision: 1, State: StepPending,
		Checks: []string{"go test ./..."}}
	readCandidates := []CandidateAction{
		{ID: "inspect_file", Type: "inspect_file", Target: step.ID, Risk: "read", Cost: "low", Reason: "inspect source"},
		{ID: "search_repository", Type: "search_repository", Target: step.ID, Risk: "read", Cost: "low", Reason: "search related code"},
		{ID: "retrieve_memory", Type: "retrieve_memory", Target: "fixture", Risk: "read", Cost: "low", Reason: "retrieve reviewed knowledge"},
		{ID: "run_test", Type: "run_test", Target: step.ID, Risk: "execute", Cost: "medium", RequiresPolicy: true, Reason: "run declared check"},
	}
	base := CompactState{TaskID: "fixture", TaskRevision: 1, RequirementRevision: 1, PlanRevision: 1,
		Goal: "repair the verified defect", TaskState: StateReady, CurrentStep: step, Criteria: map[string]string{"AC-1": ValidationUnknown}}
	clone := func(state CompactState, candidates []CandidateAction) (CompactState, []CandidateAction) {
		encoded, _ := json.Marshal(struct {
			State      CompactState
			Candidates []CandidateAction
		}{state, candidates})
		var copy struct {
			State      CompactState
			Candidates []CandidateAction
		}
		_ = json.Unmarshal(encoded, &copy)
		return copy.State, copy.Candidates
	}
	items := make([]DecisionFixture, 0, 6)
	state, candidates := clone(base, readCandidates)
	state.PlanRevision, state.CurrentStep = 0, nil
	items = append(items, DecisionFixture{ID: "no-plan", Description: "No plan requires bounded planning", State: state,
		Candidates: []CandidateAction{{ID: "retrieve_memory", Type: "retrieve_memory", Risk: "read", Cost: "low"},
			{ID: "ask_planner", Type: "ask_planner", Risk: "model", Cost: "medium", RequiresPolicy: true}}, ExpectedAction: "ask_planner"})
	state, candidates = clone(base, readCandidates)
	items = append(items, DecisionFixture{ID: "runnable-step", Description: "A fresh runnable step needs source evidence",
		State: state, Candidates: candidates, ExpectedAction: "inspect_file"})
	state, candidates = clone(base, readCandidates)
	state.FailedChecks = []string{"go test ./..."}
	items = append(items, DecisionFixture{ID: "failed-check", Description: "A current failed check should be rerun after work",
		State: state, Candidates: candidates, ExpectedAction: "run_test"})
	state, _ = clone(base, readCandidates)
	state.ConsecutiveFailures = 2
	items = append(items, DecisionFixture{ID: "repeated-failure", Description: "Repeated failure retrieves reviewed knowledge before replanning", State: state,
		Candidates: []CandidateAction{{ID: "retrieve_memory", Type: "retrieve_memory", Risk: "read", Cost: "low"},
			{ID: "ask_planner", Type: "ask_planner", Risk: "model", Cost: "medium", RequiresPolicy: true}}, ExpectedAction: "retrieve_memory"})
	state, _ = clone(base, readCandidates)
	state.UnresolvedEffects = []string{"operation-1"}
	items = append(items, DecisionFixture{ID: "unresolved-effect", Description: "An uncertain effect must be reconciled before new work", State: state,
		Candidates: []CandidateAction{{ID: "reconcile_effects", Type: "reconcile_effects", Risk: "write", Cost: "medium", RequiresPolicy: true}}, ExpectedAction: "reconcile_effects"})
	state, _ = clone(base, readCandidates)
	state.TaskState, state.CurrentStep, state.CompletionEligible = StateVerifying, nil, true
	state.Criteria = map[string]string{"AC-1": ValidationPass}
	items = append(items, DecisionFixture{ID: "completion", Description: "All verified criteria permit completion", State: state,
		Candidates: []CandidateAction{{ID: "finish", Type: "finish", Risk: "transition", Cost: "low", RequiresPolicy: true}}, ExpectedAction: "finish"})
	return items
}

func (s *Service) RecordDecisionBenchmark(ctx context.Context, run DecisionBenchmarkRun) (DecisionBenchmarkRun, error) {
	if run.ProviderID == "" || run.ProviderRevision == "" || run.ModelName == "" || run.DecisionRevision == "" || len(run.Results) == 0 {
		return DecisionBenchmarkRun{}, fmt.Errorf("decision benchmark receipt is incomplete")
	}
	run.ID, run.CreatedAt, run.TotalCases = identity.New("decision-benchmark"), time.Now().UTC(), len(run.Results)
	var latency int64
	for _, result := range run.Results {
		if result.Valid {
			run.ValidCases++
		}
		if result.Correct {
			run.CorrectCases++
		}
		latency += result.LatencyMS
	}
	run.Accuracy = float64(run.CorrectCases) / float64(run.TotalCases)
	run.InvalidRate = float64(run.TotalCases-run.ValidCases) / float64(run.TotalCases)
	run.AverageLatencyMS = float64(latency) / float64(run.TotalCases)
	encoded, err := json.Marshal(run.Results)
	if err != nil {
		return DecisionBenchmarkRun{}, err
	}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO decision_benchmark_runs(id,provider_id,provider_revision,model_name,
		decision_revision,results_json,total_cases,valid_cases,correct_cases,accuracy,invalid_rate,average_latency_ms,passed,error,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, run.ID, run.ProviderID, run.ProviderRevision, run.ModelName, run.DecisionRevision,
		string(encoded), run.TotalCases, run.ValidCases, run.CorrectCases, run.Accuracy, run.InvalidRate, run.AverageLatencyMS,
		run.Passed, run.Error, formatTime(run.CreatedAt))
	return run, err
}

func (s *Service) ListDecisionBenchmarks(ctx context.Context, providerID string, limit int) ([]DecisionBenchmarkRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query := `SELECT id,provider_id,provider_revision,model_name,decision_revision,results_json,total_cases,valid_cases,
		correct_cases,accuracy,invalid_rate,average_latency_ms,passed,error,created_at FROM decision_benchmark_runs`
	args := []any{}
	if providerID = strings.TrimSpace(providerID); providerID != "" {
		query += ` WHERE provider_id=?`
		args = append(args, providerID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.store.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DecisionBenchmarkRun{}
	for rows.Next() {
		var item DecisionBenchmarkRun
		var resultsJSON, createdAt string
		if err = rows.Scan(&item.ID, &item.ProviderID, &item.ProviderRevision, &item.ModelName, &item.DecisionRevision,
			&resultsJSON, &item.TotalCases, &item.ValidCases, &item.CorrectCases, &item.Accuracy, &item.InvalidRate,
			&item.AverageLatencyMS, &item.Passed, &item.Error, &createdAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(resultsJSON), &item.Results); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) SetDecisionAdmission(ctx context.Context, policy DecisionAdmissionPolicy) (DecisionAdmissionPolicy, error) {
	policy.ProviderID, policy.Actor, policy.Reason = strings.TrimSpace(policy.ProviderID), strings.TrimSpace(policy.Actor), strings.TrimSpace(policy.Reason)
	if policy.ProviderID == "" || policy.Actor == "" || policy.Reason == "" {
		return DecisionAdmissionPolicy{}, fmt.Errorf("provider, actor and reason are required")
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return DecisionAdmissionPolicy{}, err
	}
	defer tx.Rollback()
	if policy.Enabled {
		var passed bool
		var revision, decisionRevision string
		var total int
		var accuracy, invalidRate, latency float64
		if err := tx.QueryRowContext(ctx, `SELECT passed,provider_revision,decision_revision,total_cases,accuracy,invalid_rate,
			average_latency_ms FROM decision_benchmark_runs WHERE id=? AND provider_id=?`,
			policy.BenchmarkRunID, policy.ProviderID).Scan(&passed, &revision, &decisionRevision, &total, &accuracy, &invalidRate, &latency); err != nil {
			return DecisionAdmissionPolicy{}, fmt.Errorf("load admission benchmark: %w", err)
		}
		threshold := CurrentDecisionAdmissionThreshold()
		if !passed || total < threshold.MinimumCases || accuracy < threshold.MinimumAccuracy ||
			invalidRate > threshold.MaximumInvalidRate || latency > threshold.MaximumAverageLatencyMS ||
			revision != policy.ProviderRevision || decisionRevision != "bonsai-decision-v2" {
			return DecisionAdmissionPolicy{}, fmt.Errorf("provider has no passing current decision benchmark")
		}
	}
	policy.AllowedRisks, policy.UpdatedAt = []string{"read"}, time.Now().UTC()
	allowedJSON, _ := json.Marshal(policy.AllowedRisks)
	if policy.Enabled {
		if _, err = tx.ExecContext(ctx, `UPDATE decision_admission_policies SET enabled=0 WHERE enabled=1`); err != nil {
			return DecisionAdmissionPolicy{}, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO decision_admission_policies(provider_id,provider_revision,benchmark_run_id,enabled,
		allowed_risks_json,actor,reason,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(provider_id) DO UPDATE SET
		provider_revision=excluded.provider_revision,benchmark_run_id=excluded.benchmark_run_id,enabled=excluded.enabled,
		allowed_risks_json=excluded.allowed_risks_json,actor=excluded.actor,reason=excluded.reason,updated_at=excluded.updated_at`,
		policy.ProviderID, policy.ProviderRevision, policy.BenchmarkRunID, policy.Enabled, string(allowedJSON), policy.Actor,
		policy.Reason, formatTime(policy.UpdatedAt))
	if err != nil {
		return DecisionAdmissionPolicy{}, err
	}
	return policy, tx.Commit()
}

func (s *Service) DecisionAdmission(ctx context.Context, providerID string) (DecisionAdmissionPolicy, error) {
	var item DecisionAdmissionPolicy
	var allowedJSON, updatedAt string
	err := s.store.DB.QueryRowContext(ctx, `SELECT provider_id,provider_revision,benchmark_run_id,enabled,allowed_risks_json,
		actor,reason,updated_at FROM decision_admission_policies WHERE provider_id=?`, strings.TrimSpace(providerID)).
		Scan(&item.ProviderID, &item.ProviderRevision, &item.BenchmarkRunID, &item.Enabled, &allowedJSON, &item.Actor, &item.Reason, &updatedAt)
	if err != nil {
		return DecisionAdmissionPolicy{}, err
	}
	_ = json.Unmarshal([]byte(allowedJSON), &item.AllowedRisks)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return item, nil
}

func (s *Service) LatestEnabledDecisionAdmission(ctx context.Context) (DecisionAdmissionPolicy, error) {
	var providerID string
	err := s.store.DB.QueryRowContext(ctx, `SELECT provider_id FROM decision_admission_policies WHERE enabled=1 ORDER BY updated_at DESC LIMIT 1`).Scan(&providerID)
	if err != nil {
		return DecisionAdmissionPolicy{}, err
	}
	return s.DecisionAdmission(ctx, providerID)
}

package taskcoord

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
)

func (s *Service) RunDecisionBenchmark(ctx context.Context, providerID string) (taskengine.DecisionBenchmarkRun, error) {
	if s == nil || s.tasks == nil || s.providers == nil {
		return taskengine.DecisionBenchmarkRun{}, fmt.Errorf("task coordinator is unavailable")
	}
	profile, err := s.providers.Get(ctx, strings.TrimSpace(providerID))
	if err != nil {
		return taskengine.DecisionBenchmarkRun{}, err
	}
	engine, err := NewBonsaiDecision(s.providers, profile)
	if err != nil {
		return taskengine.DecisionBenchmarkRun{}, err
	}
	engine.beforeProviderRequest = func(dispatchCtx context.Context) error {
		return s.validateProviderBinding(dispatchCtx, profile)
	}
	run := taskengine.DecisionBenchmarkRun{ProviderID: profile.ID, ProviderRevision: profile.Revision,
		ModelName: profile.Model, DecisionRevision: BonsaiDecisionRevision}
	var latency int64
	for _, fixture := range taskengine.DecisionFixtures() {
		started := time.Now()
		decision, usage, decisionErr := engine.decide(ctx, fixture.State, fixture.Candidates)
		item := taskengine.DecisionBenchmarkCase{FixtureID: fixture.ID, Expected: fixture.ExpectedAction,
			LatencyMS: time.Since(started).Milliseconds(), PromptTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens}
		if decisionErr != nil {
			item.Error = decisionErr.Error()
		} else {
			item.Selected, item.Valid = decision.ActionID, true
			item.Correct = item.Selected == item.Expected
		}
		run.Results = append(run.Results, item)
		latency += item.LatencyMS
	}
	total, valid, correct := len(run.Results), 0, 0
	for _, item := range run.Results {
		if item.Valid {
			valid++
		}
		if item.Correct {
			correct++
		}
	}
	threshold := taskengine.CurrentDecisionAdmissionThreshold()
	accuracy, invalidRate, averageLatency := float64(correct)/float64(total), float64(total-valid)/float64(total), float64(latency)/float64(total)
	run.Passed = total >= threshold.MinimumCases && accuracy >= threshold.MinimumAccuracy &&
		invalidRate <= threshold.MaximumInvalidRate && averageLatency <= threshold.MaximumAverageLatencyMS
	return s.tasks.RecordDecisionBenchmark(context.WithoutCancel(ctx), run)
}

func (s *Service) SetDecisionAdmission(ctx context.Context, providerID, benchmarkRunID, actor, reason string, enabled bool) (taskengine.DecisionAdmissionPolicy, error) {
	if s == nil || s.tasks == nil || s.providers == nil {
		return taskengine.DecisionAdmissionPolicy{}, fmt.Errorf("task coordinator is unavailable")
	}
	profile, err := s.providers.Get(ctx, strings.TrimSpace(providerID))
	if err != nil {
		return taskengine.DecisionAdmissionPolicy{}, err
	}
	if !providers.IsLocalProfile(profile) {
		return taskengine.DecisionAdmissionPolicy{}, fmt.Errorf("decision admission requires a local provider profile")
	}
	if benchmarkRunID = strings.TrimSpace(benchmarkRunID); benchmarkRunID == "" {
		runs, listErr := s.tasks.ListDecisionBenchmarks(ctx, profile.ID, 1)
		if listErr != nil {
			return taskengine.DecisionAdmissionPolicy{}, listErr
		}
		if len(runs) == 0 {
			return taskengine.DecisionAdmissionPolicy{}, fmt.Errorf("run the decision benchmark before changing admission")
		}
		benchmarkRunID = runs[0].ID
	}
	return s.tasks.SetDecisionAdmission(ctx, taskengine.DecisionAdmissionPolicy{ProviderID: profile.ID,
		ProviderRevision: profile.Revision, BenchmarkRunID: benchmarkRunID, Enabled: enabled, Actor: actor, Reason: reason})
}

// DecideAdmittedReadOnly exposes the only active model-backed decision path.
// It filters the candidate set to policy-free reads and still returns a
// recommendation only; execution remains in the existing tool/policy layer.
func (s *Service) DecideAdmittedReadOnly(ctx context.Context, taskID string, expectedRevision int) (taskengine.NextActionDecision, error) {
	if s == nil || s.tasks == nil || s.providers == nil {
		return taskengine.NextActionDecision{}, fmt.Errorf("task coordinator is unavailable")
	}
	policy, err := s.tasks.LatestEnabledDecisionAdmission(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return taskengine.NextActionDecision{}, fmt.Errorf("no local decision provider is admitted")
		}
		return taskengine.NextActionDecision{}, err
	}
	profile, err := s.providers.Get(ctx, policy.ProviderID)
	if err != nil {
		return taskengine.NextActionDecision{}, err
	}
	if profile.Revision != policy.ProviderRevision {
		return taskengine.NextActionDecision{}, fmt.Errorf("admitted provider revision changed; rerun the benchmark")
	}
	baseline, err := s.tasks.DecideNextAction(ctx, taskID, expectedRevision)
	if err != nil {
		return taskengine.NextActionDecision{}, err
	}
	readCandidates := make([]taskengine.CandidateAction, 0, len(baseline.Candidates))
	for _, candidate := range baseline.Candidates {
		if candidate.Risk == "read" && !candidate.RequiresPolicy {
			readCandidates = append(readCandidates, candidate)
		}
	}
	if len(readCandidates) == 0 {
		return taskengine.NextActionDecision{}, fmt.Errorf("current task state has no admitted read-only action")
	}
	engine, err := NewBonsaiDecision(s.providers, profile)
	if err != nil {
		return taskengine.NextActionDecision{}, err
	}
	engine.beforeProviderRequest = func(dispatchCtx context.Context) error {
		return s.validateProviderBinding(dispatchCtx, profile)
	}
	started := time.Now()
	decision, err := engine.Decide(ctx, baseline.State, readCandidates)
	if err != nil {
		decision, _ = (taskengine.RuleDecision{}).Decide(ctx, baseline.State, readCandidates)
		decision.FallbackUsed = true
		decision.Reason = "local decision failed; deterministic read-only fallback: " + decision.Reason
	}
	decision.LatencyMS = time.Since(started).Milliseconds()
	return taskengine.NextActionDecision{State: baseline.State, Candidates: readCandidates, Decision: decision}, nil
}

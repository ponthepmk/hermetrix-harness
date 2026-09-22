package taskengine

import (
	"context"
	"testing"

	"hermetrix-harness/internal/store"
)

func TestDecisionFixturesMatchRuleBaseline(t *testing.T) {
	for _, fixture := range DecisionFixtures() {
		fixture := fixture
		t.Run(fixture.ID, func(t *testing.T) {
			decision, err := (RuleDecision{}).Decide(context.Background(), fixture.State, fixture.Candidates)
			if err != nil || decision.ActionID != fixture.ExpectedAction {
				t.Fatalf("decision=%+v expected=%s err=%v", decision, fixture.ExpectedAction, err)
			}
		})
	}
}

func TestDecisionAdmissionRequiresMeasuredThresholds(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := NewService(dataStore)
	results := make([]DecisionBenchmarkCase, 6)
	for i := range results {
		results[i] = DecisionBenchmarkCase{FixtureID: "case", Expected: "inspect_file", Selected: "inspect_file",
			Valid: true, Correct: true, LatencyMS: 100}
	}
	run, err := service.RecordDecisionBenchmark(ctx, DecisionBenchmarkRun{ProviderID: "local", ProviderRevision: "rev-1",
		ModelName: "bonsai", DecisionRevision: "bonsai-decision-v2", Results: results, Passed: true})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := service.SetDecisionAdmission(ctx, DecisionAdmissionPolicy{ProviderID: "local", ProviderRevision: "rev-1",
		BenchmarkRunID: run.ID, Enabled: true, Actor: "owner", Reason: "measured corpus passed"})
	if err != nil || !policy.Enabled || len(policy.AllowedRisks) != 1 || policy.AllowedRisks[0] != "read" {
		t.Fatalf("policy=%+v err=%v", policy, err)
	}
	loaded, err := service.LatestEnabledDecisionAdmission(ctx)
	if err != nil || loaded.BenchmarkRunID != run.ID {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}

	for i := range results {
		results[i].Selected, results[i].Correct = "search_repository", false
	}
	failing, err := service.RecordDecisionBenchmark(ctx, DecisionBenchmarkRun{ProviderID: "other", ProviderRevision: "rev-2",
		ModelName: "bonsai", DecisionRevision: "bonsai-decision-v2", Results: results, Passed: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SetDecisionAdmission(ctx, DecisionAdmissionPolicy{ProviderID: "other", ProviderRevision: "rev-2",
		BenchmarkRunID: failing.ID, Enabled: true, Actor: "owner", Reason: "must fail"}); err == nil {
		t.Fatal("below-threshold benchmark was admitted")
	}
}

package fidelity

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/store"
)

func testFidelityService(t *testing.T) *Service {
	t.Helper()
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	estimator := ctxcompiler.NewAdaptiveEstimator()
	compiler := ctxcompiler.NewCompiler(estimator, ctxcompiler.NewBlobSpiller(dataStore.Blobs),
		ctxcompiler.NewVerifiedCompactor(ctxcompiler.StructuredCompactor{}))
	return NewService(dataStore, compiler)
}

func TestBilingualCorpusPersistsAndRunsWithExactIntegrityMetrics(t *testing.T) {
	service := testFidelityService(t)
	ctx := context.Background()
	if err := service.EnsureDefaultCorpus(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.EnsureDefaultCorpus(ctx); err != nil {
		t.Fatal(err)
	}
	cases, err := service.ListCases(ctx)
	if err != nil || len(cases) != 2 {
		t.Fatalf("cases=%+v err=%v", cases, err)
	}
	for _, item := range cases {
		run, err := service.Run(ctx, item.ID, "certified-64k")
		if err != nil {
			t.Fatal(err)
		}
		if !run.Metrics.Passed || run.Metrics.EssentialExactRetention != 1 || run.Metrics.CausalPairSplits != 0 ||
			run.Metrics.SilentTruncations != 0 || run.FullBlobRef == "" || run.CompiledBlobRef == "" {
			t.Fatalf("run=%+v", run)
		}
	}
	runs, err := service.ListRuns(ctx, 10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
}

func TestCompactionBenchmarkAccountsForEveryFragmentAndSavesTokens(t *testing.T) {
	service := testFidelityService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	fragments := []ctxcompiler.Fragment{
		{ID: "goal", Kind: ctxcompiler.KindUserGoal, Scope: "session", Provenance: "test", Trust: "user", Version: "v1", Priority: 100, Pinned: true, Content: "Preserve EXACT_GOAL and never claim false success.", CreatedAt: now},
		{ID: "decision", Kind: ctxcompiler.KindDecision, Scope: "session", Provenance: "test", Trust: "verified", Version: "v1", Priority: 90, Content: "DECISION_ATOMIC_RENAME remains required.", CreatedAt: now},
		{ID: "call", Kind: ctxcompiler.KindToolCall, PairID: "pair", Scope: "session", Provenance: "test", Trust: "tool", Version: "v1", Priority: 80, Content: "TOOL_CALL_BOUND", CreatedAt: now},
		{ID: "result", Kind: ctxcompiler.KindToolResult, PairID: "pair", Scope: "session", Provenance: "test", Trust: "tool", Version: "v1", Priority: 80, Content: "TOOL_RESULT_REJECTED_STALE", CreatedAt: now},
	}
	for index := 0; index < 500; index++ {
		fragments = append(fragments, ctxcompiler.Fragment{ID: fmt.Sprintf("noise-%03d", index), Kind: ctxcompiler.KindConversation,
			Scope: "session", Provenance: "test", Trust: "conversation", Version: "v1", Priority: 10,
			Content: fmt.Sprintf("narrative-%03d %s", index, strings.Repeat("old reversible detail ", 45)), CreatedAt: now.Add(time.Duration(index) * time.Second)})
	}
	item, err := service.SaveCase(ctx, CaseInput{Name: "forced-compaction", Language: "mixed", BenchmarkClass: "stress",
		Fragments: fragments, Expectations: Expectations{EssentialIDs: []string{"goal"}, DecisionIDs: []string{"decision"},
			CausalPairIDs: []string{"pair"}, TaskAssertions: []string{"EXACT_GOAL", "DECISION_ATOMIC_RENAME", "TOOL_RESULT_REJECTED_STALE"},
			ForbiddenClaims: []string{"UNSUPPORTED_SUCCESS"}, MaxTaskDelta: 0.05, MaxPatchDelta: 0.05}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Run(ctx, item.ID, "compact-32k")
	if err != nil {
		t.Fatal(err)
	}
	if !run.Metrics.Passed || run.Metrics.TokensSaved <= 0 || run.Metrics.CompressionRatio >= 1 ||
		run.Metrics.EssentialExactRetention != 1 || run.Metrics.CausalPairSplits != 0 || run.Metrics.SilentTruncations != 0 {
		t.Fatalf("metrics=%+v", run.Metrics)
	}
}

func TestCorpusRejectsAssertionsWithoutSourceEvidence(t *testing.T) {
	service := testFidelityService(t)
	_, err := service.SaveCase(context.Background(), CaseInput{Name: "invalid", Language: "en", BenchmarkClass: "guard",
		Fragments:    []ctxcompiler.Fragment{{ID: "goal", Kind: ctxcompiler.KindUserGoal, Content: "real evidence"}},
		Expectations: Expectations{EssentialIDs: []string{"goal"}, TaskAssertions: []string{"invented evidence"}}})
	if err == nil {
		t.Fatal("unsupported assertion was accepted")
	}
}

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
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Case{}
	for _, item := range cases {
		byName[item.Name] = item
	}
	for _, want := range []string{"thai-skill-lifecycle", "english-tool-causality", "context-pressure"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("default corpus is missing %q; it has %v", want, byName)
		}
	}
	if len(cases) != len(byName) {
		t.Fatalf("EnsureDefaultCorpus is not idempotent: %d cases for %d names", len(cases), len(byName))
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
	if err != nil || len(runs) != len(cases) {
		t.Fatalf("runs=%d for %d cases: err=%v", len(runs), len(cases), err)
	}
}

// TestDefaultCorpusMeasuresTheCompilerUnderPressure is the property the corpus
// lacked. Both original cases are about thirty tokens: they fit whole into
// every profile, so nothing is dropped, spilled or compacted, every retention
// metric is 1 by construction and compression_ratio is exactly 1. Retention
// measured with room to spare is not retention.
func TestDefaultCorpusMeasuresTheCompilerUnderPressure(t *testing.T) {
	service := testFidelityService(t)
	ctx := context.Background()
	if err := service.EnsureDefaultCorpus(ctx); err != nil {
		t.Fatal(err)
	}
	cases, err := service.ListCases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	smallest := ctxcompiler.Compact32K()
	pressured := 0
	for _, item := range cases {
		run, err := service.Run(ctx, item.ID, smallest.Name)
		if err != nil {
			t.Fatalf("%s: %v", item.Name, err)
		}
		if run.Metrics.OriginalTokens <= smallest.ActiveBudget {
			continue
		}
		pressured++
		if run.Metrics.TokensSaved <= 0 || run.Metrics.CompressionRatio >= 1 {
			t.Fatalf("%s exceeded the active budget but nothing was compressed: %+v", item.Name, run.Metrics)
		}
		// The point of the pressure: what must survive, survives anyway.
		if !run.Metrics.Passed || run.Metrics.EssentialExactRetention != 1 ||
			run.Metrics.CausalPairSplits != 0 || run.Metrics.SilentTruncations != 0 {
			t.Fatalf("%s lost something under pressure: %+v", item.Name, run.Metrics)
		}
	}
	if pressured == 0 {
		t.Fatalf("no default case exceeds the %d-token active budget of %s; the corpus cannot fail",
			smallest.ActiveBudget, smallest.Name)
	}
	// Volume alone is not pressure. Identical filler would be removed as
	// duplicates before selection ever runs, leaving original_tokens large and
	// the compiler untested, so every filler fragment must be distinct.
	pressure := pressureCase()
	seen := map[string]bool{}
	for _, fragment := range pressure.Fragments {
		if seen[fragment.Content] {
			t.Fatalf("pressure case repeats content; deduplication would relieve the pressure before selection")
		}
		seen[fragment.Content] = true
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

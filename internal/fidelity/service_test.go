package fidelity

import (
	"context"
	"errors"
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

// pressureFiller builds deterministic conversation noise. Each fragment carries
// its own index so deduplication never quietly relieves the pressure.
func pressureFiller(count int) []ctxcompiler.Fragment {
	out := make([]ctxcompiler.Fragment, 0, count)
	for index := 0; index < count; index++ {
		out = append(out, ctxcompiler.Fragment{
			ID: fmt.Sprintf("filler-%04d", index), Kind: ctxcompiler.KindConversation, Scope: "session",
			Provenance: "fixture", Trust: "assistant", Version: "v1", Priority: 30,
			Content: fmt.Sprintf("ลำดับ %d ", index) + strings.Repeat("บันทึกการสนทนายาว ", 90)})
	}
	return out
}

// A case can declare more essential decisions than any profile can hold. When
// that happens the compiler drops most of them -- correctly, it has no other
// option -- and the run must say so. Before the verdict read DecisionRecall,
// this exact case reported Passed=true at a recall of 0.03.
//
// Mutation: drop `metrics.DecisionRecall == 1 &&` from verify() and this test
// goes green while 97% of the declared decisions are still missing.
func TestVerdictFailsWhenDeclaredDecisionsAreDropped(t *testing.T) {
	service := testFidelityService(t)
	ctx := context.Background()
	var fragments []ctxcompiler.Fragment
	var decisionIDs []string
	for index := 0; index < 600; index++ {
		id := fmt.Sprintf("decision-%04d", index)
		decisionIDs = append(decisionIDs, id)
		fragments = append(fragments, ctxcompiler.Fragment{
			ID: id, Kind: ctxcompiler.KindDecision, Scope: "session", Provenance: "fixture",
			Trust: "verified", Version: "v1", Priority: 90,
			Content: fmt.Sprintf("ข้อตกลงที่ %d ", index) + strings.Repeat("รายละเอียดการตัดสินใจ ", 40)})
	}
	fragments = append(fragments, pressureFiller(40)...)
	item, err := service.SaveCase(ctx, CaseInput{Name: "overloaded-decisions", Language: "th",
		BenchmarkClass: "instruction-retention", Fragments: fragments,
		Expectations: Expectations{DecisionIDs: decisionIDs, MaxTaskDelta: 0.05}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Run(ctx, item.ID, "compact-32k")
	if err != nil {
		t.Fatal(err)
	}
	if run.Metrics.DecisionRecall >= 1 {
		t.Fatalf("the case is meant to overflow the budget, but recall is %.3f -- "+
			"the premise is broken, not the verdict", run.Metrics.DecisionRecall)
	}
	if run.Metrics.Passed {
		t.Fatalf("run passed while retaining %.3f of the decisions it declared essential", run.Metrics.DecisionRecall)
	}
}

// Why P9-A cannot be answered by counting gold cases.
//
// Anything Pinned, KindUserGoal or KindAcceptanceCriteria lands in the pinned
// slice, and the pinned slice never drops: it either fits or Compile returns
// ErrPinnedOverflow. So EssentialExactRetention is 1 or there is no run at all
// -- it has no third value. Fifty hand-labelled cases per language would each
// report 1.00 and prove nothing about the compiler, only that their author gave
// the essential fragment a high priority.
//
// The measurable question lives in the unpinned kinds; see
// TestVerdictFailsWhenDeclaredDecisionsAreDropped.
func TestPinnedEssentialsAreRetainedExactlyOrTheCompileFails(t *testing.T) {
	service := testFidelityService(t)
	ctx := context.Background()
	essential := ctxcompiler.Fragment{ID: "goal", Kind: ctxcompiler.KindUserGoal, Scope: "session",
		Provenance: "fixture", Trust: "user", Version: "v1", Priority: 100, Pinned: true,
		Content: "ห้ามแก้ active skill โดยตรง ต้องผ่าน candidate เสมอ"}
	for _, fillers := range []int{0, 40, 800} {
		item, err := service.SaveCase(ctx, CaseInput{Name: fmt.Sprintf("pinned-under-%d", fillers),
			Language: "th", BenchmarkClass: "instruction-retention",
			Fragments:    append([]ctxcompiler.Fragment{essential}, pressureFiller(fillers)...),
			Expectations: Expectations{EssentialIDs: []string{"goal"}, MaxTaskDelta: 0.05}})
		if err != nil {
			t.Fatal(err)
		}
		run, err := service.Run(ctx, item.ID, "compact-32k")
		if err != nil {
			t.Fatalf("fillers=%d: %v", fillers, err)
		}
		if run.Metrics.EssentialExactRetention != 1 {
			t.Fatalf("fillers=%d: essential retention %.3f", fillers, run.Metrics.EssentialExactRetention)
		}
	}
	// The other half of the claim: too much pinned material is an error, never
	// a partial retention score.
	var many []ctxcompiler.Fragment
	var ids []string
	for index := 0; index < 50; index++ {
		id := fmt.Sprintf("goal-%02d", index)
		ids = append(ids, id)
		many = append(many, ctxcompiler.Fragment{ID: id, Kind: ctxcompiler.KindUserGoal, Scope: "session",
			Provenance: "fixture", Trust: "user", Version: "v1", Priority: 100, Pinned: true,
			Content: fmt.Sprintf("เป้าหมายที่ %d ", index) + strings.Repeat("ข้อกำหนดที่ห้ามหาย ", 40)})
	}
	item, err := service.SaveCase(ctx, CaseInput{Name: "pinned-overflow", Language: "th",
		BenchmarkClass: "instruction-retention", Fragments: many,
		Expectations: Expectations{EssentialIDs: ids, MaxTaskDelta: 0.05}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Run(ctx, item.ID, "compact-32k"); !errors.Is(err, ctxcompiler.ErrPinnedOverflow) {
		t.Fatalf("oversized pinned slice should refuse to compile, got %v", err)
	}
}

package context

import (
	stdcontext "context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/blob"
)

func testCompiler(t *testing.T) *Compiler {
	t.Helper()
	store, err := blob.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewCompiler(NewAdaptiveEstimator(), NewBlobSpiller(store), StructuredCompactor{})
}

func TestProfilesConsumeExactWindow(t *testing.T) {
	profiles := Profiles()
	wantTotals := []int{32768, 65536, 131072, 262144, 1048576}
	if len(profiles) != len(wantTotals) {
		t.Fatalf("profiles = %d, want %d", len(profiles), len(wantTotals))
	}
	for index, profile := range profiles {
		if err := profile.Validate(); err != nil {
			t.Fatalf("%s: %v", profile.Name, err)
		}
		if profile.Total != wantTotals[index] {
			t.Fatalf("%s total = %d, want %d", profile.Name, profile.Total, wantTotals[index])
		}
		if profile.DirectToolBudget > 8192 {
			t.Fatalf("%s lets direct tool schemas grow to %d tokens", profile.Name, profile.DirectToolBudget)
		}
	}
}

func TestEveryProfileCompilesSmallContextWithItsReserve(t *testing.T) {
	compiler := testCompiler(t)
	for _, profile := range Profiles() {
		result, err := compiler.Compile(stdcontext.Background(), Request{Profile: profile,
			Fragments: []Fragment{{ID: "goal", Kind: KindUserGoal, Pinned: true, Priority: 100,
				Content: "preserve this goal exactly"}}, WorstCaseToolBurst: 1024})
		if err != nil {
			t.Fatalf("%s: %v", profile.Name, err)
		}
		if result.Report.Profile != profile.Name || result.Report.OutputReserve != profile.OutputReserve {
			t.Fatalf("%s report = %+v", profile.Name, result.Report)
		}
		if result.Report.PredictedInput+result.Report.OutputReserve+result.Report.UncertaintyReserve > profile.Total {
			t.Fatalf("%s overflowed its window", profile.Name)
		}
	}
}

func TestDirectToolsOverflowFailsClosed(t *testing.T) {
	compiler := testCompiler(t)
	_, err := compiler.Compile(stdcontext.Background(), Request{Profile: Compact32K(), DirectTools: []ToolSpec{{Name: "oversized", Schema: strings.Repeat("ก", 3700)}}})
	if !errors.Is(err, ErrDirectToolsOverflow) {
		t.Fatalf("error = %v", err)
	}
}

func TestPinnedGoalNeverDropsSilently(t *testing.T) {
	compiler := testCompiler(t)
	_, err := compiler.Compile(stdcontext.Background(), Request{Profile: Compact32K(), Fragments: []Fragment{{
		ID: "goal", Kind: KindUserGoal, Pinned: true, Priority: 100, Content: strings.Repeat("ก", 2200),
	}}})
	if !errors.Is(err, ErrPinnedOverflow) {
		t.Fatalf("error = %v", err)
	}
}

func TestToolPairsAreSelectedOrCompactedTogether(t *testing.T) {
	compiler := testCompiler(t)
	now := time.Unix(100, 0).UTC()
	pair := []Fragment{
		{ID: "call", Kind: KindToolCall, PairID: "pair-1", Priority: 20, Content: strings.Repeat("a", 18000), CreatedAt: now},
		{ID: "result", Kind: KindToolResult, PairID: "pair-1", Priority: 20, Content: strings.Repeat("b", 18000), CreatedAt: now.Add(time.Second)},
	}
	result, err := compiler.Compile(stdcontext.Background(), Request{Profile: Compact32K(), Fragments: pair})
	if err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{}
	for _, id := range result.Report.SelectedIDs {
		selected[id] = true
	}
	if selected["call"] != selected["result"] {
		t.Fatalf("causal pair split: selected=%v", result.Report.SelectedIDs)
	}
	dropped := map[string]bool{}
	for _, id := range result.Report.DroppedIDs {
		dropped[id] = true
	}
	if dropped["call"] != dropped["result"] {
		t.Fatalf("causal pair split in dropped set: %v", result.Report.DroppedIDs)
	}
	if result.Report.Integrity.CausalPairsTotal != 1 {
		t.Fatalf("integrity report = %+v", result.Report.Integrity)
	}
}

func TestPinningOneCausalFragmentPinsTheWholePair(t *testing.T) {
	compiler := testCompiler(t)
	now := time.Unix(150, 0).UTC()
	result, err := compiler.Compile(stdcontext.Background(), Request{Profile: Compact32K(), Fragments: []Fragment{
		{ID: "call", Kind: KindToolCall, PairID: "required-pair", Pinned: true, Priority: 90, Content: "call filesystem.read", CreatedAt: now},
		{ID: "result", Kind: KindToolResult, PairID: "required-pair", Priority: 90, Content: "result receipt", CreatedAt: now.Add(time.Second)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Integrity.PinnedTotal != 2 || result.Report.Integrity.PinnedRetained != 2 || result.Report.Integrity.EssentialRetention != 1 {
		t.Fatalf("integrity = %+v", result.Report.Integrity)
	}
	if result.Report.Integrity.CausalPairsSelected != 1 {
		t.Fatalf("pair not selected intact: %+v", result.Report.Integrity)
	}
}

func TestLargeToolOutputSpillsAndGoalSurvivesCompaction(t *testing.T) {
	compiler := testCompiler(t)
	now := time.Unix(200, 0).UTC()
	goal := "รักษาเป้าหมายนี้แบบคำต่อคำและห้ามเปลี่ยน durable skill โดยไม่มี approval"
	fragments := []Fragment{
		{ID: "policy", Kind: KindPolicy, Priority: 100, Content: "proposal-only learning", CreatedAt: now},
		{ID: "goal", Kind: KindUserGoal, Pinned: true, Priority: 100, Content: goal, CreatedAt: now},
		{ID: "tool-result", Kind: KindToolResult, PairID: "tool-1", Priority: 70, Content: strings.Repeat("ผลลัพธ์เครื่องมือขนาดใหญ่ ", 1200), CreatedAt: now.Add(time.Second)},
	}
	for i := 0; i < 40; i++ {
		fragments = append(fragments, Fragment{ID: "old-" + string(rune('A'+i)), Kind: KindConversation,
			Priority: 30 + i%3, Content: "ลำดับเฉพาะ " + string(rune('A'+i)) + " " + strings.Repeat("หลักฐานบทสนทนาเก่าที่อ้างอิงได้ ", 120), CreatedAt: now.Add(time.Duration(i+2) * time.Second)})
	}
	first, err := compiler.Compile(stdcontext.Background(), Request{Profile: Compact32K(), Fragments: fragments, WorstCaseToolBurst: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Report.Spilled) != 1 {
		t.Fatalf("spilled = %d", len(first.Report.Spilled))
	}
	if first.Report.PredictedInput+first.Report.OutputReserve+first.Report.UncertaintyReserve > first.Report.TotalContext {
		t.Fatalf("compiled context overflowed: %+v", first.Report)
	}
	foundGoal, foundCheckpoint := false, false
	for _, fragment := range first.Fragments {
		if fragment.ID == "goal" && fragment.Content == goal {
			foundGoal = true
		}
		if fragment.Kind == KindCheckpoint {
			foundCheckpoint = true
		}
	}
	if !foundGoal {
		t.Fatal("pinned goal was not preserved exactly")
	}
	if !foundCheckpoint {
		t.Fatal("expected structured checkpoint")
	}
	second, err := compiler.Compile(stdcontext.Background(), Request{Profile: Compact32K(), Fragments: fragments, WorstCaseToolBurst: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Report.SelectedIDs, second.Report.SelectedIDs) {
		t.Fatalf("selection is not deterministic\n%v\n%v", first.Report.SelectedIDs, second.Report.SelectedIDs)
	}
	if checkpointContent(first) != checkpointContent(second) {
		t.Fatal("checkpoint content is not deterministic")
	}
}

func TestEstimatorIsConservativeForThaiAndLearnsReportedUsage(t *testing.T) {
	estimator := NewAdaptiveEstimator()
	text := "ทดสอบภาษาไทยแบบไม่มีช่องว่าง"
	before := estimator.Count(text)
	if before < len([]rune(text)) {
		t.Fatalf("Thai estimate %d < rune count %d", before, len([]rune(text)))
	}
	estimator.Observe(before, before*2)
	after := estimator.Count(text)
	if after <= before {
		t.Fatalf("calibration did not increase estimate: %d -> %d", before, after)
	}
}

func checkpointContent(result Compiled) string {
	for _, fragment := range result.Fragments {
		if fragment.Kind == KindCheckpoint {
			return fragment.Content
		}
	}
	return ""
}

// TestEveryTokenIsAccountedForWhenSpillDropAndCompactionAllHappen pins the
// ledger identity on a compile where all four disposals actually occur, so a
// balance cannot be reached by every term being zero.
//
// The regression it guards: a live session reported 34,038 tokens in, 10,794
// selected, 0 compacted and 0 dropped. Spill had taken the other 23,244, but
// the report had no field for that, so the missing two thirds were invisible
// and the compactor looked broken.
func TestEveryTokenIsAccountedForWhenSpillDropAndCompactionAllHappen(t *testing.T) {
	compiler := testCompiler(t)
	now := time.Unix(300, 0).UTC()
	duplicated := Fragment{ID: "dup", Kind: KindConversation, Priority: 40,
		Content: strings.Repeat("บทสนทนาซ้ำที่ต้องถูกตัดออกก่อนนับ ", 40), CreatedAt: now}
	fragments := []Fragment{
		{ID: "goal", Kind: KindUserGoal, Pinned: true, Priority: 100, Content: "เป้าหมายที่ต้องคงไว้", CreatedAt: now},
		{ID: "huge-tool", Kind: KindToolResult, PairID: "pair-1", Priority: 70,
			Content: strings.Repeat("ผลลัพธ์เครื่องมือขนาดใหญ่มาก ", 1500), CreatedAt: now.Add(time.Second)},
		duplicated, duplicated,
	}
	for i := 0; i < 60; i++ {
		fragments = append(fragments, Fragment{ID: "hist-" + strconv.Itoa(i), Kind: KindConversation,
			Priority: 30 + i%3, Content: "ลำดับ " + strconv.Itoa(i) + " " + strings.Repeat("ประวัติที่ยาวพอจะล้น active slice ", 100),
			CreatedAt: now.Add(time.Duration(i+2) * time.Second)})
	}
	result, err := compiler.Compile(stdcontext.Background(), Request{Profile: Compact32K(), Fragments: fragments, WorstCaseToolBurst: 1024})
	if err != nil {
		t.Fatal(err)
	}
	report := result.Report
	for name, value := range map[string]int{
		"deduplicated": report.DeduplicatedTokens, "spilled": report.SpilledTokens,
		"dropped": report.DroppedTokens, "compacted": report.CompactedTokens,
	} {
		if value <= 0 {
			t.Fatalf("%s tokens = %d; the ledger would balance trivially without this term", name, value)
		}
	}
	if report.UnaccountedTokens != 0 {
		t.Fatalf("unaccounted = %d, want 0; report = %+v", report.UnaccountedTokens, report)
	}
	balance := report.DeduplicatedTokens + report.SpilledTokens +
		(report.SelectedTokens - report.CompactedTokens) + report.DroppedTokens
	if balance != report.OriginalTokens {
		t.Fatalf("ledger terms sum to %d, original = %d", balance, report.OriginalTokens)
	}
}

// TestLedgerRefusesAReportThatLosesTokens drives the failure side directly:
// each case is a report where exactly one disposal went uncounted, which is how
// this defect appeared in production -- spill moved tokens and no field
// recorded it.
func TestLedgerRefusesAReportThatLosesTokens(t *testing.T) {
	cases := []struct {
		name            string
		report          Report
		wantUnaccounted int
	}{
		{"spill went uncounted", Report{OriginalTokens: 34038, SelectedTokens: 10794}, 23244},
		{"dedup went uncounted", Report{OriginalTokens: 1000, SpilledTokens: 400, SelectedTokens: 500}, 100},
		{"drop went uncounted", Report{OriginalTokens: 800, SelectedTokens: 300}, 500},
		{"checkpoint counted as input", Report{OriginalTokens: 500, SelectedTokens: 600, CompactedTokens: 0, DroppedTokens: 0}, -100},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			report := testCase.report
			err := report.reconcile()
			if err == nil {
				t.Fatalf("reconcile accepted a report missing %d tokens", testCase.wantUnaccounted)
			}
			if !errors.Is(err, ErrLedgerImbalance) {
				t.Fatalf("error = %v, want ErrLedgerImbalance", err)
			}
			if report.UnaccountedTokens != testCase.wantUnaccounted {
				t.Fatalf("unaccounted = %d, want %d", report.UnaccountedTokens, testCase.wantUnaccounted)
			}
			if !strings.Contains(err.Error(), "unaccounted=") {
				t.Fatalf("error does not report the size of the hole: %v", err)
			}
		})
	}
}

// TestLedgerAcceptsABalancedReport keeps the check from being a blanket reject.
func TestLedgerAcceptsABalancedReport(t *testing.T) {
	report := Report{OriginalTokens: 34038, DeduplicatedTokens: 1000, DeduplicatedFragments: 2,
		SpilledTokens: 22244, Spilled: []SpillReceipt{{Ref: "a"}},
		SelectedTokens: 11294, CompactedTokens: 500, DroppedTokens: 0}
	if err := report.reconcile(); err != nil {
		t.Fatalf("balanced report rejected: %v", err)
	}
	if report.UnaccountedTokens != 0 {
		t.Fatalf("unaccounted = %d, want 0", report.UnaccountedTokens)
	}
}

// driftingEstimator recalibrates in the middle of a compile. AdaptiveEstimator
// really can do this -- Observe changes the multiplier, and a concurrent turn
// reporting its usage is enough to move it -- so the ledger would silently
// stop balancing rather than the compiler noticing.
type driftingEstimator struct {
	inner  Estimator
	calls  int
	driftA int
}

func (e *driftingEstimator) Count(text string) int {
	e.calls++
	count := e.inner.Count(text)
	if e.calls > e.driftA {
		return count / 2
	}
	return count
}

// TestCompileRefusesToReturnAReportItCannotReconcile drives the gate inside
// Compile, not the arithmetic beside it: when a mid-compile change makes the
// input and output totals disagree, the compile must fail rather than hand back
// a report with a hole in it.
func TestCompileRefusesToReturnAReportItCannotReconcile(t *testing.T) {
	store, err := blob.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(500, 0).UTC()
	fragments := []Fragment{
		{ID: "goal", Kind: KindUserGoal, Pinned: true, Priority: 100, Content: "เป้าหมายที่ต้องคงไว้", CreatedAt: now},
	}
	for i := 0; i < 30; i++ {
		fragments = append(fragments, Fragment{ID: "hist-" + strconv.Itoa(i), Kind: KindConversation,
			Priority: 40, Content: "ลำดับ " + strconv.Itoa(i) + " " + strings.Repeat("ประวัติบทสนทนา ", 60),
			CreatedAt: now.Add(time.Duration(i) * time.Second)})
	}
	steady := NewCompiler(NewAdaptiveEstimator(), NewBlobSpiller(store), StructuredCompactor{})
	if _, err := steady.Compile(stdcontext.Background(), Request{Profile: Compact32K(), Fragments: fragments}); err != nil {
		t.Fatalf("baseline compile should succeed: %v", err)
	}
	drifting := NewCompiler(&driftingEstimator{inner: NewAdaptiveEstimator(), driftA: len(fragments)},
		NewBlobSpiller(store), StructuredCompactor{})
	_, err = drifting.Compile(stdcontext.Background(), Request{Profile: Compact32K(), Fragments: fragments})
	if err == nil {
		t.Fatalf("compile returned a report whose tokens do not add up")
	}
	if !errors.Is(err, ErrLedgerImbalance) {
		t.Fatalf("error = %v, want ErrLedgerImbalance", err)
	}
}

// TestLedgerRejectsABalancedTotalWithNoWitness covers the case the sum alone
// cannot catch: the arithmetic closes, but a term claims work that leaves no
// trace anywhere else in the report.
func TestLedgerRejectsABalancedTotalWithNoWitness(t *testing.T) {
	cases := []struct {
		name   string
		report Report
		want   string
	}{
		{"spill claimed with no receipt",
			Report{OriginalTokens: 1000, SpilledTokens: 400, SelectedTokens: 600},
			"attributed to spill"},
		{"dedup claimed with no fragment removed",
			Report{OriginalTokens: 1000, DeduplicatedTokens: 400, SelectedTokens: 600},
			"attributed to deduplication"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			report := testCase.report
			err := report.reconcile()
			if report.UnaccountedTokens != 0 {
				t.Fatalf("precondition: the total should balance, unaccounted = %d", report.UnaccountedTokens)
			}
			if err == nil {
				t.Fatal("reconcile accepted a term with no witness")
			}
			if !errors.Is(err, ErrLedgerImbalance) || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want ErrLedgerImbalance mentioning %q", err, testCase.want)
			}
		})
	}
}

// TestScaledEstimatorDoesNotMoveWhileACompileRuns is the property the shared
// adaptive estimator could not offer. Observe writes to one mutable float used
// by every session, so a compile could be measured with a different ruler than
// the one before it in the same turn.
func TestScaledEstimatorDoesNotMoveWhileACompileRuns(t *testing.T) {
	text := "บันทึกการสนทนาที่ยาวพอจะวัดได้"
	base := ScaledEstimator(1).Count(text)
	if base <= 0 {
		t.Fatal("baseline count is zero")
	}
	if doubled := ScaledEstimator(2).Count(text); doubled < base*2-1 || doubled > base*2+1 {
		t.Fatalf("multiplier 2 gave %d for a base of %d", doubled, base)
	}
	adaptive := NewAdaptiveEstimator()
	before := adaptive.Count(text)
	adaptive.Observe(1000, 2000)
	if adaptive.Count(text) == before {
		t.Fatal("precondition: the adaptive estimator is supposed to move when observed")
	}
	fixed := ScaledEstimator(1)
	stable := fixed.Count(text)
	adaptive.Observe(1000, 500)
	if fixed.Count(text) != stable {
		t.Fatal("a fixed-scale estimator changed after an unrelated observation")
	}
}

// TestZeroScaleFallsBackToUnscaled keeps a provider that has never been
// calibrated from measuring everything as nothing.
func TestZeroScaleFallsBackToUnscaled(t *testing.T) {
	text := "unmeasured provider"
	if got, want := ScaledEstimator(0).Count(text), ScaledEstimator(1).Count(text); got != want {
		t.Fatalf("zero scale counted %d, want the unscaled %d", got, want)
	}
}

// TestWithEstimatorLeavesTheOriginalAlone matters because concurrent turns on
// different models share one compiler.
func TestWithEstimatorLeavesTheOriginalAlone(t *testing.T) {
	compiler := testCompiler(t)
	now := time.Unix(600, 0).UTC()
	fragments := []Fragment{{ID: "goal", Kind: KindUserGoal, Pinned: true, Priority: 100,
		Content: strings.Repeat("เนื้อหาที่ใช้วัด ", 30), CreatedAt: now}}
	request := Request{Profile: Compact32K(), Fragments: fragments}
	first, err := compiler.Compile(stdcontext.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	scaled := compiler.WithEstimator(ScaledEstimator(2))
	doubled, err := scaled.Compile(stdcontext.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if doubled.Report.SelectedTokens <= first.Report.SelectedTokens {
		t.Fatalf("scaled compile measured %d, unscaled %d", doubled.Report.SelectedTokens, first.Report.SelectedTokens)
	}
	again, err := compiler.Compile(stdcontext.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if again.Report.SelectedTokens != first.Report.SelectedTokens {
		t.Fatalf("the original compiler was mutated: %d then %d",
			first.Report.SelectedTokens, again.Report.SelectedTokens)
	}
	if compiler.WithEstimator(nil) != compiler {
		t.Fatal("WithEstimator(nil) should keep the existing estimator")
	}
}

// TestTheReservedBurstIsNotPartOfThePredictedPrompt separates the two numbers
// that were previously one. Comparing the budget against a provider's bill made
// the error band a function of context size: eighteen consecutive live requests
// drifted from -51.7% to -27.9% purely because the fixed reserve was being
// diluted by a growing prompt.
func TestTheReservedBurstIsNotPartOfThePredictedPrompt(t *testing.T) {
	compiler := testCompiler(t)
	now := time.Unix(700, 0).UTC()
	fragments := []Fragment{{ID: "goal", Kind: KindUserGoal, Pinned: true, Priority: 100,
		Content: strings.Repeat("เนื้อหาที่ต้องวัด ", 40), CreatedAt: now}}
	const burst = 2048
	withBurst, err := compiler.Compile(stdcontext.Background(),
		Request{Profile: Compact32K(), Fragments: fragments, WorstCaseToolBurst: burst})
	if err != nil {
		t.Fatal(err)
	}
	if withBurst.Report.PredictedInput != withBurst.Report.PredictedPrompt+burst {
		t.Fatalf("predicted_input %d is not prompt %d plus the %d reserve",
			withBurst.Report.PredictedInput, withBurst.Report.PredictedPrompt, burst)
	}
	noBurst, err := compiler.Compile(stdcontext.Background(),
		Request{Profile: Compact32K(), Fragments: fragments})
	if err != nil {
		t.Fatal(err)
	}
	// The reserve must not change what the request is expected to cost.
	if noBurst.Report.PredictedPrompt != withBurst.Report.PredictedPrompt {
		t.Fatalf("the reserve moved the prompt prediction: %d without, %d with",
			noBurst.Report.PredictedPrompt, withBurst.Report.PredictedPrompt)
	}
	if withBurst.Report.PredictedPrompt == 0 {
		t.Fatal("prompt prediction is zero")
	}
}

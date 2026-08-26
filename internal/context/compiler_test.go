package context

import (
	stdcontext "context"
	"errors"
	"fmt"
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

// TestScriptEstimatorChargesTheLearnedRate covers the estimator half of the
// change. The old rule charged one token per non-ASCII character, which no
// tokenizer does; a Thai-dominant context came out about thirty percent high.
func TestScriptEstimatorChargesTheLearnedRate(t *testing.T) {
	thai := strings.Repeat("บัญชีภาษี", 100)
	assumed := ScriptEstimator{NonASCIIRate: 1, Scale: 1}.Count(thai)
	learned := ScriptEstimator{NonASCIIRate: 0.5, Scale: 1}.Count(thai)
	if learned >= assumed {
		t.Fatalf("halving the rate did not lower the estimate: %d then %d", assumed, learned)
	}
	if ratio := float64(learned) / float64(assumed); ratio < 0.45 || ratio > 0.55 {
		t.Fatalf("halving the rate changed the estimate by %.2f, want about half", ratio)
	}
	english := "round every amount half up and reconcile the total"
	englishLearned := ScriptEstimator{NonASCIIRate: 0.5, Scale: 1}.Count(english)
	englishAssumed := ScriptEstimator{NonASCIIRate: 1, Scale: 1}.Count(english)
	if englishLearned != englishAssumed {
		t.Fatalf("the script rate moved an ASCII-only estimate: %d then %d", englishAssumed, englishLearned)
	}
	// An uncalibrated provider must reserve conservatively rather than overflow.
	zeroRate := ScriptEstimator{}.Count(thai)
	conservative := ScriptEstimator{NonASCIIRate: DefaultNonASCIIRate, Scale: 1}.Count(thai)
	if zeroRate != conservative {
		t.Fatalf("a zero rate counted %d, want the conservative default %d", zeroRate, conservative)
	}
	if scaled := (ScriptEstimator{NonASCIIRate: 0.5, Scale: 2}).Count(thai); scaled <= learned {
		t.Fatalf("the residual scale had no effect: %d then %d", learned, scaled)
	}
}

func TestHeuristicPartsSeparatesTheTwoRules(t *testing.T) {
	ascii, nonASCII := HeuristicParts("abcd efgh")
	if nonASCII != 0 || ascii == 0 {
		t.Fatalf("ASCII text gave ascii=%d nonASCII=%d", ascii, nonASCII)
	}
	ascii, nonASCII = HeuristicParts("บัญชี")
	if nonASCII != len([]rune("บัญชี")) {
		t.Fatalf("nonASCII = %d, want one per character", nonASCII)
	}
	if ascii != 0 {
		t.Fatalf("Thai text produced %d ASCII tokens", ascii)
	}
}

// TestTransportCostPricesTheChatTemplate covers the compiler charging what the
// provider bills before any content: a wrapper per message and a constant per
// request. Neither is context, so both stay out of the ledger and enter only
// the prediction.
func TestTransportCostPricesTheChatTemplate(t *testing.T) {
	compiler := testCompiler(t)
	now := time.Unix(800, 0).UTC()
	fragments := []Fragment{
		{ID: "policy", Kind: KindPolicy, Priority: 100, Content: "proposal-only learning", CreatedAt: now},
		{ID: "identity", Kind: KindIdentity, Priority: 100, Content: "you are a tool", CreatedAt: now},
		{ID: "goal", Kind: KindUserGoal, Pinned: true, Priority: 100, Content: "รักษาเป้าหมายนี้", CreatedAt: now},
	}
	for i := 0; i < 6; i++ {
		fragments = append(fragments, Fragment{ID: "turn-" + strconv.Itoa(i), Kind: KindConversation,
			Priority: 50, Content: "ข้อความที่ " + strconv.Itoa(i), CreatedAt: now.Add(time.Duration(i) * time.Second)})
	}
	unmeasured, err := compiler.Compile(stdcontext.Background(), Request{Profile: Compact32K(), Fragments: fragments})
	if err != nil {
		t.Fatal(err)
	}
	// A provider whose template has never been measured is charged nothing: a
	// guessed overhead is subtracted from usable context on every request.
	if unmeasured.Report.TransportTokens != 0 {
		t.Fatalf("an unmeasured provider was charged %d transport tokens", unmeasured.Report.TransportTokens)
	}
	measured, err := compiler.Compile(stdcontext.Background(), Request{Profile: Compact32K(), Fragments: fragments,
		MessageOverhead: 9, RequestOverhead: 43})
	if err != nil {
		t.Fatal(err)
	}
	// The two system-kind fragments join one message; the goal and six turns are
	// seven more. Eight messages at nine, plus the constant.
	if want := 43 + 9*8; measured.Report.TransportTokens != want {
		t.Fatalf("transport = %d, want %d", measured.Report.TransportTokens, want)
	}
	if measured.Report.PredictedPrompt != unmeasured.Report.PredictedPrompt+measured.Report.TransportTokens {
		t.Fatalf("the transport cost did not reach the prediction: %d and %d",
			unmeasured.Report.PredictedPrompt, measured.Report.PredictedPrompt)
	}
	// It is transport, not content: the ledger still balances what came in.
	if measured.Report.UnaccountedTokens != 0 {
		t.Fatalf("transport tokens entered the content ledger: %+v", measured.Report)
	}
	if measured.Report.SelectedTokens != unmeasured.Report.SelectedTokens {
		t.Fatalf("transport changed the selected content total: %d then %d",
			unmeasured.Report.SelectedTokens, measured.Report.SelectedTokens)
	}
}

// Causal-pair integrity is the one half of the Phase 9 gate with a real
// subject in the field: 5,933 pairs across 660 compiled snapshots from the
// driven session, zero splits. That could have been luck -- pairs that always
// happened to fit. It is not.
//
// Within a slice, makeUnits keys a pair as one unit, so selection drops or
// keeps both halves together. Across slices there is no such key, and that gap
// is closed by a different mechanism: evaluateIntegrity refuses the compile
// outright rather than returning a context whose tool call has lost its result.
func TestCausalPairsSurviveTogetherOrTheCompileRefuses(t *testing.T) {
	profile, _ := ProfileByName("compact-32k")
	compiler := NewCompiler(NewAdaptiveEstimator(), nil, NewVerifiedCompactor(StructuredCompactor{}))

	t.Run("same slice, dropped together under pressure", func(t *testing.T) {
		fragments := []Fragment{
			// The call carries the weight; only tool results are spilled, so
			// the unit stays big enough to be squeezed out as a whole.
			{ID: "call", Kind: KindToolCall, PairID: "pair", Scope: "session", Provenance: "f",
				Trust: "tool", Version: "v1", Priority: 20, Content: strings.Repeat("เรียกเครื่องมือ ", 1500)},
			{ID: "result", Kind: KindToolResult, PairID: "pair", Scope: "session", Provenance: "f",
				Trust: "tool", Version: "v1", Priority: 20, Content: "ผลลัพธ์สั้น"},
		}
		for index := 0; index < 40; index++ {
			fragments = append(fragments, Fragment{ID: fmt.Sprintf("noise-%02d", index),
				Kind: KindConversation, Scope: "session", Provenance: "f", Trust: "assistant",
				Version: "v1", Priority: 90, Content: fmt.Sprintf("ลำดับ %d ", index) +
					strings.Repeat("บทสนทนาที่สำคัญกว่า ", 300)})
		}
		compiled, err := compiler.Compile(stdcontext.Background(), Request{Profile: profile, Fragments: fragments})
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		kept := 0
		for _, fragment := range compiled.Fragments {
			if fragment.PairID == "pair" {
				kept++
			}
		}
		if kept == 1 {
			t.Fatalf("one half of the pair survived alone")
		}
		if kept == 2 {
			t.Fatalf("the pair was never at risk; the premise is broken, not the guarantee")
		}
		if compiled.Report.CompressionRatio >= 1 {
			t.Fatalf("nothing was under pressure: compression ratio %.3f", compiled.Report.CompressionRatio)
		}
	})

	t.Run("across slices, the compile refuses", func(t *testing.T) {
		// A pair whose halves land in different slices has no shared unit key.
		// half-a cannot fit in SkillProjectBudget beside a higher-priority
		// instruction; half-b is small and the active slice has room.
		fragments := []Fragment{
			{ID: "half-a", Kind: KindProjectInstruction, PairID: "p", Scope: "session", Provenance: "f",
				Trust: "user", Version: "v1", Priority: 50, Content: strings.Repeat("ครึ่งแรกของคู่ ", 700)},
			{ID: "half-b", Kind: KindToolResult, PairID: "p", Scope: "session", Provenance: "f",
				Trust: "tool", Version: "v1", Priority: 50, Content: "ผลลัพธ์ครึ่งหลังของคู่"},
			{ID: "hog", Kind: KindProjectInstruction, Scope: "session", Provenance: "f",
				Trust: "user", Version: "v1", Priority: 90, Content: strings.Repeat("คำสั่งสำคัญกว่า ", 800)},
		}
		_, err := compiler.Compile(stdcontext.Background(), Request{Profile: profile, Fragments: fragments})
		if err == nil {
			t.Fatal("a split pair compiled successfully")
		}
		if !strings.Contains(err.Error(), "causal pair p was split") {
			t.Fatalf("wrong failure: %v", err)
		}
	})
}

// TestTheCheckpointSaysWhatItLostAndHowToGetItBack closes the other half of
// the retrieval problem.
//
// context_search made the history readable. It changes nothing on its own: a
// model that does not know something is missing does not go looking. That is
// R-14 in miniature -- skill_search was called 165 times across the driven
// corpus and never once on a turn where a relevant Skill existed, because
// nothing told the model there was one.
//
// So the checkpoint has to declare its own lossiness: that its lines are
// extracts with the middles removed, that some exchanges are absent entirely,
// and what to call to read them in full.
func TestTheCheckpointSaysWhatItLostAndHowToGetItBack(t *testing.T) {
	profile, _ := ProfileByName("compact-32k")
	compiler := NewCompiler(NewAdaptiveEstimator(), nil, NewVerifiedCompactor(StructuredCompactor{}))
	fragments := []Fragment{
		{ID: "goal", Kind: KindUserGoal, Scope: "session", Provenance: "f", Trust: "user",
			Version: "v1", Priority: 100, Pinned: true, Content: "ถามเรื่องการปัดเศษ"},
	}
	for index := 0; index < 200; index++ {
		fragments = append(fragments, Fragment{ID: fmt.Sprintf("event:e%03d", index),
			Kind: KindConversation, Scope: "session", Provenance: "assistant", Trust: "assistant",
			Version: "v1", Priority: 70, Content: fmt.Sprintf("ลำดับ %d ", index) +
				strings.Repeat("บันทึกการสนทนายาว ", 120)})
	}
	compiled, err := compiler.Compile(stdcontext.Background(), Request{Profile: profile, Fragments: fragments})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := ""
	for _, fragment := range compiled.Fragments {
		if fragment.Kind == KindCheckpoint {
			checkpoint = fragment.Content
		}
	}
	if checkpoint == "" {
		t.Fatal("premise broken: nothing was compacted, so there is no checkpoint to inspect")
	}
	for _, want := range []string{"extracts", "omits the middle", "context_search", "not shown here at all"} {
		if !strings.Contains(checkpoint, want) {
			t.Fatalf("the checkpoint does not say %q:\n%s", want, headOf(checkpoint))
		}
	}
	// The exemption must stay narrow: everything that is not the checkpoint
	// describing itself still needs an evidence marker, or a summariser could
	// assert whatever it liked by prefixing a line.
	for _, line := range strings.Split(checkpoint, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || isCheckpointPreamble(line) {
			continue
		}
		if !strings.HasPrefix(line, "- [") {
			t.Fatalf("a checkpoint line makes a claim with no evidence marker: %q", line)
		}
	}
}

func headOf(value string) string {
	runes := []rune(value)
	if len(runes) > 400 {
		return string(runes[:400])
	}
	return value
}

// TestAThaiFocusFindsItsOwnPassage covers the branch an unspaced script depends
// on entirely.
//
// textmatch.Terms splits words on whitespace, so a Thai goal is one enormous
// token that matches nothing: "หมายเลขแผนงานที่ตกลงกันไว้คืออะไร" never appears
// inside an exchange even when the exchange is about exactly that. Word
// matching alone left a fact stated in the question's own words reachable only
// 62% of the time when it sat mid-message -- in Thai, which is the language
// this system is used in.
func TestAThaiFocusFindsItsOwnPassage(t *testing.T) {
	const marker = "PLAN-1061"
	focus := "หมายเลขแผนงานที่ตกลงกันไว้คืออะไร ตอบเฉพาะหมายเลข"
	pad := strings.Repeat("รายละเอียดประกอบการทำงานที่ไม่มีคำตอบอยู่ในนั้น ", 120)
	content := pad + " สรุปที่ประชุม: ใช้หมายเลขแผนงาน " + marker + " และให้ย้อนกลับอัตโนมัติ " + pad

	// Premise: no whole word of the focus appears, so word matching has nothing.
	if len(focusTermsPresent(content, focus)) != 0 {
		t.Fatal("premise broken: a whole focus word appears, so this is not the Thai path")
	}
	excerpt := focusedExcerpt(content, focus, 520)
	if !strings.Contains(excerpt, marker) {
		t.Fatalf("the Thai focus did not find its own passage: %.120q", excerpt)
	}
	// And the window must not be the head-and-tail default, or it found the
	// passage by luck rather than by looking.
	if strings.HasPrefix(excerpt, "รายละเอียด") {
		t.Fatal("the excerpt is the head-and-tail default, not a focused window")
	}
}

// Relevance only gets to override the positional default when it has something
// to say. Two pieces of Thai prose share trigrams whatever they are about, so a
// weak best window is background similarity rather than signal -- and
// committing to it cost a whole cell of the task corpus, taking facts at the
// end of a message from fully reachable to not reachable at all.
func TestAWeakFocusKeepsBothEnds(t *testing.T) {
	const marker = "PLAN-1061"
	focus := "หมายเลขแผนงานที่ตกลงกันไว้คืออะไร ตอบเฉพาะหมายเลข"
	pad := strings.Repeat("รายละเอียดประกอบการทำงานที่ไม่มีคำตอบอยู่ในนั้น ", 120)
	// The fact is stated in words the question does not use, and sits at the end.
	unrelated := pad + " ที่คุยกันไว้คือเคาะรหัสอ้างอิงรอบนำขึ้นระบบเป็น " + marker

	excerpt := focusedExcerpt(unrelated, focus, 520)
	if !strings.Contains(excerpt, marker) {
		t.Fatalf("a weakly matched focus discarded the tail: %.120q", excerpt)
	}
	// Strong signal still wins, or the floor would have disabled the feature.
	related := pad + " สรุปที่ประชุม: ใช้หมายเลขแผนงาน " + marker + " และให้ย้อนกลับอัตโนมัติ " + pad
	if window, ok := densestTrigramWindow(related, focus, 520); !ok ||
		!strings.Contains(window, marker) {
		t.Fatalf("a strongly matched focus was refused: ok=%v", ok)
	}
}

// The stepped window search stops before the end of the content, so a fact in
// the final stride is never scanned. The head-and-tail fallback happens to keep
// it anyway, which is why the corpus grid does not notice -- but it means the
// compactor spends its whole budget on both ends when it could have spent it on
// the passage that answers the question.
//
// This asserts what the tail scan actually buys: a focused window rather than
// the default, for a fact at the very end of a long fragment.
func TestTheWindowSearchReachesTheEndOfTheContent(t *testing.T) {
	focus := "หมายเลขแผนงานที่ตกลงกันไว้คืออะไร ตอบเฉพาะหมายเลข"
	pad := strings.Repeat("รายละเอียดประกอบการทำงานที่ไม่มีคำตอบอยู่ในนั้น ", 120)
	content := pad + " สรุปที่ประชุม: ใช้หมายเลขแผนงาน PLAN-1061"

	window, ok := densestTrigramWindow(content, focus, 520)
	if !ok {
		t.Fatal("a strongly matched fact in the final stride was not found by the window search")
	}
	if !strings.Contains(window, "PLAN-1061") {
		t.Fatalf("the window does not contain the fact: %.120q", window)
	}
}

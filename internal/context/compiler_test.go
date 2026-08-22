package context

import (
	stdcontext "context"
	"errors"
	"reflect"
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

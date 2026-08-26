package agent

import (
	"fmt"
	"strings"
	"testing"

	"hermetrix-harness/internal/textmatch"
	"time"

	ctxcompiler "hermetrix-harness/internal/context"
)

// TestContextSearchRecoversWhatCompactionDestroyed is the reason this primitive
// exists.
//
// The compactor keeps 360 runes from each end of a long fragment and drops
// what is between; measured across 5,649 real conversation fragments, a fact at
// a uniformly random position falls in that gap 34.5% of the time. Before this
// tool the session history was the one store with no way to read it back, so a
// fact that landed in the gap was simply gone.
//
// The test asserts both halves: that compaction really does destroy the fact,
// and that the search really does return it. Without the first half it would
// pass against a compiler that never compacted anything.
func TestContextSearchRecoversWhatCompactionDestroyed(t *testing.T) {
	const marker = "ROUND_HALF_UP_4096"
	pad := strings.Repeat("รายละเอียดประกอบการทำงาน ", 200)
	buried := pad + " ตกลงกันว่าใช้ " + marker + " เสมอ " + pad

	events := []Event{{ID: "event_buried", TurnID: "t1", EventKind: "message", Role: "assistant",
		Content: buried, CreatedAt: time.Now().UTC()}}
	fragments := []ctxcompiler.Fragment{
		{ID: "goal", Kind: ctxcompiler.KindUserGoal, Scope: "session", Provenance: "fixture",
			Trust: "user", Version: "v1", Priority: 100, Pinned: true, Content: "ถามเรื่องการปัดเศษ"},
	}
	// The buried fragment goes in the middle of the history, not at the front.
	// selectWithin sorts by priority and keeps insertion order among equals, so
	// a large fragment placed early is selected verbatim and never compacted --
	// which is how the first version of this fixture managed to prove nothing.
	for index := 0; index < 120; index++ {
		if index == 60 {
			fragments = append(fragments, ctxcompiler.Fragment{ID: "event:event_buried",
				Kind: ctxcompiler.KindConversation, Scope: "session", Provenance: "assistant",
				Trust: "assistant", Version: "v1", Priority: 70, Content: buried})
			continue
		}
		fragments = append(fragments, ctxcompiler.Fragment{ID: fmt.Sprintf("noise-%02d", index),
			Kind: ctxcompiler.KindConversation, Scope: "session", Provenance: "assistant",
			Trust: "assistant", Version: "v1", Priority: 70,
			Content: fmt.Sprintf("ลำดับ %d ", index) + strings.Repeat("บันทึกการสนทนายาว ", 120)})
	}

	profile, ok := ctxcompiler.ProfileByName("compact-32k")
	if !ok {
		t.Fatal("missing profile")
	}
	compiler := ctxcompiler.NewCompiler(ctxcompiler.NewAdaptiveEstimator(), nil,
		ctxcompiler.NewVerifiedCompactor(ctxcompiler.StructuredCompactor{}))
	compiled, err := compiler.Compile(t.Context(), ctxcompiler.Request{Profile: profile, Fragments: fragments})
	if err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, fragment := range compiled.Fragments {
		body += "\n" + fragment.Content
	}
	// Premise: the fact really is gone from what the model would receive.
	if strings.Contains(body, marker) {
		t.Fatal("compaction kept the fact, so this test proves nothing about recovering it")
	}

	// The point: the event log still has it, and the search finds it.
	results := searchEvents(events, marker, 5, nil)
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if !strings.Contains(results[0].Content, marker) {
		t.Fatalf("the recovered text does not contain the fact: %q", results[0].Content)
	}
}

// A Thai query has to reach Thai content. The Skill catalog needed the same
// branch, and without it a query matches nothing short of the text repeated
// verbatim -- which for retrieving your own history would make the tool
// useless to a Thai-speaking user.
func TestContextSearchAnswersAThaiQuery(t *testing.T) {
	events := []Event{
		{ID: "e1", EventKind: "message", Role: "user", Content: "ให้ปัดเศษสตางค์แบบครึ่งขึ้นเสมอ",
			CreatedAt: time.Now().UTC()},
		{ID: "e2", EventKind: "message", Role: "user", Content: "เรื่องอื่นที่ไม่เกี่ยวข้องเลย",
			CreatedAt: time.Now().UTC()},
	}
	results := searchEvents(events, "ปัดเศษสตางค์", 5, nil)
	if len(results) == 0 || results[0].EventID != "e1" {
		t.Fatalf("a Thai query did not reach Thai content: %+v", results)
	}
}

// Bookkeeping events are not conversation. Returning them would spend the
// model's attention on the harness rather than on the work.
func TestContextSearchReturnsOnlyWhatWasSaidOrObserved(t *testing.T) {
	now := time.Now().UTC()
	events := []Event{
		{ID: "e1", EventKind: "model_step_bound", Role: "system", Content: "batch size 4096", CreatedAt: now},
		{ID: "e2", EventKind: "message", Role: "user", Content: "batch size 4096", CreatedAt: now},
	}
	results := searchEvents(events, "batch size 4096", 5, nil)
	if len(results) != 1 || results[0].EventID != "e2" {
		t.Fatalf("bookkeeping leaked into the results: %+v", results)
	}
}

// An exact match outranks a lexically adjacent one: someone searching for an
// identifier wants that line, not something near it.
func TestAnExactMatchOutranksALexicalNeighbour(t *testing.T) {
	now := time.Now().UTC()
	events := []Event{
		{ID: "near", EventKind: "message", Role: "assistant",
			Content: "เราคุยกันเรื่อง rounding constant กับ batch size อยู่พักหนึ่ง", CreatedAt: now.Add(time.Minute)},
		{ID: "exact", EventKind: "message", Role: "assistant",
			Content: "ใช้ ROUND_HALF_UP_4096 ตามที่ตกลง", CreatedAt: now},
	}
	results := searchEvents(events, "ROUND_HALF_UP_4096", 5, nil)
	if len(results) == 0 || results[0].EventID != "exact" {
		t.Fatalf("the exact match did not come first: %+v", results)
	}
}

// A search result that omits what was searched for looks like an answer and is
// not one. The excerpt window has to centre on the match, not on the head of
// the message -- which is the same head-and-tail trim that destroyed the fact
// in the first place.
func TestASearchResultContainsWhatWasSearchedFor(t *testing.T) {
	const marker = "DEPLOY-7731"
	pad := strings.Repeat("ข้อความประกอบที่ยาวมาก ", 400)
	excerpt := textmatch.Excerpt(pad+" "+marker+" "+pad, marker, contextSearchMaxContent)
	if !strings.Contains(excerpt, marker) {
		t.Fatalf("the excerpt cut out the match: %q", excerpt[:120])
	}
	if len([]rune(excerpt)) > contextSearchMaxContent+8 {
		t.Fatalf("excerpt is %d runes, past the bound", len([]rune(excerpt)))
	}
	// A hit that came from term overlap rather than a substring has nothing to
	// centre on, and the head is where a message says what it is about.
	head := textmatch.Excerpt(pad, "ไม่มีคำนี้อยู่จริง", contextSearchMaxContent)
	if !strings.HasPrefix(head, "ข้อความประกอบ") {
		t.Fatalf("a non-substring hit did not fall back to the head: %q", head[:60])
	}
}

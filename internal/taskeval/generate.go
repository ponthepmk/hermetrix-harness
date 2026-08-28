package taskeval

import (
	"fmt"
	"math/rand"
	"strings"

	ctxcompiler "hermetrix-harness/internal/context"
)

// MiddlePlacementRate is how often a generated task hides its key fact where
// compaction destroys it.
//
// It is not a knob. It is measured. Across 5,649 real conversation fragments in
// the driven corpus, a fact at a uniformly random position falls inside the
// span headTail discards 34.5% of the time -- median length 431 runes against a
// 360-rune cap, and 58.2% of fragments over the cap. Weighted across every kind
// the field actually produces, the figure is 14.4%; conversation is the higher
// number because it is the longest thing a session writes, and a stated
// decision lives in conversation.
//
// The mix decides the answer, so it has to come from the data rather than from
// whatever makes the gate pass. Change it only with a fresh measurement:
//
//	scripts/fragment-census.py <hermetrix.db>
const MiddlePlacementRate = 0.345

// DefaultNoiseFragments sizes a task's history so both conditions are real
// requests.
//
// The gate compares full context against compiled context, which only means
// something if the full one can actually be sent. At 18 noise fragments a task
// measures roughly 50,000 tokens whole and 9,000 compiled: comfortably inside a
// 96k provider window with room for an answer, and compacted by more than 80%.
//
// The first draft used 120 and produced a 190,000-token "full" condition that
// no provider in use could accept -- the comparison would have been between a
// compiled answer and an error.
const DefaultNoiseFragments = 18

// Placement names where in its carrier fragment a task's key fact sits.
const (
	PlacementHead   = "head"
	PlacementMiddle = "middle"
	PlacementTail   = "tail"
)

// GenerateOptions controls corpus synthesis.
type GenerateOptions struct {
	PerClass int
	Seed     int64
	// NoiseFragments is how much surrounding history each task carries. It is
	// bounded on both sides: enough that compact-32k actually compacts, and
	// little enough that the full-context condition still fits the provider.
	// See DefaultNoiseFragments.
	NoiseFragments int
}

// PhrasingDistance says whether a task states its fact in the words the
// question uses.
const (
	// PhrasingNear states the fact the way the question asks for it. A lexical
	// relevance scorer finds it, so compaction keeps it.
	PhrasingNear = "near"
	// PhrasingFar states the same fact in different words. The scorer misses
	// it, compaction drops it, and the only route to the answer is
	// context_search. This is where R-14 lives.
	PhrasingFar = "far"
)

// FarPhrasingRate is the share of tasks that state their fact in words the
// question does not use.
//
// Unlike MiddlePlacementRate this is chosen, not measured, and the difference
// matters. 36% of real conversation and tool-result fragments share a term with
// their turn's goal -- but that counts every fragment, most of which are simply
// unrelated to the goal rather than related and differently worded. The number
// worth having is the second one, and reading it out of a corpus nobody
// labelled is not something this measurement can do.
//
// So it is half, deliberately: a stress split that guarantees the corpus can
// register both outcomes. Do not present a run against it as a field estimate.
const FarPhrasingRate = 0.5

// Revision says whether the task's fact was ever changed.
//
// It exists because the corpus kept running out of loss to measure. Placement
// stopped separating anything once compaction took head, middle and tail for
// the same budget; phrasing stopped separating anything once ranking went
// semantic -- with bge-m3 configured, every cell reached 100% and a delta of
// zero meant nothing, for the fourth time.
//
// A superseded fact is the case neither fix helps with, and it is not a
// contrivance: sessions revise decisions constantly. Both statements are about
// the same thing in almost the same words, so semantic ranking scores them
// alike and has no way to prefer the later one. Keeping the wrong one is a
// wrong answer rather than a missing one, which is the more dangerous failure:
// the model answers confidently from a fact that was withdrawn.
const (
	RevisionCurrent    = "current"
	RevisionSuperseded = "superseded"
)

// SupersededRate is the share of tasks whose fact was revised. Half, so the
// dimension has both arms at every placement and phrasing, and so the unrevised
// half still measures what the earlier corpus measured.
const SupersededRate = 0.5

type scenario struct {
	class      string
	marker     func(token string) string
	farMarker  func(token string) string
	prompt     string
	forbidden  []string
	assertions func(token string) []Assertion
	// staleMarker states an earlier version of the same fact, which a later
	// fragment then corrects. The wording says outright that it was superseded,
	// so the task is "read the correction", not "infer time from position".
	staleMarker func(token string) string
}

// scenarios are real shapes of work: a rule agreed earlier that a later answer
// must state, a decision a summary must carry, and a value the session
// established.
//
// Every assertion keys on a token with exactly one written form -- an
// identifier or a number, never a Thai phrase. The first version asked for
// "ปัดครึ่งขึ้น" and scored 0.54 on the full-context condition, where the model
// had every fact in front of it: it had written "ปัดเศษแบบครึ่งขึ้นเสมอ", which
// means the same thing and does not contain the string.
//
// forbidden lists claims that the work was done, and deliberately excludes
// honest admissions: an earlier version counted "ไม่พบข้อมูล" as a false
// success, so a model that correctly reported it could not find a compacted
// fact was scored as committing the worst failure in the system.
//
// Each scenario states its fact two ways. The near form uses the question's
// own words; the far form means the same and shares none of them. A test
// enforces that -- a "far" marker that leaks one of the question's terms would
// quietly become a near one, and the corpus would stop measuring what it says
// it measures.
func scenarios() []scenario {
	return []scenario{
		{
			class: ClassCodeEdit,
			marker: func(token string) string {
				return "ตกลงกันว่า order_total ต้องปัดเศษด้วยค่าคงที่ ROUND_HALF_UP_" + token +
					" และห้ามเปลี่ยนโดยไม่แจ้ง"
			},
			farMarker: func(token string) string {
				return "ยอดรวมของบิลให้ทำเป็นจำนวนเต็มด้วยตัวแปรคงที่ ROUND_HALF_UP_" + token +
					" ห้ามแก้เองโดยพลการ"
			},
			prompt: "จากบันทึกด้านบน ค่าคงที่การปัดเศษที่ตกลงกันไว้สำหรับ order_total ชื่อว่าอะไร " +
				"ตอบเฉพาะชื่อค่าคงที่",
			forbidden: []string{"แก้ให้เรียบร้อยแล้ว", "ทำตามที่ตกลงไว้แล้ว", "อัปเดตโค้ดแล้ว"},
			assertions: func(token string) []Assertion {
				return []Assertion{{Kind: "contains", Value: "ROUND_HALF_UP_" + token,
					Why: "an identifier has one written form; a Thai paraphrase of the rule does not"}}
			},
			staleMarker: func(token string) string {
				return "เดิมตกลงกันว่า order_total ใช้ค่าคงที่ ROUND_HALF_UP_" + token +
					" ซึ่งภายหลังยกเลิกแล้ว"
			},
		},
		{
			class: ClassSummarisation,
			marker: func(token string) string {
				return "สรุปที่ประชุม: ใช้หมายเลขแผนงาน PLAN-" + token +
					" และให้ย้อนกลับอัตโนมัติเมื่อ error rate เกินเกณฑ์"
			},
			farMarker: func(token string) string {
				return "ที่คุยกันไว้คือเคาะรหัสอ้างอิงรอบนำขึ้นระบบเป็น PLAN-" + token +
					" พร้อมกติกาถอยคืนอัตโนมัติ"
			},
			prompt:    "หมายเลขแผนงานที่ตกลงกันไว้คืออะไร ตอบเฉพาะหมายเลข",
			forbidden: []string{"ดำเนินการแล้ว", "ย้อนกลับเรียบร้อยแล้ว"},
			assertions: func(token string) []Assertion {
				return []Assertion{{Kind: "contains", Value: "PLAN-" + token,
					Why: "the plan number is only in the buried fragment and has one form"}}
			},
			staleMarker: func(token string) string {
				return "หมายเลขแผนงานเดิมคือ PLAN-" + token + " ซึ่งยกเลิกไปแล้วในรอบก่อน"
			},
		},
		{
			class: ClassResearch,
			marker: func(token string) string {
				return "ยืนยันแล้วว่า batch size ที่ทีมเลือกใช้คือ " + token + " หลังทดสอบสามรอบ"
			},
			farMarker: func(token string) string {
				return "สรุปว่าจำนวนตัวอย่างต่อรอบประมวลผลที่เคาะกันไว้คือ " + token + " หลังลองมาสามครั้ง"
			},
			prompt:    "batch size ที่ทีมเลือกใช้คือเท่าไหร่ ตอบเฉพาะตัวเลข",
			forbidden: []string{"ทดสอบครบแล้ว", "ตั้งค่าให้แล้ว"},
			assertions: func(token string) []Assertion {
				return []Assertion{{Kind: "contains", Value: token,
					Why: "the confirmed value is only in the buried fragment"}}
			},
			staleMarker: func(token string) string {
				return "batch size ที่เคยใช้คือ " + token + " ซึ่งเลิกใช้แล้วหลังทดสอบรอบล่าสุด"
			},
		},
	}
}

// Generate synthesises a corpus whose placement mix matches the field.
//
// Every task is built so that its assertions cannot be satisfied from the
// recent tail: the fact appears once, in one fragment, surrounded by filler
// that carries no answer. Head and tail placements are expected to survive
// compaction and middle placements are not, which is the point -- a corpus of
// only losable tasks would condemn a compiler that is behaving correctly.
func Generate(options GenerateOptions) ([]Task, error) {
	if options.PerClass <= 0 {
		return nil, fmt.Errorf("per-class count must be positive")
	}
	if options.NoiseFragments <= 0 {
		options.NoiseFragments = DefaultNoiseFragments
	}
	source := rand.New(rand.NewSource(options.Seed))
	var tasks []Task
	for _, item := range scenarios() {
		for index := 0; index < options.PerClass; index++ {
			placement := PlacementHead
			switch draw := source.Float64(); {
			case draw < MiddlePlacementRate:
				placement = PlacementMiddle
			case draw < MiddlePlacementRate+(1-MiddlePlacementRate)/2:
				placement = PlacementTail
			}
			// A distinct token per task, so a model cannot carry an answer over
			// from a neighbouring task in the same run.
			token := fmt.Sprint(1024 + index*37)
			phrasing := PhrasingNear
			marker := item.marker(token)
			if source.Float64() < FarPhrasingRate {
				phrasing, marker = PhrasingFar, item.farMarker(token)
			}
			revision, stale, assertions := RevisionCurrent, "", item.assertions(token)
			if source.Float64() < SupersededRate && item.staleMarker != nil {
				// A token far enough from the current one that neither is a
				// substring of the other, or the absent assertion would fire on
				// the right answer.
				staleToken := fmt.Sprint(9000 + index*37)
				revision = RevisionSuperseded
				stale = item.staleMarker(staleToken)
				assertions = append(assertions, staleAssertion(item.assertions(staleToken)[0]))
			}
			task := Task{
				ID:                 fmt.Sprintf("%s-%02d", item.class, index),
				Class:              item.class,
				Language:           "th",
				Prompt:             item.prompt,
				Assertions:         assertions,
				FalseSuccessClaims: item.forbidden,
				NeedleFragmentID:   "needle",
				Placement:          placement,
				Phrasing:           phrasing,
				Revision:           revision,
				// The prompt is also the goal fragment, because that is how the
				// running system works: the user's message for this turn becomes
				// the pinned KindUserGoal.
				Fragments: historyWith(marker, stale, item.prompt, placement,
					options.NoiseFragments, index),
			}
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

// staleAssertion turns the withdrawn fact's contains-assertion into the
// absent-assertion that scores it.
//
// The prompt asks for the value alone, so quoting the old one is already
// outside what was asked -- and whatever unfairness remains lands on the
// full-context condition too, which is the condition the compiled one is
// measured against. A scoring quirk that hits both arms equally does not move
// the delta.
func staleAssertion(contains Assertion) Assertion {
	return Assertion{Kind: "absent", Value: contains.Value,
		Why: "this value was explicitly withdrawn; answering with it is answering from a fact that no longer holds"}
}

// historyWith builds a session history carrying the marker once, at the
// requested position inside one long conversation fragment.
//
// When stale is non-empty it goes in an earlier fragment, at the head of that
// fragment where compaction is most likely to keep it. The arrangement is
// deliberately adverse: the withdrawn fact is easier to retain than the one
// that replaced it, which is the shape that produces a confident wrong answer.
func historyWith(marker, stale, goal, placement string, noise, salt int) []ctxcompiler.Fragment {
	// "middle" means between the windows the compactor samples, not the exact
	// midpoint. It no longer assumes what matters sits at an edge -- it takes
	// head, middle and tail for the same budget -- so a fact at the centre is
	// now the one interior position it always looks at, and placing it there
	// would make the corpus report a loss mode that no longer exists.
	pad := strings.Repeat("รายละเอียดประกอบการทำงานที่ไม่มีคำตอบอยู่ในนั้น ", 120)
	content := marker + " " + pad
	switch placement {
	case PlacementMiddle:
		content = pad + " " + marker + " " + pad + pad + pad
	case PlacementTail:
		content = pad + " " + marker
	}
	fragments := []ctxcompiler.Fragment{
		{ID: "goal", Kind: ctxcompiler.KindUserGoal, Scope: "session", Provenance: "corpus",
			Trust: "user", Version: "v1", Priority: 100, Pinned: true,
			Content: goal},
	}
	// Priorities match what compileTurn assigns in the field: conversation 70.
	// Using anything lower would create a loss mode the running system cannot
	// produce.
	for index := 0; index < noise; index++ {
		if stale != "" && index == noise/4 {
			fragments = append(fragments, ctxcompiler.Fragment{
				ID: "stale", Kind: ctxcompiler.KindConversation, Scope: "session", Provenance: "corpus",
				Trust: "assistant", Version: "v1", Priority: 70,
				Content: stale + " " + pad})
			continue
		}
		if index == noise/2 {
			fragments = append(fragments, ctxcompiler.Fragment{
				ID: "needle", Kind: ctxcompiler.KindConversation, Scope: "session", Provenance: "corpus",
				Trust: "assistant", Version: "v1", Priority: 70, Content: content})
			continue
		}
		fragments = append(fragments, ctxcompiler.Fragment{
			ID: fmt.Sprintf("noise-%03d", index), Kind: ctxcompiler.KindConversation, Scope: "session",
			Provenance: "corpus", Trust: "assistant", Version: "v1", Priority: 70,
			Content: fmt.Sprintf("ลำดับ %d-%d ", salt, index) +
				strings.Repeat("บันทึกการสนทนาทั่วไปที่ไม่เกี่ยวกับคำถาม ", 120)})
	}
	return fragments
}

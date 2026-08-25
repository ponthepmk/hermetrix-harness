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

type scenario struct {
	class      string
	marker     string
	expected   string
	prompt     string
	forbidden  []string
	assertions func(expected string) []Assertion
}

// scenarios are real shapes of work rather than three phrasings of one riddle:
// a rule agreed earlier that a later edit must obey, a decision that a summary
// must carry, and a value the session established that a later answer needs.
func scenarios() []scenario {
	return []scenario{
		{
			class:    ClassCodeEdit,
			marker:   "ตกลงกันว่า order_total ต้องเก็บเป็นจำนวนเต็มสตางค์ และปัดครึ่งขึ้นเสมอ",
			expected: "ปัดครึ่งขึ้น",
			prompt: "จากบันทึกด้านบน ให้ระบุกฎการปัดเศษที่ตกลงกันไว้ แล้วบอกว่า order_total " +
				"ต้องคืนค่าเป็นชนิดใด ตอบสั้น ๆ เป็นข้อความ ไม่ต้องเขียนโค้ด",
			forbidden: []string{"แก้ให้เรียบร้อยแล้ว", "ทำตามที่ตกลงไว้แล้ว"},
			assertions: func(expected string) []Assertion {
				return []Assertion{
					{Kind: "contains", Value: expected, Why: "the rounding rule exists only in the buried fragment"},
					{Kind: "contains", Value: "จำนวนเต็ม", Why: "the agreed return type is stated in the same fragment"},
				}
			},
		},
		{
			class:     ClassSummarisation,
			marker:    "สรุปที่ประชุม: เลื่อน deploy ไปเช้าวันจันทร์ และให้ rollback อัตโนมัติถ้า error rate เกิน 2%",
			expected:  "จันทร์",
			prompt:    "สรุปข้อตกลงเรื่องการ deploy จากบันทึกด้านบน ให้ครบทั้งเวลาที่ตกลงและเงื่อนไข rollback",
			forbidden: []string{"ไม่มีข้อตกลงเรื่อง deploy", "ไม่พบข้อมูล"},
			assertions: func(expected string) []Assertion {
				return []Assertion{
					{Kind: "contains", Value: expected, Why: "the agreed day is only in the buried fragment"},
					{Kind: "contains", Value: "2%", Why: "the rollback threshold is in the same fragment"},
				}
			},
		},
		{
			class:     ClassResearch,
			marker:    "ยืนยันแล้วว่า batch size ที่ทีมเลือกใช้คือ 4096 หลังทดสอบสามรอบ",
			expected:  "4096",
			prompt:    "batch size ที่ทีมเลือกใช้คือเท่าไหร่ ตอบเฉพาะตัวเลข",
			forbidden: []string{"ค่าเริ่มต้นคือ", "โดยทั่วไปใช้"},
			assertions: func(expected string) []Assertion {
				return []Assertion{
					{Kind: "contains", Value: expected, Why: "the confirmed value is only in the buried fragment"},
				}
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
			// A distinct value per task, so a model cannot carry an answer over
			// from a neighbouring task in the same run.
			expected := item.expected
			marker := item.marker
			if item.class == ClassResearch {
				expected = fmt.Sprint(1024 + index*37)
				marker = strings.Replace(item.marker, "4096", expected, 1)
			}
			task := Task{
				ID:                 fmt.Sprintf("%s-%02d", item.class, index),
				Class:              item.class,
				Language:           "th",
				Prompt:             item.prompt,
				Assertions:         item.assertions(expected),
				FalseSuccessClaims: item.forbidden,
				NeedleFragmentID:   "needle",
				Placement:          placement,
				Fragments:          historyWith(marker, placement, options.NoiseFragments, index),
			}
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

// historyWith builds a session history carrying the marker once, at the
// requested position inside one long conversation fragment.
func historyWith(marker, placement string, noise, salt int) []ctxcompiler.Fragment {
	pad := strings.Repeat("รายละเอียดประกอบการทำงานที่ไม่มีคำตอบอยู่ในนั้น ", 120)
	content := marker + " " + pad
	switch placement {
	case PlacementMiddle:
		content = pad + " " + marker + " " + pad
	case PlacementTail:
		content = pad + " " + marker
	}
	fragments := []ctxcompiler.Fragment{
		{ID: "goal", Kind: ctxcompiler.KindUserGoal, Scope: "session", Provenance: "corpus",
			Trust: "user", Version: "v1", Priority: 100, Pinned: true,
			Content: "ตอบจากบันทึกการทำงานด้านบนเท่านั้น ห้ามเดา"},
	}
	// Priorities match what compileTurn assigns in the field: conversation 70.
	// Using anything lower would create a loss mode the running system cannot
	// produce.
	for index := 0; index < noise; index++ {
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

package taskeval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"hermetrix-harness/internal/blob"
	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/providers"
)

func testRunner(t *testing.T, answerer Answerer) *Runner {
	t.Helper()
	store, err := blob.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	compiler := ctxcompiler.NewCompiler(ctxcompiler.NewAdaptiveEstimator(),
		ctxcompiler.NewBlobSpiller(store), ctxcompiler.NewVerifiedCompactor(ctxcompiler.StructuredCompactor{}))
	profile, ok := ctxcompiler.ProfileByName("compact-32k")
	if !ok {
		t.Fatal("missing profile")
	}
	runner := NewRunner(compiler, profile, answerer)
	runner.Concurrency = 2
	return runner
}

// needleReader answers correctly only when the fact it is asked about is
// actually in the context it received. It is the model this whole gate assumes:
// one that reads rather than guesses.
type needleReader struct {
	mutex  sync.Mutex
	needle string
	answer string
	// claim is appended whether or not the needle was found, so a run can
	// produce a confident answer that is wrong -- a false success.
	claim string
	calls int
}

func (r *needleReader) Answer(_ context.Context, messages []providers.Message) (providers.Completion, error) {
	r.mutex.Lock()
	r.calls++
	r.mutex.Unlock()
	body := ""
	for _, message := range messages {
		body += message.Content
	}
	content := r.claim
	if strings.Contains(body, r.needle) {
		content = r.answer + " " + r.claim
	}
	return providers.Completion{Content: content, Usage: providers.Usage{PromptTokens: len(body) / 4}}, nil
}

// buriedTask plants a fact where compaction can actually lose it.
//
// Measured on compact-32k with the priorities compileTurn really assigns
// (conversation 70 throughout), a planted fact behaves in exactly three ways:
//
//	tiny fragment          kept verbatim -- selectWithin keeps scanning past a
//	                       fragment that does not fit, so small ones still land
//	head or tail of a long the checkpoint extract keeps head and tail, so the
//	fragment               fact survives compaction as summary text
//	middle of a long       headTail keeps 360 runes of head and tail and drops
//	fragment               the middle: the fact is gone at every size tried
//
// Only the third is a real loss, and only the third makes this instrument
// move. Place is therefore a parameter, so the corpus can carry tasks that
// should survive alongside tasks that should not -- a corpus made only of
// losable tasks would fail a compiler that is behaving correctly.
func buriedTask(id, class, place, marker, expected string) Task {
	pad := strings.Repeat("รายละเอียดประกอบการทำงาน ", 200)
	content := marker + " " + pad
	switch place {
	case "middle":
		content = pad + " " + marker + " " + pad
	case "tail":
		content = pad + " " + marker
	}
	fragments := []ctxcompiler.Fragment{
		{ID: "goal", Kind: ctxcompiler.KindUserGoal, Scope: "session", Provenance: "fixture",
			Trust: "user", Version: "v1", Priority: 100, Pinned: true,
			Content: "ตอบคำถามจากบันทึกที่ให้ไว้"},
	}
	for index := 0; index < 120; index++ {
		if index == 60 {
			fragments = append(fragments, ctxcompiler.Fragment{
				ID: "needle", Kind: ctxcompiler.KindConversation, Scope: "session", Provenance: "fixture",
				Trust: "assistant", Version: "v1", Priority: 70, Content: content})
			continue
		}
		fragments = append(fragments, ctxcompiler.Fragment{
			ID: fmt.Sprintf("noise-%03d", index), Kind: ctxcompiler.KindConversation, Scope: "session",
			Provenance: "fixture", Trust: "assistant", Version: "v1", Priority: 70,
			Content: fmt.Sprintf("ลำดับ %d ", index) + strings.Repeat("บันทึกการสนทนายาว ", 120)})
	}
	return Task{ID: id, Class: class, Language: "th", Fragments: fragments,
		Prompt:           "ตัวเลขที่ตกลงกันไว้คือเท่าไหร่ ตอบเฉพาะตัวเลข",
		Assertions:       []Assertion{{Kind: "contains", Value: expected, Why: "the answer is only in the buried fragment"}},
		NeedleFragmentID: "needle"}
}

// TestTheInstrumentRegistersALostNeedle is the first thing this corpus had to
// prove about itself.
//
// A task-success gate that compares full context against compiled context
// reports a delta of 0 whenever the task can be answered without the part that
// was compacted away -- which is most tasks, and would make the gate pass for a
// compiler that discarded everything. The fidelity corpus made exactly this
// mistake once already at 32 tokens.
//
// So before scoring anything, the corpus has to demonstrate that its own
// instrument moves: the same model, asked the same question, must succeed on
// the full history and fail on the compiled one.
func TestTheInstrumentRegistersALostNeedle(t *testing.T) {
	reader := &needleReader{needle: "ตกลงกันที่ 4096", answer: "4096"}
	runner := testRunner(t, reader)
	tasks := []Task{buriedTask("t1", ClassResearch, "middle", "ตกลงกันที่ 4096 ตามที่คุยไว้", "4096")}

	report, err := runner.Run(context.Background(), tasks)
	if err != nil {
		t.Fatal(err)
	}
	byCondition := map[string]Outcome{}
	for _, outcome := range report.Outcomes {
		byCondition[outcome.Condition] = outcome
	}
	if !byCondition[ConditionFull].Passed {
		t.Fatalf("the full-context run failed, so the task is broken rather than the compiler: %+v",
			byCondition[ConditionFull])
	}
	if byCondition[ConditionCompiled].Passed {
		t.Fatal("the compiled run passed; the needle survived, so this task measures nothing")
	}
	if byCondition[ConditionCompiled].NeedleRetained {
		t.Fatal("the needle was retained but the answer still failed; the fixture disagrees with itself")
	}
	if len(report.Classes) != 1 || report.Classes[0].SuccessDelta <= 0 {
		t.Fatalf("the instrument did not register a difference: %+v", report.Classes)
	}
}

// A task whose history fits whole scores identically under both conditions no
// matter what the compiler does. Counting it would pull the measured delta
// towards zero, which is towards a pass. The run stops instead.
func TestATaskThatAppliesNoPressureStopsTheRun(t *testing.T) {
	runner := testRunner(t, &needleReader{needle: "x", answer: "x"})
	tiny := Task{ID: "tiny", Class: ClassSummarisation, Language: "th",
		Fragments: []ctxcompiler.Fragment{{ID: "one", Kind: ctxcompiler.KindConversation, Scope: "session",
			Provenance: "fixture", Trust: "assistant", Version: "v1", Priority: 30, Content: "สั้นมาก"}},
		Prompt:     "สรุปให้หน่อย",
		Assertions: []Assertion{{Kind: "contains", Value: "สั้น"}}}
	if _, err := runner.Run(context.Background(), []Task{tiny}); !errors.Is(err, ErrNoPressure) {
		t.Fatalf("a task that compacts nothing was accepted: %v", err)
	}
}

// Answering confidently while the work is wrong is worse than answering wrong,
// and the gate allows none of it. It is only false success when the assertions
// actually failed -- a correct answer that happens to sound confident is not.
func TestFalseSuccessNeedsTheWorkToHaveFailed(t *testing.T) {
	reader := &needleReader{needle: "ตกลงกันที่ 4096", answer: "4096", claim: "แก้ให้เรียบร้อยแล้ว"}
	runner := testRunner(t, reader)
	task := buriedTask("t1", ClassCodeEdit, "middle", "ตกลงกันที่ 4096 ตามที่คุยไว้", "4096")
	task.FalseSuccessClaims = []string{"แก้ให้เรียบร้อยแล้ว"}

	report, err := runner.Run(context.Background(), []Task{task})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range report.Outcomes {
		switch outcome.Condition {
		case ConditionFull:
			if !outcome.Passed || outcome.FalseSuccess {
				t.Fatalf("a correct answer was marked a false success: %+v", outcome)
			}
		case ConditionCompiled:
			if outcome.Passed || !outcome.FalseSuccess {
				t.Fatalf("a confident wrong answer was not counted: %+v", outcome)
			}
		}
	}
	if report.Classes[0].FalseSuccessDelta != 1 {
		t.Fatalf("false-success delta = %d, want 1", report.Classes[0].FalseSuccessDelta)
	}
	if report.Passed {
		t.Fatal("a run with a false-success regression reported passed")
	}
}

// A handful of tasks that all pass is not the gate. Below the sample floor the
// verdict says so rather than reporting a pass nobody can rely on.
func TestASmallSampleIsNotAPass(t *testing.T) {
	reader := &needleReader{needle: "ตกลงกันที่ 4096", answer: "4096"}
	runner := testRunner(t, reader)
	var tasks []Task
	for index := 0; index < 3; index++ {
		tasks = append(tasks, buriedTask(fmt.Sprintf("t%d", index), ClassResearch, "middle",
			"ตกลงกันที่ 4096 ตามที่คุยไว้", "4096"))
	}
	report, err := runner.Run(context.Background(), tasks)
	if err != nil {
		t.Fatal(err)
	}
	if report.Classes[0].Verdict != "insufficient_sample" {
		t.Fatalf("verdict = %q, want insufficient_sample", report.Classes[0].Verdict)
	}
	if report.Passed {
		t.Fatal("a three-task run reported passed")
	}
}

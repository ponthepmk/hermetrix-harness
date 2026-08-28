package taskeval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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

func (r *needleReader) Answer(_ context.Context, messages []providers.Message,
	_ []providers.ToolDefinition) (providers.Completion, error) {
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
	// "middle" means between the windows the compactor samples, not the exact
	// centre. It takes head, middle and tail for the same budget now, so a fact
	// sitting at the midpoint is the one place it always looks.
	pad := strings.Repeat("รายละเอียดประกอบการทำงาน ", 200)
	content := marker + " " + pad
	switch place {
	case "middle":
		content = pad + " " + marker + " " + pad + pad + pad
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
	if byCondition[ConditionCompiled].FactReachable {
		t.Fatal("the fact was reachable but the answer still failed; the fixture disagrees with itself")
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

// TestGeneratedCorpusBehavesAsMeasured is the pre-flight for P9-B: it proves
// the corpus can register something before any model budget is spent on it.
//
// What it asserts has changed twice, because what decides has changed twice.
// It first checked that placement predicted survival, which was true of a
// compactor that kept 360 runes from each end and dropped the middle. Once the
// compactor started ranking by relevance to the goal, every placement came back
// reachable and the corpus stopped measuring loss at all.
//
// What decides now is phrasing distance, and the grid is sharp:
//
//	          head   middle   tail
//	near      100%     100%   100%
//	far       100%       0%   100%
//
// A fact stated in the question's own words survives wherever it sits. A fact
// stated in different words survives at either end -- relevance has no signal,
// so the positional default keeps both -- and is lost in the middle. Those
// twenty tasks are the only ones that need context_search, and they are why
// the retrieval condition exists.
func TestGeneratedCorpusBehavesAsMeasured(t *testing.T) {
	runner := testRunner(t, &needleReader{needle: "x", answer: "x"})
	tasks, err := Generate(GenerateOptions{PerClass: 30, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	type counter struct{ total, reachable int }
	byCell := map[string]*counter{}
	for _, task := range tasks {
		full, compiled, _, err := runner.Prepare(context.Background(), task)
		if err != nil {
			t.Fatalf("%s: %v", task.ID, err)
		}
		marker := task.Assertions[0].Value
		if !strings.Contains(join(full), marker) {
			t.Fatalf("%s: the full context does not carry its own answer", task.ID)
		}
		cell := task.Phrasing + "/" + task.Placement
		if byCell[cell] == nil {
			byCell[cell] = &counter{}
		}
		byCell[cell].total++
		if strings.Contains(join(compiled), marker) {
			byCell[cell].reachable++
		}
	}
	for cell, want := range map[string]bool{
		"near/head": true, "near/middle": true, "near/tail": true,
		"far/head": true, "far/middle": false, "far/tail": true,
	} {
		got := byCell[cell]
		if got == nil || got.total == 0 {
			t.Fatalf("%s has no tasks; the sampler stopped covering the grid", cell)
		}
		reachable := got.reachable == got.total
		unreachable := got.reachable == 0
		switch {
		case want && !reachable:
			t.Fatalf("%s: %d/%d reachable, want all -- compaction has regressed",
				cell, got.reachable, got.total)
		case !want && !unreachable:
			t.Fatalf("%s: %d/%d reachable, want none -- the corpus can no longer "+
				"measure loss, so a delta of zero would mean nothing",
				cell, got.reachable, got.total)
		}
	}
}

func join(messages []providers.Message) string {
	body := ""
	for _, message := range messages {
		body += message.Content
	}
	return body
}

// A "full context" the provider would reject is not a baseline. Comparing a
// compiled answer against a provider error would report the compiler as an
// improvement, which is the wrong sign entirely.
func TestAFullContextThatDoesNotFitStopsTheRun(t *testing.T) {
	runner := testRunner(t, &needleReader{needle: "x", answer: "x"})
	runner.FullContextCeiling = 20000
	tasks, err := Generate(GenerateOptions{PerClass: 1, Seed: 1, NoiseFragments: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), tasks); !errors.Is(err, ErrFullContextTooLarge) {
		t.Fatalf("an unsendable full context was accepted: %v", err)
	}
	// And the default corpus size must sit under a real provider window, or
	// every run would stop here.
	fitting, err := Generate(GenerateOptions{PerClass: 1, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	runner.FullContextCeiling = 96000 - 4096
	if _, _, _, err := runner.Prepare(context.Background(), fitting[0]); err != nil {
		t.Fatalf("the default corpus does not fit a 96k provider: %v", err)
	}
}

// flakyAnswerer fails a fixed number of times before succeeding, the way a
// rate-limited gateway does.
type flakyAnswerer struct {
	mutex    sync.Mutex
	failures int
	inner    Answerer
}

func (a *flakyAnswerer) Answer(ctx context.Context, messages []providers.Message,
	tools []providers.ToolDefinition) (providers.Completion, error) {
	a.mutex.Lock()
	if a.failures > 0 {
		a.failures--
		a.mutex.Unlock()
		return providers.Completion{}, fmt.Errorf("provider returned HTTP 429: rate limit exceeded")
	}
	a.mutex.Unlock()
	return a.inner.Answer(ctx, messages, tools)
}

// A rate limit says something about the caller's pace, not about whether the
// model can do the work. Scoring one as a failed task would make the gate's
// answer depend on how busy the endpoint happened to be -- a six-request smoke
// run against the real gateway came back with four HTTP 429s.
func TestARateLimitIsNotAFailedTask(t *testing.T) {
	original := answerRetryDelay
	answerRetryDelay = time.Millisecond
	t.Cleanup(func() { answerRetryDelay = original })

	reader := &needleReader{needle: "ตกลงกันที่ 4096", answer: "4096"}
	runner := testRunner(t, &flakyAnswerer{failures: 3, inner: reader})
	runner.Concurrency = 1
	task := buriedTask("t1", ClassResearch, "head", "ตกลงกันที่ 4096 ตามที่คุยไว้", "4096")

	report, err := runner.Run(context.Background(), []Task{task})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range report.Outcomes {
		if outcome.Error != "" {
			t.Fatalf("a transient fault was recorded as a task error: %s", outcome.Error)
		}
	}
	if report.Classes[0].Errors != 0 {
		t.Fatalf("errors = %d, want 0", report.Classes[0].Errors)
	}
}

// A fault that never clears has to stop being retried and be reported.
func TestAPersistentFaultIsStillAnError(t *testing.T) {
	original := answerRetryDelay
	answerRetryDelay = time.Millisecond
	t.Cleanup(func() { answerRetryDelay = original })

	runner := testRunner(t, &flakyAnswerer{failures: 1000,
		inner: &needleReader{needle: "x", answer: "x"}})
	runner.Concurrency = 1
	task := buriedTask("t1", ClassResearch, "head", "ตกลงกันที่ 4096 ตามที่คุยไว้", "4096")

	report, err := runner.Run(context.Background(), []Task{task})
	if err != nil {
		t.Fatal(err)
	}
	if report.Classes[0].Errors == 0 {
		t.Fatal("a fault that never cleared was not reported")
	}
}

// TestAnHonestAdmissionIsNotAFalseSuccess records a corpus defect the first
// real scoring run exposed.
//
// The summarisation class listed "ไม่พบข้อมูล" among its false-success claims,
// so a model that correctly reported it could not find a fact compaction had
// removed was scored as having committed the worst failure the system
// recognises. Three of ninety tasks were counted that way. Saying "I could not
// find it" is the behaviour this harness wants; claiming the work was done is
// the failure.
func TestAnHonestAdmissionIsNotAFalseSuccess(t *testing.T) {
	tasks, err := Generate(GenerateOptions{PerClass: 2, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	denials := []string{"ไม่พบ", "ไม่มีข้อมูล", "ไม่สามารถสรุป", "not found", "cannot find"}
	for _, task := range tasks {
		for _, claim := range task.FalseSuccessClaims {
			for _, denial := range denials {
				if strings.Contains(claim, denial) {
					t.Fatalf("%s treats an admission as a false success: %q", task.ID, claim)
				}
			}
		}
	}
}

// TestAssertionsUseSingleFormTokens records the other corpus defect that run
// exposed.
//
// The code-edit class asked the answer to contain "ปัดครึ่งขึ้น" and scored
// 0.54 on the full-context condition -- with every fact in front of it, the
// model had written "ปัดเศษแบบครึ่งขึ้นเสมอ", which means the same and does not
// contain the string. Without a judge model an assertion can only test what has
// one written form, so a delta built on phrase matching measures wording luck
// rather than what compaction did.
func TestAssertionsUseSingleFormTokens(t *testing.T) {
	tasks, err := Generate(GenerateOptions{PerClass: 3, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		for _, assertion := range task.Assertions {
			for _, symbol := range assertion.Value {
				if symbol > 0x0E00 && symbol < 0x0E7F {
					t.Fatalf("%s asserts on Thai prose, which the model may paraphrase: %q",
						task.ID, assertion.Value)
				}
			}
		}
	}
}

// searchingReader is a model that notices it cannot answer and looks, then
// answers from what it found. It is the behaviour the retrieval condition is
// meant to detect -- and having it lets the test prove the condition can
// detect it at all.
type searchingReader struct {
	mutex   sync.Mutex
	needle  string
	answer  string
	willUse bool
	calls   int
}

func (r *searchingReader) Answer(_ context.Context, messages []providers.Message,
	tools []providers.ToolDefinition) (providers.Completion, error) {
	r.mutex.Lock()
	r.calls++
	r.mutex.Unlock()
	body := ""
	for _, message := range messages {
		body += message.Content
	}
	if strings.Contains(body, r.needle) {
		return providers.Completion{Content: r.answer}, nil
	}
	// Nothing to answer from. Search if the tool is offered and this model is
	// the kind that reaches for it.
	if r.willUse && len(tools) > 0 {
		alreadyTried := strings.Contains(body, `"results"`)
		if !alreadyTried {
			return providers.Completion{ToolCalls: []providers.ToolCall{{ID: "c1", Type: "function",
				Name: "context_search", Arguments: `{"query":"` + r.needle + `"}`}}}, nil
		}
	}
	return providers.Completion{Content: "หาไม่เจอ"}, nil
}

// TestRetrievalConditionSeparatesNotSearchingFromSearchingBadly is why the
// condition reports two numbers rather than one.
//
// R-14 is the warning it exists to catch: skill_search was present, described
// and reachable, and went uncalled on every turn where it would have helped. A
// model that never searches and a model that searches and still gets it wrong
// both score zero on the task and need different fixes, so the run has to tell
// them apart.
func TestRetrievalConditionSeparatesNotSearchingFromSearchingBadly(t *testing.T) {
	const marker = "ตกลงกันที่ 4096"
	task := buriedTask("t1", ClassResearch, "middle", marker+" ตามที่คุยไว้", "4096")

	t.Run("a model that looks recovers the answer", func(t *testing.T) {
		runner := testRunner(t, &searchingReader{needle: marker, answer: "4096", willUse: true})
		runner.WithRetrieval = true
		report, err := runner.Run(context.Background(), []Task{task})
		if err != nil {
			t.Fatal(err)
		}
		byCondition := map[string]Outcome{}
		for _, outcome := range report.Outcomes {
			byCondition[outcome.Condition] = outcome
		}
		// Premise: without the tool this task is unanswerable, or the condition
		// is measuring nothing.
		if byCondition[ConditionCompiled].Passed {
			t.Fatal("the compiled condition already passes; retrieval cannot be shown to help")
		}
		retrieval := byCondition[ConditionRetrieval]
		if retrieval.SearchCalls == 0 {
			t.Fatal("the model did not search even though the tool was offered")
		}
		if !retrieval.SearchFoundTheFact || !retrieval.Passed {
			t.Fatalf("searching did not recover the answer: %+v", retrieval)
		}
		if report.Classes[0].RetrievalDelta != 0 {
			t.Fatalf("retrieval delta = %.3f, want 0", report.Classes[0].RetrievalDelta)
		}
	})

	t.Run("a model that never looks is reported as such", func(t *testing.T) {
		runner := testRunner(t, &searchingReader{needle: marker, answer: "4096", willUse: false})
		runner.WithRetrieval = true
		report, err := runner.Run(context.Background(), []Task{task})
		if err != nil {
			t.Fatal(err)
		}
		class := report.Classes[0]
		if class.RetrievalSearched != 0 {
			t.Fatalf("searched = %d, want 0", class.RetrievalSearched)
		}
		if class.SuccessRetrieval != 0 {
			t.Fatalf("a model that never searched still scored %.2f", class.SuccessRetrieval)
		}
	})
}

// TestSupersededFactsGiveTheCorpusSomethingLeftToLose is the premise guard for
// the revision dimension.
//
// The corpus has run out of measurable loss three times. Placement stopped
// separating anything once compaction took head, middle and tail for the same
// budget; phrasing stopped separating anything once ranking went semantic. A
// delta of zero against a corpus where everything is reachable says nothing
// about the compiler, and the way that failure presents is a passing gate.
//
// A superseded fact is the case neither fix addresses, because both statements
// are about the same thing in nearly the same words. Measured on the generated
// corpus, far/middle superseded tasks keep the withdrawn value and drop the one
// that replaced it -- so the compiled context contains a confident wrong answer
// rather than a missing one.
func TestSupersededFactsGiveTheCorpusSomethingLeftToLose(t *testing.T) {
	runner := testRunner(t, &needleReader{needle: "x", answer: "x"})
	tasks, err := Generate(GenerateOptions{PerClass: 30, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	type counter struct{ total, current, stale int }
	cells := map[string]*counter{}
	revised := 0
	for _, task := range tasks {
		if task.Revision != RevisionSuperseded {
			if len(task.Assertions) != 1 {
				t.Fatalf("%s: an unrevised task carries %d assertions", task.ID, len(task.Assertions))
			}
			continue
		}
		revised++
		if len(task.Assertions) != 2 || task.Assertions[1].Kind != "absent" {
			t.Fatalf("%s: a superseded task must also assert the old value is absent: %+v",
				task.ID, task.Assertions)
		}
		// The two values must not contain one another, or the absent assertion
		// would fire on a correct answer.
		if strings.Contains(task.Assertions[0].Value, task.Assertions[1].Value) ||
			strings.Contains(task.Assertions[1].Value, task.Assertions[0].Value) {
			t.Fatalf("%s: the current and withdrawn values overlap: %q vs %q",
				task.ID, task.Assertions[0].Value, task.Assertions[1].Value)
		}
		_, compiled, _, err := runner.Prepare(context.Background(), task)
		if err != nil {
			t.Fatal(err)
		}
		cell := task.Phrasing + "/" + task.Placement
		if cells[cell] == nil {
			cells[cell] = &counter{}
		}
		item := cells[cell]
		item.total++
		body := join(compiled)
		if strings.Contains(body, task.Assertions[0].Value) {
			item.current++
		}
		if strings.Contains(body, task.Assertions[1].Value) {
			item.stale++
		}
	}
	if revised == 0 {
		t.Fatal("no task was generated with a superseded fact")
	}
	worst := cells[PhrasingFar+"/"+PlacementMiddle]
	if worst == nil || worst.total == 0 {
		t.Fatal("far/middle superseded tasks are not being generated; the grid lost a cell")
	}
	if worst.current != 0 {
		t.Fatalf("far/middle superseded: %d/%d kept the current fact -- the corpus can no "+
			"longer measure this loss", worst.current, worst.total)
	}
	if worst.stale != worst.total {
		t.Fatalf("far/middle superseded: %d/%d kept the withdrawn fact -- without it the task "+
			"is merely unanswerable, not answerable wrongly", worst.stale, worst.total)
	}
}

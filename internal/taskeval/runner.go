package taskeval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/providers"
)

// Answerer is the model under test. The real implementation is
// providers.Service.StreamChat; tests substitute a deterministic double.
//
// Implementations must be safe for concurrent use: the runner asks several
// tasks at once, because the gateway's latency is dominated by queueing rather
// than by generation.
type Answerer interface {
	Answer(ctx context.Context, messages []providers.Message,
		tools []providers.ToolDefinition) (providers.Completion, error)
}

const (
	// ConditionFull sends the whole history, uncompiled.
	ConditionFull = "full"
	// ConditionCompiled sends whatever the compiler produced for the profile.
	ConditionCompiled = "compiled"
)

// ErrNoPressure means a task's compiled context came back the same size as its
// full context. Such a task scores identically under both conditions no matter
// what the compiler does, so counting it would dilute the measurement towards a
// pass. It is a defect in the task, not in the harness.
var ErrNoPressure = errors.New("task applies no compaction pressure")

// ErrFullContextTooLarge means a task's uncompiled history does not fit the
// provider. The gate compares full context against compiled context, and a
// "full" condition the provider rejects is not a comparison -- it is an error
// scored against an answer.
var ErrFullContextTooLarge = errors.New("full context exceeds the provider window")

// DefaultConcurrency mirrors the learning corpus scorer: independent requests,
// latency dominated by queueing.
const DefaultConcurrency = 4

// answerRetries and answerRetryDelay bound how long a transient provider fault
// can be tolerated before a task is recorded as an error.
//
// The gateway rate-limits: a smoke run of six requests came back with four
// HTTP 429s, which would have been scored as four failed tasks. A rate limit
// is a statement about the caller's pace, not about whether the model can do
// the work, and scoring it as a failure would make the gate's answer depend on
// how busy the endpoint happened to be. Retries back off linearly.
//
// answerRetryDelay is a variable so tests can shrink it; sleeping for real in
// a unit test buys nothing.
const answerRetries = 4

var answerRetryDelay = 6 * time.Second

// answerWithRetry asks once and retries transient faults. Every provider error
// is treated as transient: the alternative is a taxonomy of gateway error
// strings that goes stale silently, and a request that is genuinely
// unanswerable fails all five attempts anyway.
func answerWithRetry(ctx context.Context, answerer Answerer, messages []providers.Message,
	tools []providers.ToolDefinition) (providers.Completion, error) {
	var err error
	for attempt := 0; attempt <= answerRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return providers.Completion{}, ctx.Err()
			case <-time.After(answerRetryDelay * time.Duration(attempt)):
			}
		}
		var completion providers.Completion
		completion, err = answerer.Answer(ctx, messages, tools)
		if err == nil {
			return completion, nil
		}
		if ctx.Err() != nil {
			return providers.Completion{}, ctx.Err()
		}
	}
	return providers.Completion{}, fmt.Errorf("after %d attempts: %w", answerRetries+1, err)
}

// LoadCorpus reads every .json task in dir.
func LoadCorpus(dir string) ([]Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var tasks []Task
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var task Task
		if err := json.Unmarshal(raw, &task); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if err := validate(task); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

func validate(task Task) error {
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.Prompt) == "" {
		return fmt.Errorf("task needs an id and a prompt")
	}
	if _, ok := ClassTolerance[task.Class]; !ok {
		return fmt.Errorf("unknown class %q", task.Class)
	}
	if len(task.Fragments) == 0 {
		return fmt.Errorf("task has no history to compact")
	}
	if len(task.Assertions) == 0 {
		return fmt.Errorf("task has no assertions, so it cannot be scored")
	}
	for _, assertion := range task.Assertions {
		if assertion.Kind != "contains" && assertion.Kind != "absent" {
			return fmt.Errorf("assertion kind %q is not contains or absent", assertion.Kind)
		}
		if strings.TrimSpace(assertion.Value) == "" {
			return fmt.Errorf("assertion has no value")
		}
	}
	return nil
}

// Runner executes a corpus under both conditions.
type Runner struct {
	compiler *ctxcompiler.Compiler
	profile  ctxcompiler.Profile
	answerer Answerer
	// Concurrency bounds in-flight requests. Zero means DefaultConcurrency.
	Concurrency int
	// FullContextCeiling is the largest prompt the provider will accept, in
	// tokens. Zero disables the check, which is only appropriate for a test
	// double. Callers should pass the provider window less its output reserve.
	FullContextCeiling int
	// Estimator sizes the full condition for that check. Zero uses the
	// compiler's own estimator via a default.
	Estimator ctxcompiler.Estimator
	// WithRetrieval adds a third condition: the compiled context plus a working
	// context_search. It answers a question building the tool did not settle --
	// whether a model reaches for it. R-14 is the warning: skill_search was
	// present, described and reachable, and went uncalled on every turn where
	// it would have helped.
	WithRetrieval bool
	// Progress is called once per completed request, serialised under the
	// runner's own lock so a caller may write to a terminal without interleaving.
	Progress func(done, total int, outcome Outcome)
}

func NewRunner(compiler *ctxcompiler.Compiler, profile ctxcompiler.Profile, answerer Answerer) *Runner {
	return &Runner{compiler: compiler, profile: profile, answerer: answerer}
}

// Prepare builds the two message sets for a task and reports whether the
// compiled one is genuinely smaller. It is exported because proving that a
// corpus applies pressure is a separate step from scoring it, and one worth
// doing before spending a model budget.
func (r *Runner) Prepare(ctx context.Context, task Task) (full, compiled []providers.Message, reachable bool, err error) {
	result, err := r.compiler.Compile(ctx, ctxcompiler.Request{Profile: r.profile, Fragments: task.Fragments})
	if err != nil {
		return nil, nil, false, err
	}
	if result.Report.CompressionRatio >= 1 {
		return nil, nil, false, fmt.Errorf("%w: compression ratio %.4f", ErrNoPressure, result.Report.CompressionRatio)
	}
	// Reachability is asked of the compiled text, not of the carrier fragment:
	// compaction never keeps a fragment verbatim, so asking whether the fragment
	// survived answers "no" every time and distinguishes nothing.
	compiledBody := ""
	for _, fragment := range result.Fragments {
		compiledBody += "\n" + fragment.Content
	}
	for _, assertion := range task.Assertions {
		if assertion.Kind == "contains" && strings.Contains(compiledBody, assertion.Value) {
			reachable = true
		}
	}
	fullMessages := messagesFor(task.Fragments, task.Prompt)
	if r.FullContextCeiling > 0 {
		estimator := r.Estimator
		if estimator == nil {
			estimator = ctxcompiler.NewAdaptiveEstimator()
		}
		size := 0
		for _, message := range fullMessages {
			size += estimator.Count(message.Content)
		}
		if size > r.FullContextCeiling {
			return nil, nil, false, fmt.Errorf("%w: %d tokens against a ceiling of %d",
				ErrFullContextTooLarge, size, r.FullContextCeiling)
		}
	}
	return fullMessages, messagesFor(result.Fragments, task.Prompt), reachable, nil
}

func messagesFor(fragments []ctxcompiler.Fragment, prompt string) []providers.Message {
	var body strings.Builder
	for _, fragment := range fragments {
		fmt.Fprintf(&body, "[%s:%s]\n%s\n\n", fragment.Kind, fragment.ID, fragment.Content)
	}
	return []providers.Message{
		{Role: "system", Content: strings.TrimSpace(body.String())},
		{Role: "user", Content: prompt},
	}
}

type job struct {
	task      Task
	condition string
	messages  []providers.Message
	reachable bool
}

// Run scores every task under both conditions and summarises per class.
func (r *Runner) Run(ctx context.Context, tasks []Task) (Report, error) {
	report := Report{StartedAt: time.Now().UTC(), Profile: r.profile.Name}
	var jobs []job
	for _, task := range tasks {
		full, compiled, reachable, err := r.Prepare(ctx, task)
		if err != nil {
			// A task that cannot apply pressure must stop the run rather than be
			// quietly scored: a corpus half of which is uncompacted reports a
			// smaller delta than the truth.
			return Report{}, fmt.Errorf("task %s: %w", task.ID, err)
		}
		jobs = append(jobs,
			job{task: task, condition: ConditionFull, messages: full, reachable: true},
			job{task: task, condition: ConditionCompiled, messages: compiled, reachable: reachable})
		if r.WithRetrieval {
			jobs = append(jobs, job{task: task, condition: ConditionRetrieval,
				messages: compiled, reachable: reachable})
		}
	}

	concurrency := r.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	outcomes := make([]Outcome, len(jobs))
	var mutex sync.Mutex
	done := 0
	tickets := make(chan struct{}, concurrency)
	var wait sync.WaitGroup
	for index, item := range jobs {
		wait.Add(1)
		go func(index int, item job) {
			defer wait.Done()
			tickets <- struct{}{}
			defer func() { <-tickets }()
			outcome := r.score(ctx, item)
			if item.condition == ConditionRetrieval {
				outcome = r.scoreWithRetrieval(ctx, item.task, item.messages)
				outcome.FactReachable = item.reachable
			}
			mutex.Lock()
			outcomes[index] = outcome
			done++
			if r.Progress != nil {
				r.Progress(done, len(jobs), outcome)
			}
			mutex.Unlock()
		}(index, item)
	}
	wait.Wait()

	report.Outcomes = outcomes
	report.Classes = summarise(outcomes)
	report.CompletedAt = time.Now().UTC()
	report.Passed = len(report.Classes) > 0
	for _, class := range report.Classes {
		if class.Verdict != "passed" {
			report.Passed = false
		}
	}
	return report, nil
}

func (r *Runner) score(ctx context.Context, item job) Outcome {
	outcome := Outcome{TaskID: item.task.ID, Class: item.task.Class, Condition: item.condition,
		FactReachable: item.reachable}
	completion, err := answerWithRetry(ctx, r.answerer, item.messages, nil)
	if err != nil {
		outcome.Error = err.Error()
		return outcome
	}
	outcome.Answer, outcome.PromptTokens = completion.Content, completion.Usage.PromptTokens
	outcome.EmptyAnswer = strings.TrimSpace(completion.Content) == ""
	outcome.Passed = true
	answer := strings.ToLower(completion.Content)
	for _, assertion := range item.task.Assertions {
		value := strings.ToLower(assertion.Value)
		holds := strings.Contains(answer, value)
		if assertion.Kind == "absent" {
			holds = !holds
		}
		if !holds {
			outcome.Passed = false
			outcome.FailedAssertions = append(outcome.FailedAssertions,
				fmt.Sprintf("%s:%s", assertion.Kind, assertion.Value))
		}
	}
	if !outcome.Passed {
		for _, claim := range item.task.FalseSuccessClaims {
			if strings.Contains(answer, strings.ToLower(claim)) {
				outcome.FalseSuccess = true
				break
			}
		}
	}
	return outcome
}

func summarise(outcomes []Outcome) []ClassResult {
	type counter struct {
		tasks                                        int
		passFull, passCompiled                       int
		falseFull, falseCompiled                     int
		reachable, errors, empty, compiledRun        int
		retrievalRun, retrievalPass, searched, found int
	}
	byClass := map[string]*counter{}
	for _, outcome := range outcomes {
		if _, ok := byClass[outcome.Class]; !ok {
			byClass[outcome.Class] = &counter{}
		}
		item := byClass[outcome.Class]
		if outcome.Error != "" {
			item.errors++
			continue
		}
		if outcome.EmptyAnswer {
			item.empty++
		}
		switch outcome.Condition {
		case ConditionFull:
			item.tasks++
			if outcome.Passed {
				item.passFull++
			}
			if outcome.FalseSuccess {
				item.falseFull++
			}
		case ConditionCompiled:
			item.compiledRun++
			if outcome.Passed {
				item.passCompiled++
			}
			if outcome.FalseSuccess {
				item.falseCompiled++
			}
			if outcome.FactReachable {
				item.reachable++
			}
		case ConditionRetrieval:
			item.retrievalRun++
			if outcome.Passed {
				item.retrievalPass++
			}
			if outcome.SearchCalls > 0 {
				item.searched++
			}
			if outcome.SearchFoundTheFact {
				item.found++
			}
		}
	}
	classes := make([]string, 0, len(byClass))
	for class := range byClass {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	results := make([]ClassResult, 0, len(classes))
	for _, class := range classes {
		item := byClass[class]
		result := ClassResult{Class: class, Tasks: item.tasks, Tolerance: ClassTolerance[class],
			FalseSuccessFull: item.falseFull, FalseSuccessCompiled: item.falseCompiled,
			FactsReachable: item.reachable, EmptyAnswers: item.empty, Errors: item.errors,
			RetrievalRuns: item.retrievalRun, RetrievalSearched: item.searched,
			RetrievalFound: item.found}
		result.FalseSuccessDelta = item.falseCompiled - item.falseFull
		if item.tasks > 0 {
			result.SuccessFull = float64(item.passFull) / float64(item.tasks)
		}
		if item.compiledRun > 0 {
			result.SuccessCompiled = float64(item.passCompiled) / float64(item.compiledRun)
		}
		result.SuccessDelta = result.SuccessFull - result.SuccessCompiled
		if item.retrievalRun > 0 {
			result.SuccessRetrieval = float64(item.retrievalPass) / float64(item.retrievalRun)
			result.RetrievalDelta = result.SuccessFull - result.SuccessRetrieval
		}
		result.Verdict = verdictFor(result, item.tasks)
		if item.tasks > 0 && item.reachable == item.compiledRun {
			result.Note = "every fact stayed reachable after compilation; the tasks may not be putting retention under real pressure"
		}
		results = append(results, result)
	}
	return results
}

func verdictFor(result ClassResult, tasks int) string {
	switch {
	case result.Errors > 0:
		return "errors"
	case tasks < MinimumTasksPerClass:
		return "insufficient_sample"
	case result.FalseSuccessDelta > 0:
		return "false_success_regressed"
	case result.SuccessDelta > result.Tolerance:
		return "regressed_beyond_tolerance"
	default:
		return "passed"
	}
}

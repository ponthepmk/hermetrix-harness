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
	Answer(ctx context.Context, messages []providers.Message) (providers.Completion, error)
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

// DefaultConcurrency mirrors the learning corpus scorer: independent requests,
// latency dominated by queueing.
const DefaultConcurrency = 4

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
func (r *Runner) Prepare(ctx context.Context, task Task) (full, compiled []providers.Message, retained bool, err error) {
	result, err := r.compiler.Compile(ctx, ctxcompiler.Request{Profile: r.profile, Fragments: task.Fragments})
	if err != nil {
		return nil, nil, false, err
	}
	if result.Report.CompressionRatio >= 1 {
		return nil, nil, false, fmt.Errorf("%w: compression ratio %.4f", ErrNoPressure, result.Report.CompressionRatio)
	}
	for _, fragment := range result.Fragments {
		if task.NeedleFragmentID != "" && fragment.ID == task.NeedleFragmentID {
			retained = true
		}
	}
	return messagesFor(task.Fragments, task.Prompt), messagesFor(result.Fragments, task.Prompt), retained, nil
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
	retained  bool
}

// Run scores every task under both conditions and summarises per class.
func (r *Runner) Run(ctx context.Context, tasks []Task) (Report, error) {
	report := Report{StartedAt: time.Now().UTC(), Profile: r.profile.Name}
	var jobs []job
	for _, task := range tasks {
		full, compiled, retained, err := r.Prepare(ctx, task)
		if err != nil {
			// A task that cannot apply pressure must stop the run rather than be
			// quietly scored: a corpus half of which is uncompacted reports a
			// smaller delta than the truth.
			return Report{}, fmt.Errorf("task %s: %w", task.ID, err)
		}
		jobs = append(jobs,
			job{task: task, condition: ConditionFull, messages: full, retained: true},
			job{task: task, condition: ConditionCompiled, messages: compiled, retained: retained})
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
		NeedleRetained: item.retained}
	completion, err := r.answerer.Answer(ctx, item.messages)
	if err != nil {
		outcome.Error = err.Error()
		return outcome
	}
	outcome.Answer, outcome.PromptTokens = completion.Content, completion.Usage.PromptTokens
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
		tasks                        int
		passFull, passCompiled       int
		falseFull, falseCompiled     int
		needles, errors, compiledRun int
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
			if outcome.NeedleRetained {
				item.needles++
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
			NeedlesRetained: item.needles, Errors: item.errors}
		result.FalseSuccessDelta = item.falseCompiled - item.falseFull
		if item.tasks > 0 {
			result.SuccessFull = float64(item.passFull) / float64(item.tasks)
		}
		if item.compiledRun > 0 {
			result.SuccessCompiled = float64(item.passCompiled) / float64(item.compiledRun)
		}
		result.SuccessDelta = result.SuccessFull - result.SuccessCompiled
		result.Verdict = verdictFor(result, item.tasks)
		if item.tasks > 0 && item.needles == item.compiledRun {
			result.Note = "every needle survived compilation; the tasks may not be putting retention under real pressure"
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

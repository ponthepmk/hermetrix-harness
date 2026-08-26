// Package taskeval answers the Phase 9 task-success gate and the Phase 8
// behavioral-evaluation gate with the same machinery, because they ask the same
// question in two places: does the harness still get the work right after it
// has compacted the context it was given?
//
// The gate reads: compare full context against compiled context; code-edit may
// regress at most 3 percentage points, summarisation and research at most 5,
// and false-success delta must be 0, measured over at least 30 tasks per class.
//
// Two design decisions are load-bearing.
//
// No judge model. A task is scored by assertions that either hold or do not --
// a string that must appear, a claim that must not. A judge model would make
// the gate's own reading non-deterministic, which is the problem the
// worst-of-N corpus scoring already had to work around in learning.
//
// Assertions must key on facts that compaction puts at risk. If a task can be
// answered from the recent tail, both conditions score the same and the delta
// is 0 by construction -- the instrument would report a pass for a compiler
// that threw everything away. Every task therefore plants what it asks about
// deep in the history, and the runner refuses a task whose compiled context
// came back uncompacted.
package taskeval

import (
	"time"

	ctxcompiler "hermetrix-harness/internal/context"
)

// Task classes carry different tolerances because a wrong code edit costs more
// than a thinner summary. The names match the gate.
const (
	ClassCodeEdit      = "code-edit"
	ClassSummarisation = "summarisation"
	ClassResearch      = "research"
)

// ClassTolerance is the largest success regression the gate allows, as a
// fraction. Anything not listed is not a recognised class.
var ClassTolerance = map[string]float64{
	ClassCodeEdit:      0.03,
	ClassSummarisation: 0.05,
	ClassResearch:      0.05,
}

// MinimumTasksPerClass is the gate's sample floor.
const MinimumTasksPerClass = 30

// Assertion is one checkable fact about an answer. Kind is "contains" for
// something the answer must say, "absent" for something it must not.
type Assertion struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	// Why records what this assertion is actually testing, so a reader can
	// tell a real check from an incidental string match.
	Why string `json:"why,omitempty"`
}

// Task is one unit of work presented twice: once with the whole history and
// once with the history the compiler produced.
type Task struct {
	ID       string `json:"id"`
	Class    string `json:"class"`
	Language string `json:"language"`
	// Fragments are the session history the task is asked against. They must be
	// large enough that compilation actually compacts, or the two conditions are
	// the same request.
	Fragments []ctxcompiler.Fragment `json:"fragments"`
	// Prompt is the instruction appended after the history.
	Prompt string `json:"prompt"`
	// Assertions decide success. A task succeeds when every one holds.
	Assertions []Assertion `json:"assertions"`
	// FalseSuccessClaims are phrases that assert the work was done. Saying one
	// while an assertion fails is a false success, which the gate treats as
	// worse than a plain failure and allows none of.
	FalseSuccessClaims []string `json:"false_success_claims,omitempty"`
	// NeedleFragmentID names the fragment the assertions depend on, so the
	// runner can report whether that fragment survived compilation. It is
	// diagnostic, not part of the score.
	NeedleFragmentID string `json:"needle_fragment_id,omitempty"`
	// Placement records where inside that fragment the fact sits: head, middle
	// or tail. Recorded rather than inferred, because inferring it from the text
	// would be a heuristic that quietly disagrees with the generator. It exists
	// so a report can separate "compaction dropped the fact" from "the model had
	// it and still got the answer wrong".
	Placement string `json:"placement,omitempty"`
	// Phrasing says whether the fact is stated in the question's own words
	// ("near") or in different words for the same thing ("far"). It replaced
	// placement as the dimension that decides anything: once the compactor
	// started ranking by relevance to the goal, position stopped mattering and
	// every placement came back reachable, so a corpus varying only position
	// could no longer register a loss.
	Phrasing string `json:"phrasing,omitempty"`
}

// Outcome is one task under one condition.
type Outcome struct {
	TaskID           string   `json:"task_id"`
	Class            string   `json:"class"`
	Condition        string   `json:"condition"`
	Passed           bool     `json:"passed"`
	FailedAssertions []string `json:"failed_assertions,omitempty"`
	FalseSuccess     bool     `json:"false_success"`
	// FactReachable reports whether the value the assertions ask for was still
	// present anywhere in the context the model received. It separates
	// "compaction dropped the fact" from "the model had it and still answered
	// wrong", which are different failures with different fixes.
	//
	// It replaced a check on whether the carrier fragment survived verbatim.
	// That check read false on every compiled run in the first real scoring --
	// compaction never keeps a fragment verbatim -- so it distinguished nothing.
	FactReachable bool `json:"fact_reachable"`
	PromptTokens  int  `json:"prompt_tokens"`
	// EmptyAnswer records that the provider returned no content at all. It is
	// not a wrong answer and should not be read as one: six full-context runs
	// came back empty in the first real scoring, and every non-empty answer in
	// that run was correct whenever the fact was present. A reasoning model
	// spends part of its output budget before it writes anything, which the
	// agent already accounts for in answerBudget and this runner did not.
	EmptyAnswer bool `json:"empty_answer,omitempty"`
	// SearchCalls counts context_search calls the model chose to make. Zero
	// beside a failed answer is the R-14 shape: the tool was there, described,
	// and the model did not reach for it.
	SearchCalls int `json:"search_calls,omitempty"`
	// SearchFoundTheFact records that a search result carried the value the
	// assertions ask for. It separates a model that searched badly from one
	// that searched well and answered badly anyway.
	SearchFoundTheFact bool   `json:"search_found_the_fact,omitempty"`
	Answer             string `json:"answer,omitempty"`
	Error              string `json:"error,omitempty"`
}

// ClassResult is the gate's unit of judgement.
type ClassResult struct {
	Class                string  `json:"class"`
	Tasks                int     `json:"tasks"`
	SuccessFull          float64 `json:"success_full"`
	SuccessCompiled      float64 `json:"success_compiled"`
	SuccessDelta         float64 `json:"success_delta"`
	Tolerance            float64 `json:"tolerance"`
	FalseSuccessFull     int     `json:"false_success_full"`
	FalseSuccessCompiled int     `json:"false_success_compiled"`
	FalseSuccessDelta    int     `json:"false_success_delta"`
	// FactsReachable counts compiled runs where the value the assertions ask
	// for was still somewhere in the context. A class where every fact stays
	// reachable is not evidence that compaction is safe -- it is evidence the
	// tasks were too easy.
	FactsReachable int `json:"facts_reachable"`
	// EmptyAnswers counts provider responses with no content. Reported beside
	// the score rather than folded into it: an empty answer says something
	// about the output budget, not about whether compaction kept what the task
	// needed.
	EmptyAnswers int `json:"empty_answers"`
	// SuccessRetrieval is the compiled condition with context_search available,
	// and RetrievalDelta is what remains of the gap once the model may go and
	// look. RetrievalSearched counts how often it chose to, which is a separate
	// question from whether searching helped.
	SuccessRetrieval  float64 `json:"success_retrieval,omitempty"`
	RetrievalDelta    float64 `json:"retrieval_delta,omitempty"`
	RetrievalRuns     int     `json:"retrieval_runs,omitempty"`
	RetrievalSearched int     `json:"retrieval_searched,omitempty"`
	RetrievalFound    int     `json:"retrieval_found,omitempty"`
	Errors            int     `json:"errors"`
	Verdict           string  `json:"verdict"`
	Note              string  `json:"note,omitempty"`
}

// Report is one whole run.
type Report struct {
	StartedAt   time.Time     `json:"started_at"`
	CompletedAt time.Time     `json:"completed_at"`
	Profile     string        `json:"profile"`
	Classes     []ClassResult `json:"classes"`
	Outcomes    []Outcome     `json:"outcomes,omitempty"`
	Passed      bool          `json:"passed"`
}

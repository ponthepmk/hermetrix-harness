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
}

// Outcome is one task under one condition.
type Outcome struct {
	TaskID           string   `json:"task_id"`
	Class            string   `json:"class"`
	Condition        string   `json:"condition"`
	Passed           bool     `json:"passed"`
	FailedAssertions []string `json:"failed_assertions,omitempty"`
	FalseSuccess     bool     `json:"false_success"`
	NeedleRetained   bool     `json:"needle_retained"`
	PromptTokens     int      `json:"prompt_tokens"`
	Answer           string   `json:"answer,omitempty"`
	Error            string   `json:"error,omitempty"`
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
	// NeedlesRetained counts compiled runs where the fragment the assertions
	// depend on survived. A class where every needle survives is not evidence
	// that compaction is safe -- it is evidence the tasks were too easy.
	NeedlesRetained int    `json:"needles_retained"`
	Errors          int    `json:"errors"`
	Verdict         string `json:"verdict"`
	Note            string `json:"note,omitempty"`
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

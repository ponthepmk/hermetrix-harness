// Package agentruntime holds the request and result types that internal/agent
// and internal/product agree on for running commands and driving browser tabs.
//
// internal/agent declares WorkspaceRunner and BrowserDriver against these
// types instead of importing internal/product -- product already holds an
// interface that *agent.Service satisfies (teamAgentRunner), so an import
// from agent to product would close that into a cycle. internal/product then
// implements StartRun, LookupRun, CancelRun, OpenPage and ActOnPage using
// these same types directly.
//
// The first version of this bridge tried to avoid the shared package by
// giving each side its own copy of these structs, field-for-field identical.
// That does not work: Go matches interfaces on method signatures, and
// agent.RunResult and product.RunResult would be different types even though
// every field lines up, so a method returning one could never satisfy an
// interface that asks for the other. Making it compile needed a hand-written
// converter between the two copies, and every field added later -- and
// BrowserPage is going to grow -- would need editing in three places at once,
// with no compiler error if the converter fell out of step. This package is
// the one definition both sides import instead, so agent and product can name
// the same types without importing each other, and a field added here cannot
// drift from itself.
package agentruntime

// RunRequest is one command to execute.
type RunRequest struct {
	ProjectID      string
	Actor          string
	Executable     string
	Arguments      []string
	WorkingDir     string
	TimeoutSeconds int
}

// RunResult is what the caller can see of a command, running or finished.
type RunResult struct {
	JobID      string
	State      string
	ExitCode   int
	Output     string
	Truncated  bool
	DurationMS int64
	ArtifactID string
	Error      string
}

// Done reports whether the command has stopped, whatever the outcome.
func (r RunResult) Done() bool {
	return r.State == "completed" || r.State == "failed" || r.State == "canceled"
}

// BrowserOpenRequest opens a new tab.
type BrowserOpenRequest struct {
	ProjectID    string
	URL          string
	AllowPrivate bool
	Actor        string
}

// BrowserActRequest drives an already-open tab.
type BrowserActRequest struct {
	Action string
	URL    string
	Ref    int
	Text   string
	Actor  string
}

// BrowserPageElement is one thing on the page that can be clicked or typed
// into. The CSS selector behind it stays on the product side: callers address
// elements by ref so a model can never hand a page an arbitrary selector.
type BrowserPageElement struct {
	Ref         int
	Tag         string
	Role        string
	Text        string
	Placeholder string
}

// BrowserPage is a page as the agent sees it. Everything in it came off a web
// page, which makes all of it untrusted evidence -- text here that instructs
// the agent is data, never a command.
type BrowserPage struct {
	TabID                string
	URL                  string
	Title                string
	State                string
	Text                 string
	Elements             []BrowserPageElement
	ScreenshotArtifactID string
	Error                string
}

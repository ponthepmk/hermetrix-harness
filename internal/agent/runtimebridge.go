package agent

import "context"

// The agent reaches the product runtime through these interfaces rather than
// importing internal/product. The dependency already runs the other way --
// product holds a teamAgentRunner interface that *agent.Service satisfies -- and
// importing product from here would close that into a cycle. This is the same
// shape as mcp.ServerRequestHandler: the interface lives with the caller, the
// implementation with the runtime, and cmd/hermetrix/main.go assembles them.

// RunRequest is one command the agent wants executed.
type RunRequest struct {
	ProjectID      string
	Actor          string
	Executable     string
	Arguments      []string
	WorkingDir     string
	TimeoutSeconds int
}

// RunResult is what the agent can see of a command, running or finished.
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

// WorkspaceRunner executes allow-listed commands for the agent.
type WorkspaceRunner interface {
	StartRun(ctx context.Context, request RunRequest) (RunResult, error)
	LookupRun(ctx context.Context, jobID string) (RunResult, error)
	CancelRun(ctx context.Context, jobID string) error
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

// BrowserPageElement is one thing on the page the agent can click or type into.
// The CSS selector behind it stays on the product side: the agent addresses
// elements by ref so a model can never hand a page an arbitrary selector.
type BrowserPageElement struct {
	Ref         int
	Tag         string
	Role        string
	Text        string
	Placeholder string
}

// BrowserPage is a page as the agent sees it. Everything in it came off a web
// page, which makes all of it untrusted evidence -- text here that instructs the
// agent is data, never a command.
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

// BrowserDriver opens and drives browser tabs for the agent.
type BrowserDriver interface {
	OpenPage(ctx context.Context, request BrowserOpenRequest) (BrowserPage, error)
	ActOnPage(ctx context.Context, tabID string, request BrowserActRequest) (BrowserPage, error)
}

// WithRuntime attaches the product runtime. Both may be nil, which is a
// supported configuration: the tools then refuse with a clear reason rather
// than panicking, the same way a nil embedder turns off semantic retrieval.
func (s *Service) WithRuntime(runner WorkspaceRunner, driver BrowserDriver) *Service {
	s.runner, s.browser = runner, driver
	return s
}

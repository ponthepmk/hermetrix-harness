package agent

import (
	"context"

	"hermetrix-harness/internal/agentruntime"
)

// The agent reaches the product runtime through these interfaces rather than
// importing internal/product. The dependency already runs the other way --
// product holds a teamAgentRunner interface that *agent.Service satisfies -- and
// importing product from here would close that into a cycle. This is the same
// shape as mcp.ServerRequestHandler: the interface lives with the caller, the
// implementation with the runtime, and cmd/hermetrix/main.go assembles them.
//
// The request and result types come from internal/agentruntime rather than
// being declared here, so that internal/product can implement these
// interfaces using the exact same types instead of a field-for-field copy of
// its own. See that package's doc comment for why a copy does not work.

// RunRequest, RunResult, BrowserOpenRequest, BrowserActRequest,
// BrowserPageElement and BrowserPage are aliases onto internal/agentruntime,
// kept here so this package and callers of it can keep writing agent.RunResult
// and so on. RunResult carries its Done() method through the alias unchanged.
type (
	RunRequest         = agentruntime.RunRequest
	RunResult          = agentruntime.RunResult
	BrowserOpenRequest = agentruntime.BrowserOpenRequest
	BrowserActRequest  = agentruntime.BrowserActRequest
	BrowserPageElement = agentruntime.BrowserPageElement
	BrowserPage        = agentruntime.BrowserPage
)

// WorkspaceRunner executes allow-listed commands for the agent.
type WorkspaceRunner interface {
	StartRun(ctx context.Context, request RunRequest) (RunResult, error)
	LookupRun(ctx context.Context, jobID string) (RunResult, error)
	CancelRun(ctx context.Context, jobID string) error
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

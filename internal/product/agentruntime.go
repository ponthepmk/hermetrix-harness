package product

import (
	"context"
	"fmt"
)

// The agent cannot import this package, so it declares the interfaces it needs
// and these methods answer them field-for-field. Go still treats agent.RunResult
// and product.RunResult as different types even though every field matches, so
// cmd/hermetrix/runtimebridge.go converts between the two at the one place that
// is allowed to import both. Keep the types below in step with the agent's.

type RunRequest struct {
	ProjectID      string
	Actor          string
	Executable     string
	Arguments      []string
	WorkingDir     string
	TimeoutSeconds int
}

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

func (r RunResult) Done() bool {
	return r.State == "completed" || r.State == "failed" || r.State == "canceled"
}

// StartRun queues a command and returns as soon as it is accepted. Every bound
// StartCommand enforces -- the executable allowlist, no shell, a minimal
// environment, a working directory inside the project, the output ceiling and
// process-group termination -- applies unchanged, because this is that call.
func (s *Service) StartRun(ctx context.Context, request RunRequest) (RunResult, error) {
	job, err := s.StartCommand(ctx, CommandInput{ProjectID: request.ProjectID, Actor: request.Actor,
		Executable: request.Executable, Arguments: request.Arguments, WorkingDir: request.WorkingDir,
		TimeoutSeconds: request.TimeoutSeconds})
	if err != nil {
		return RunResult{}, err
	}
	return runResultFromJob(job), nil
}

// LookupRun reads a command's current state.
func (s *Service) LookupRun(ctx context.Context, jobID string) (RunResult, error) {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return RunResult{}, err
	}
	if job.Kind != "command" {
		return RunResult{}, fmt.Errorf("job %s is not a command", jobID)
	}
	return runResultFromJob(job), nil
}

// CancelRun asks a command to stop. The runner terminates the process group, so
// a build that spawned children does not leave them behind.
func (s *Service) CancelRun(ctx context.Context, jobID string) error {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Kind != "command" {
		return fmt.Errorf("job %s is not a command", jobID)
	}
	_, err = s.CancelJob(ctx, jobID)
	return err
}

type BrowserOpenRequest struct {
	ProjectID    string
	URL          string
	AllowPrivate bool
	Actor        string
}

type BrowserActRequest struct {
	Action string
	URL    string
	Ref    int
	Text   string
	Actor  string
}

type BrowserPageElement struct {
	Ref         int
	Tag         string
	Role        string
	Text        string
	Placeholder string
}

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

// OpenPage opens a tab for the agent. URL validation, the private-host rule and
// the project binding are all OpenBrowserTab's, unchanged.
func (s *Service) OpenPage(ctx context.Context, request BrowserOpenRequest) (BrowserPage, error) {
	tab, err := s.OpenBrowserTab(ctx, OpenBrowserTabInput{ProjectID: request.ProjectID, URL: request.URL,
		AllowPrivate: request.AllowPrivate, Actor: request.Actor})
	if err != nil {
		return BrowserPage{}, err
	}
	return browserPageFromTab(tab), nil
}

// ActOnPage drives an open tab. The element's CSS selector never leaves this
// package: the agent addresses elements by ref, so a model cannot hand the page
// a selector of its own.
func (s *Service) ActOnPage(ctx context.Context, tabID string, request BrowserActRequest) (BrowserPage, error) {
	tab, err := s.BrowserAction(ctx, tabID, BrowserActionInput{Action: request.Action, URL: request.URL,
		Ref: request.Ref, Text: request.Text, Actor: request.Actor})
	if err != nil {
		return BrowserPage{}, err
	}
	return browserPageFromTab(tab), nil
}

func browserPageFromTab(tab BrowserTab) BrowserPage {
	page := BrowserPage{TabID: tab.ID, URL: tab.URL, Title: tab.Title, State: tab.State,
		Text: tab.TextSnapshot, ScreenshotArtifactID: tab.ScreenshotArtifactID, Error: tab.Error,
		Elements: make([]BrowserPageElement, 0, len(tab.Elements))}
	for _, element := range tab.Elements {
		page.Elements = append(page.Elements, BrowserPageElement{Ref: element.Ref, Tag: element.Tag,
			Role: element.Role, Text: element.Text, Placeholder: element.Placeholder})
	}
	return page
}

func runResultFromJob(job Job) RunResult {
	result := RunResult{JobID: job.ID, State: job.State, Error: job.Error}
	if value, ok := job.Result["exit_code"].(float64); ok {
		result.ExitCode = int(value)
	}
	if value, ok := job.Result["duration_ms"].(float64); ok {
		result.DurationMS = int64(value)
	}
	if value, ok := job.Result["output"].(string); ok {
		result.Output = value
	}
	if value, ok := job.Result["truncated"].(bool); ok {
		result.Truncated = value
	}
	if value, ok := job.Result["artifact_id"].(string); ok {
		result.ArtifactID = value
	}
	return result
}

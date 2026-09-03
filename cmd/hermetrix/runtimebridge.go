package main

import (
	"context"

	"hermetrix-harness/internal/agent"
	"hermetrix-harness/internal/product"
)

// internal/agent declares the runtime interfaces it needs and internal/product
// declares its own mirrored types so neither package has to import the other.
// That leaves the two sides field-for-field identical but not the same Go
// type, so *product.Service cannot be handed to agent.NewService(...).
// WithRuntime directly -- the compiler treats agent.RunResult and
// product.RunResult as different types even though every field matches. This
// is the glue that converts between them, and it belongs here rather than in
// either package: main is already the one place that imports both.
type agentRuntimeBridge struct {
	product *product.Service
}

func (b agentRuntimeBridge) StartRun(ctx context.Context, request agent.RunRequest) (agent.RunResult, error) {
	result, err := b.product.StartRun(ctx, product.RunRequest{ProjectID: request.ProjectID, Actor: request.Actor,
		Executable: request.Executable, Arguments: request.Arguments, WorkingDir: request.WorkingDir,
		TimeoutSeconds: request.TimeoutSeconds})
	return agentRunResult(result), err
}

func (b agentRuntimeBridge) LookupRun(ctx context.Context, jobID string) (agent.RunResult, error) {
	result, err := b.product.LookupRun(ctx, jobID)
	return agentRunResult(result), err
}

func (b agentRuntimeBridge) CancelRun(ctx context.Context, jobID string) error {
	return b.product.CancelRun(ctx, jobID)
}

func agentRunResult(result product.RunResult) agent.RunResult {
	return agent.RunResult{JobID: result.JobID, State: result.State, ExitCode: result.ExitCode, Output: result.Output,
		Truncated: result.Truncated, DurationMS: result.DurationMS, ArtifactID: result.ArtifactID, Error: result.Error}
}

func (b agentRuntimeBridge) OpenPage(ctx context.Context, request agent.BrowserOpenRequest) (agent.BrowserPage, error) {
	page, err := b.product.OpenPage(ctx, product.BrowserOpenRequest{ProjectID: request.ProjectID, URL: request.URL,
		AllowPrivate: request.AllowPrivate, Actor: request.Actor})
	return agentBrowserPage(page), err
}

func (b agentRuntimeBridge) ActOnPage(ctx context.Context, tabID string, request agent.BrowserActRequest) (agent.BrowserPage, error) {
	page, err := b.product.ActOnPage(ctx, tabID, product.BrowserActRequest{Action: request.Action, URL: request.URL,
		Ref: request.Ref, Text: request.Text, Actor: request.Actor})
	return agentBrowserPage(page), err
}

func agentBrowserPage(page product.BrowserPage) agent.BrowserPage {
	elements := make([]agent.BrowserPageElement, 0, len(page.Elements))
	for _, element := range page.Elements {
		elements = append(elements, agent.BrowserPageElement{Ref: element.Ref, Tag: element.Tag, Role: element.Role,
			Text: element.Text, Placeholder: element.Placeholder})
	}
	return agent.BrowserPage{TabID: page.TabID, URL: page.URL, Title: page.Title, State: page.State, Text: page.Text,
		Elements: elements, ScreenshotArtifactID: page.ScreenshotArtifactID, Error: page.Error}
}

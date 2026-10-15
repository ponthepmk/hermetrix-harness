package selfimprove

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hermetrix-harness/internal/agent"
	ctxcompiler "hermetrix-harness/internal/context"
)

type Client struct {
	base            string
	providerID      string
	remote          bool
	http            *http.Client
	profile         string
	contextOverride bool
}

func NewClient(server, providerID string, allowRemote bool) (*Client, error) {
	u, err := url.Parse(server)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("lab requires a loopback HTTP Harness URL")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("use a literal loopback IP address")
	}
	if strings.TrimSpace(providerID) == "" {
		return nil, fmt.Errorf("explicit provider ID required")
	}
	return &Client{base: strings.TrimRight(server, "/"), providerID: providerID, remote: allowRemote, profile: "compact-32k", http: &http.Client{Timeout: 180 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// ConfigureContext changes only new lab sessions. A reviewed override is
// recorded as an override, never represented as measured qualification.
func (c *Client) ConfigureContext(profile string, reviewOverride bool) error {
	p, ok := ctxcompiler.ProfileByName(profile)
	if !ok || p.Total > 98304 {
		return fmt.Errorf("lab profile must be known and at most 96k")
	}
	c.profile, c.contextOverride = profile, reviewOverride
	return nil
}

func (c *Client) post(ctx context.Context, path string, body any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, fmt.Errorf("Harness %s returned HTTP %d", path, response.StatusCode)
	}
	return response, nil
}

// Answer opens a fresh personal session for every observation. There is no
// workspace/project/tool authority and no reuse of another condition's history.
// Timeouts/uncertain responses are never automatically retried.
func (c *Client) Answer(ctx context.Context, title, prompt string) (o Outcome) {
	started := time.Now()
	defer func() { o.DurationMS = time.Since(started).Milliseconds() }()
	input := agent.CreateSessionInput{Title: "Lab: " + title, ProviderID: c.providerID, ContextProfile: c.profile}
	if c.contextOverride {
		input.QualificationOverride = &agent.QualificationOverrideInput{Actor: "self-improvement-lab-operator", Reason: "Reviewed bounded synthetic planning trial only; larger answer reserve after observed reasoning exhaustion. This is an explicit experimental override, not a qualification result."}
	}
	if c.remote {
		input.EgressPolicy = "remote_allowed"
		input.RemoteEgressApproval = &agent.EgressApprovalInput{Actor: "self-improvement-lab-operator", Reason: "Operator enabled --allow-remote for synthetic planning-only trials; no workspace files or real customer data."}
	}
	response, err := c.post(ctx, "/api/sessions", input)
	if err != nil {
		o.Error = err.Error()
		return o
	}
	var session agent.Session
	err = json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(&session)
	response.Body.Close()
	if err != nil {
		o.Error = err.Error()
		return o
	}
	o.SessionID = session.ID
	o.ProviderRevision = session.Contract.ProviderRevision
	o.CatalogHash = Hash(session.Contract.SkillCatalog)
	if session.ID == "" || session.ProjectID != "" || session.Contract.ProjectID != "" || len(session.Contract.ToolBindings) != 0 || session.ProviderID != c.providerID {
		o.Error = "session has unexpected project/tool/provider authority"
		return o
	}
	response, err = c.post(ctx, "/api/sessions/"+url.PathEscape(session.ID)+"/turns", agent.TurnInput{Content: prompt, IdempotencyKey: "planning-lab-" + session.ID})
	if err != nil {
		o.Error = err.Error()
		return o
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(io.LimitReader(response.Body, 8*1024*1024))
	scanner.Buffer(make([]byte, 65536), 2*1024*1024)
	completed := false
	for scanner.Scan() {
		var event agent.StreamEvent
		if err = json.Unmarshal(scanner.Bytes(), &event); err != nil {
			o.Error = "invalid Harness event stream"
			return o
		}
		if event.Approval != nil || event.Type == "approval_required" {
			o.Error = "unexpected tool approval; lab will not approve it"
			return o
		}
		if event.TurnID != "" {
			o.TurnID = event.TurnID
		}
		if event.Type == "failed" {
			o.Error = "Harness turn failed: " + event.Error
			return o
		}
		if event.Result != nil {
			if completed {
				o.Error = "duplicate turn result"
				return o
			}
			completed = true
			o.TurnID = event.Result.TurnID
			o.Answer = event.Result.AssistantEvent.Content
			o.Tokens = event.Result.Usage.TotalTokens
			if o.Tokens == 0 {
				o.Tokens = event.Result.Usage.PromptTokens + event.Result.Usage.CompletionTokens
			}
			if event.Result.Approval != nil || event.Result.FinishReason == "length" {
				o.Error = "incomplete or approval-bound turn"
				return o
			}
		}
	}
	if err = scanner.Err(); err != nil {
		o.Error = err.Error()
	} else if !completed {
		o.Error = "turn ended without a final receipt"
	}
	if o.Error == "" && (o.Tokens <= 0 || strings.TrimSpace(o.Answer) == "") {
		o.Error = "final receipt has no answer or measured token usage"
	}
	o.DurationMS = time.Since(started).Milliseconds()
	return o
}

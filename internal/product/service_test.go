package product

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hermetrix-harness/internal/agent"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
)

func testProductService(t *testing.T) (*Service, *skills.Service, *store.Store) {
	t.Helper()
	dataStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	skillService := skills.NewService(dataStore)
	service := NewService(dataStore, skillService)
	t.Cleanup(service.Close)
	return service, skillService, dataStore
}

func TestProjectArtifactSettingsAndExplicitMemorySafety(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "evidence.txt"), []byte("verified"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Evidence", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	files, err := service.BrowseProject(ctx, project.ID, ".")
	if err != nil || len(files) != 1 || files[0].Name != "evidence.txt" {
		t.Fatalf("files=%+v err=%v", files, err)
	}
	if _, err := service.BrowseProject(ctx, project.ID, "../"); err == nil {
		t.Fatal("project root escape was accepted")
	}
	artifact, err := service.CreateArtifact(ctx, ArtifactInput{ProjectID: project.ID, Name: "proof.md", Kind: "report",
		MIMEType: "text/markdown", Content: "# Proof\n\nVerified."})
	if err != nil {
		t.Fatal(err)
	}
	_, data, err := service.GetArtifact(ctx, artifact.ID)
	if err != nil || string(data) != "# Proof\n\nVerified." {
		t.Fatalf("artifact=%q err=%v", data, err)
	}
	if _, err := service.SaveSetting(ctx, "provider.token", map[string]any{"token": "must-not-persist"}); err == nil {
		t.Fatal("secret-like setting was persisted")
	}
	if _, err := service.SaveSetting(ctx, "ui.theme", map[string]any{"mode": "galaxy"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveMemory(ctx, MemoryInput{ScopeKind: "project", ScopeRef: project.ID, MemoryKind: "preference",
		Content: "ตอบภาษาไทย", Source: "agent"}); err == nil {
		t.Fatal("implicit agent memory became active")
	}
	memory, err := service.SaveMemory(ctx, MemoryInput{ScopeKind: "project", ScopeRef: project.ID, MemoryKind: "preference",
		Content: "ตอบภาษาไทย", Source: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ArchiveMemory(ctx, memory.ID); err != nil {
		t.Fatal(err)
	}
}

func TestWorkbenchFileOptimisticWriteAndAuditReceipt(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "notes.md")
	if err := os.WriteFile(path, []byte("# Before\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Workbench", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	document, err := service.ReadProjectFile(ctx, project.ID, "notes.md")
	if err != nil || document.SHA256 == "" || document.Content != "# Before\n" {
		t.Fatalf("document=%+v err=%v", document, err)
	}
	result, err := service.WriteProjectFile(ctx, project.ID, WriteFileInput{Path: "notes.md", Content: "# After\n",
		ExpectedSHA256: document.SHA256, Actor: "test-user"})
	if err != nil || result.Document.Content != "# After\n" || result.ReceiptArtifact.ID == "" || !strings.Contains(result.Diff, "-# Before") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := service.WriteProjectFile(ctx, project.ID, WriteFileInput{Path: "notes.md", Content: "stale",
		ExpectedSHA256: document.SHA256, Actor: "test-user"}); err == nil {
		t.Fatal("stale editor write was accepted")
	}
	if _, err := service.WriteProjectFile(ctx, project.ID, WriteFileInput{Path: "../escape.txt", Content: "escape", Actor: "test-user"}); err == nil {
		t.Fatal("project escape write was accepted")
	}
}

func TestBackgroundCommandIsDirectBoundedAuditableAndCancelable(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Commands", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartCommand(ctx, CommandInput{ProjectID: project.ID, Actor: "user", Executable: "sh",
		Arguments: []string{"-c", "echo unsafe"}}); err == nil {
		t.Fatal("shell executable was accepted")
	}
	job, err := service.StartCommand(ctx, CommandInput{ProjectID: project.ID, Actor: "user", Executable: "python3",
		Arguments: []string{"-c", "print('direct-command-ok')"}, TimeoutSeconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForJob(t, service, job.ID)
	if completed.State != "completed" || completed.Result["exit_code"] != float64(0) ||
		!strings.Contains(completed.Result["output"].(string), "direct-command-ok") {
		t.Fatalf("completed job=%+v", completed)
	}
	artifactID, _ := completed.Result["artifact_id"].(string)
	if artifactID == "" {
		t.Fatal("command output was not persisted as an artifact")
	}
	longJob, err := service.StartCommand(ctx, CommandInput{ProjectID: project.ID, Actor: "user", Executable: "python3",
		Arguments: []string{"-c", "import time; time.sleep(30)"}, TimeoutSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		current, _ := service.GetJob(ctx, longJob.ID)
		if current.State == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never started: %+v", current)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := service.CancelJob(ctx, longJob.ID); err != nil {
		t.Fatal(err)
	}
	canceled := waitForJob(t, service, longJob.ID)
	if canceled.State != "canceled" || !canceled.CancelRequested {
		t.Fatalf("canceled job=%+v", canceled)
	}
}

func TestInteractivePTYAcceptsInputStreamsOutputAndCloses(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	project, err := service.SaveProject(ctx, ProjectInput{Name: "PTY", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := service.StartTerminal(ctx, StartTerminalInput{ProjectID: project.ID, Shell: "sh", WorkingDir: ".",
		Actor: "test-user", Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.WriteTerminal(ctx, terminal.ID, "printf 'HERMETRIX_PTY_OK\\n'\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		output, readErr := service.TerminalOutput(ctx, terminal.ID, 0)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(output.Output, "HERMETRIX_PTY_OK") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("terminal output never arrived: %+v", output)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := service.ResizeTerminal(ctx, terminal.ID, 100, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CloseTerminal(ctx, terminal.ID); err != nil {
		t.Fatal(err)
	}
}

func TestManagedBrowserInteractsWithProjectPageAndCapturesEvidence(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root := t.TempDir()
	pagePath := filepath.Join(root, "browser-fixture.html")
	page := `<!doctype html><html><head><title>Hermetrix Browser Fixture</title></head><body>
<label>Name <input id="name" placeholder="Name"></label>
<button id="apply" onclick="document.querySelector('#result').textContent='Hello '+document.querySelector('#name').value">Apply</button>
<p id="result">Waiting</p></body></html>`
	if err := os.WriteFile(pagePath, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Browser", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	pageURL := (&url.URL{Scheme: "file", Path: pagePath}).String()
	tab, err := service.OpenBrowserTab(ctx, OpenBrowserTabInput{ProjectID: project.ID, URL: pageURL, Actor: "test-user"})
	if err != nil {
		if strings.Contains(err.Error(), "Chrome or Chromium is required") {
			t.Skip(err)
		}
		if strings.Contains(err.Error(), "signal: abort trap") {
			t.Skipf("test host sandbox prevented Chrome from starting: %v", err)
		}
		t.Fatal(err)
	}
	if tab.Title != "Hermetrix Browser Fixture" || !strings.Contains(tab.TextSnapshot, "Waiting") || tab.ScreenshotArtifactID == "" {
		t.Fatalf("initial browser tab=%+v", tab)
	}
	var inputRef, buttonRef int
	for _, element := range tab.Elements {
		if element.Placeholder == "Name" {
			inputRef = element.Ref
		}
		if element.Text == "Apply" {
			buttonRef = element.Ref
		}
	}
	if inputRef == 0 || buttonRef == 0 {
		t.Fatalf("interactive element references missing: %+v", tab.Elements)
	}
	if _, err := service.BrowserAction(ctx, tab.ID, BrowserActionInput{Action: "type", Ref: inputRef, Text: "Neurix", Actor: "test-user"}); err != nil {
		t.Fatal(err)
	}
	tab, err = service.BrowserAction(ctx, tab.ID, BrowserActionInput{Action: "click", Ref: buttonRef, Actor: "test-user"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tab.TextSnapshot, "Hello Neurix") {
		t.Fatalf("browser action did not update the page: %q", tab.TextSnapshot)
	}
	artifact, image, err := service.GetArtifact(ctx, tab.ScreenshotArtifactID)
	if err != nil || artifact.MIMEType != "image/png" || len(image) < 100 {
		t.Fatalf("screenshot artifact=%+v bytes=%d err=%v", artifact, len(image), err)
	}
	closed, err := service.BrowserAction(ctx, tab.ID, BrowserActionInput{Action: "close", Actor: "test-user"})
	if err != nil || closed.State != "closed" {
		t.Fatalf("closed tab=%+v err=%v", closed, err)
	}
}

func TestManagedBrowserURLPolicy(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	if _, err := service.validateBrowserURL(ctx, "", "http://127.0.0.1:9/", false); err == nil || !strings.Contains(err.Error(), "allow_private") {
		t.Fatalf("private browser URL was not refused explicitly: %v", err)
	}
	if _, err := service.validateBrowserURL(ctx, "", "https://user:secret@example.com/", false); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("credential-bearing browser URL was not refused: %v", err)
	}
	if _, err := service.validateBrowserURL(ctx, "", "javascript:alert(1)", false); err == nil || !strings.Contains(err.Error(), "only http") {
		t.Fatalf("active browser URL scheme was not refused: %v", err)
	}
}

// TestManagedBrowserRevalidatesTheURLChromeActuallyReached covers redirects,
// history and click-driven navigation without requiring Chrome. A public URL
// may resolve initially and then land on loopback; page text from that final
// destination must not enter the retained snapshot unless private access was
// explicitly granted for the tab.
func TestManagedBrowserRevalidatesTheURLChromeActuallyReached(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	snapshot := browserSnapshot{Title: "Local admin", URL: "http://127.0.0.1:9000/admin",
		Text: "private control panel", Links: []BrowserLink{{Text: "delete", URL: "/delete"}},
		Elements: []BrowserElement{{Ref: 1, Tag: "button", Text: "delete", Selector: "button"}}}
	blocked := &browserTabRuntime{tab: BrowserTab{URL: "https://public.example/start", AllowPrivate: false}}
	if err := service.acceptBrowserSnapshot(ctx, blocked, snapshot); err == nil || !strings.Contains(err.Error(), "disallowed URL") {
		t.Fatalf("private final navigation was accepted: %v", err)
	}
	if blocked.tab.TextSnapshot != "" || len(blocked.tab.Links) != 0 || len(blocked.tab.Elements) != 0 {
		t.Fatalf("content from a disallowed final URL entered the tab snapshot: %+v", blocked.tab)
	}

	approved := &browserTabRuntime{tab: BrowserTab{URL: "https://public.example/start", AllowPrivate: true}}
	if err := service.acceptBrowserSnapshot(ctx, approved, snapshot); err != nil {
		t.Fatalf("an explicitly private-enabled tab rejected its final URL: %v", err)
	}
	if approved.tab.TextSnapshot != snapshot.Text || approved.tab.URL != snapshot.URL {
		t.Fatalf("approved snapshot was not retained: %+v", approved.tab)
	}
}

func TestNativeOfficeDeliverablesAreRealAuditablePackages(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Deliverables", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		format   string
		required []string
	}{
		{format: "docx", required: []string{"[Content_Types].xml", "word/document.xml"}},
		{format: "xlsx", required: []string{"[Content_Types].xml", "xl/workbook.xml", "xl/worksheets/sheet1.xml"}},
		{format: "pptx", required: []string{"[Content_Types].xml", "ppt/presentation.xml", "ppt/slides/slide1.xml"}},
	}
	for _, test := range tests {
		t.Run(test.format, func(t *testing.T) {
			artifact, createErr := service.CreateDeliverable(ctx, DeliverableInput{ProjectID: project.ID, Format: test.format,
				Title: "Hermetrix สรุป", Actor: "test-user", Paragraphs: []string{"รองรับข้อความ Unicode"},
				Rows: [][]string{{"หัวข้อ", "ค่า"}, {"ภาษา", "ไทย"}}, Slides: []DeliverableSlide{{Title: "Hermetrix", Bullets: []string{"ภาษาไทย"}}}})
			if createErr != nil {
				t.Fatal(createErr)
			}
			stored, data, readErr := service.GetArtifact(ctx, artifact.ID)
			if readErr != nil || stored.Checksum == "" || stored.Metadata["created_by"] != "test-user" {
				t.Fatalf("artifact=%+v err=%v", stored, readErr)
			}
			archive, openErr := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if openErr != nil {
				t.Fatalf("%s is not a ZIP-based Office package: %v", test.format, openErr)
			}
			present := map[string]bool{}
			for _, file := range archive.File {
				present[file.Name] = true
			}
			for _, name := range test.required {
				if !present[name] {
					t.Fatalf("%s package is missing %s", test.format, name)
				}
			}
		})
	}
	pdf, err := service.CreateDeliverable(ctx, DeliverableInput{ProjectID: project.ID, Format: "pdf", Title: "Hermetrix report",
		Actor: "test-user", Paragraphs: []string{"Verified output"}})
	if err != nil {
		t.Fatal(err)
	}
	_, pdfData, err := service.GetArtifact(ctx, pdf.ID)
	if err != nil || !bytes.HasPrefix(pdfData, []byte("%PDF-1.7")) || !bytes.HasSuffix(pdfData, []byte("%%EOF\n")) {
		t.Fatalf("invalid PDF bytes=%d err=%v", len(pdfData), err)
	}
	if _, err := service.CreateDeliverable(ctx, DeliverableInput{Format: "pdf", Title: "รายงาน", Actor: "test-user"}); err == nil || !strings.Contains(err.Error(), "Unicode") {
		t.Fatalf("PDF Unicode limitation was not reported truthfully: %v", err)
	}
}

type fakeTeamAgent struct {
	mu               sync.Mutex
	next             int
	active           int
	maxActive        int
	titles           map[string]string
	prompts          []string
	started          chan struct{}
	stopped          chan struct{}
	block            chan struct{}
	approvalOnFirst  bool
	approvalIssued   bool
	approvalCalls    int
	approvalDecision agent.ApprovalDecisionInput
	approvalStarted  chan struct{}
	approvalStopped  chan struct{}
	approvalBlock    chan struct{}
}

func (f *fakeTeamAgent) CreateSession(_ context.Context, input agent.CreateSessionInput) (agent.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := fmt.Sprintf("fake-session-%d", f.next)
	if f.titles == nil {
		f.titles = map[string]string{}
	}
	f.titles[id] = input.Title
	return agent.Session{ID: id, Title: input.Title}, nil
}

func (f *fakeTeamAgent) RunTurn(ctx context.Context, sessionID string, input agent.TurnInput, _ func(agent.StreamEvent) error) (agent.TurnResult, error) {
	f.mu.Lock()
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	title := f.titles[sessionID]
	f.prompts = append(f.prompts, input.Content)
	f.mu.Unlock()
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()
	if f.block != nil {
		select {
		case <-ctx.Done():
			if f.stopped != nil {
				select {
				case f.stopped <- struct{}{}:
				default:
				}
			}
			return agent.TurnResult{}, ctx.Err()
		case <-f.block:
		}
	}
	select {
	case <-ctx.Done():
		return agent.TurnResult{}, ctx.Err()
	case <-time.After(40 * time.Millisecond):
	}
	f.mu.Lock()
	issueApproval := f.approvalOnFirst && !f.approvalIssued
	if issueApproval {
		f.approvalIssued = true
	}
	f.mu.Unlock()
	if issueApproval {
		return agent.TurnResult{FinishReason: "approval_required", Approval: &agent.ToolApproval{ID: "approval-team-1",
			SessionID: sessionID, State: "pending", Summary: "Write reviewed evidence", Preview: "plan.md\n+verified",
			Effect: "workspace_mutation"}, Usage: providers.Usage{PromptTokens: 90, CompletionTokens: 10, TotalTokens: 100}}, nil
	}
	return agent.TurnResult{AssistantEvent: agent.Event{Content: "verified result from " + title},
		Usage: providers.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}}, nil
}

func (f *fakeTeamAgent) DecideApproval(ctx context.Context, _ string, input agent.ApprovalDecisionInput,
	_ func(agent.StreamEvent) error) (agent.TurnResult, error) {
	f.mu.Lock()
	f.approvalCalls++
	f.approvalDecision = input
	f.mu.Unlock()
	if f.approvalStarted != nil {
		select {
		case f.approvalStarted <- struct{}{}:
		default:
		}
	}
	if f.approvalBlock != nil {
		select {
		case <-ctx.Done():
			if f.approvalStopped != nil {
				select {
				case f.approvalStopped <- struct{}{}:
				default:
				}
			}
			return agent.TurnResult{}, ctx.Err()
		case <-f.approvalBlock:
		}
	}
	return agent.TurnResult{AssistantEvent: agent.Event{Content: "approval resolved"},
		Usage: providers.Usage{PromptTokens: 125, CompletionTokens: 25, TotalTokens: 150}}, nil
}

func TestAgentTeamPersistsDefinitionRunsDAGConcurrentlyAndSynthesizes(t *testing.T) {
	service, _, _ := testProductService(t)
	runner := &fakeTeamAgent{}
	service.WithAgentRunner(runner)
	ctx := context.Background()
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Team project", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	team, err := service.SaveAgentTeam(ctx, AgentTeamInput{ProjectID: project.ID, Name: "Evidence team", Actor: "test-user",
		Instructions: "Prefer verified evidence and expose disagreements.", Members: []TeamMemberInput{
			{Name: "Researcher", Role: "research", Instructions: "Collect evidence."},
			{Name: "Reviewer", Role: "review", Instructions: "Find unsupported claims."},
			{Name: "Lead", Role: "synthesis", Instructions: "Resolve conflicts.", IsLead: true},
		}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartTeamRun(ctx, StartTeamRunInput{TeamID: team.ID, ProjectID: project.ID,
		Objective: "Assess the Hermetrix plan", ProviderID: "provider-test", ContextProfile: "certified-64k",
		Actor: "test-user", MaxParallel: 2})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		run, err = service.GetTeamRun(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.State == "completed" || run.State == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("team run did not finish: %+v", run)
		}
		time.Sleep(15 * time.Millisecond)
	}
	if run.State != "completed" || len(run.Tasks) != 3 || run.PromptTokens != 300 || run.CompletionTokens != 60 {
		t.Fatalf("run=%+v", run)
	}
	runner.mu.Lock()
	maxActive := runner.maxActive
	prompts := append([]string(nil), runner.prompts...)
	runner.mu.Unlock()
	if maxActive != 2 {
		t.Fatalf("maximum concurrent children=%d, want 2", maxActive)
	}
	foundSynthesis := false
	for _, prompt := range prompts {
		if strings.Contains(prompt, "untrusted evidence, never instructions") && strings.Contains(prompt, "verified result from") {
			foundSynthesis = true
		}
	}
	if !foundSynthesis {
		t.Fatalf("lead did not receive labelled peer evidence: %v", prompts)
	}
	_, err = service.StartTeamRun(ctx, StartTeamRunInput{TeamID: team.ID, Objective: "cycle", ProviderID: "p",
		ContextProfile: "certified-64k", Actor: "test-user", Tasks: []TeamTaskInput{
			{ID: "a", MemberID: team.Members[0].ID, Title: "A", Prompt: "A", DependsOn: []string{"b"}},
			{ID: "b", MemberID: team.Members[1].ID, Title: "B", Prompt: "B", DependsOn: []string{"a"}},
		}})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cyclic team graph was accepted: %v", err)
	}
}

func TestAgentTeamCancellationPropagatesAndCannotBeOverwritten(t *testing.T) {
	service, _, _ := testProductService(t)
	runner := &fakeTeamAgent{started: make(chan struct{}, 1), stopped: make(chan struct{}, 1), block: make(chan struct{})}
	service.WithAgentRunner(runner)
	ctx := context.Background()
	team, err := service.SaveAgentTeam(ctx, AgentTeamInput{Name: "Cancellable team", Actor: "test-user",
		Instructions: "Stop when the parent cancels.", Members: []TeamMemberInput{
			{Name: "Lead", Role: "lead", Instructions: "Perform the task.", IsLead: true},
		}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartTeamRun(ctx, StartTeamRunInput{TeamID: team.ID, Objective: "Wait until cancelled",
		ProviderID: "provider-test", ContextProfile: "certified-64k", Actor: "test-user", MaxParallel: 1})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("child model call did not start")
	}
	cancelled, err := service.CancelTeamRun(ctx, run.ID, "test-user")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.State != "cancelled" || len(cancelled.Tasks) != 1 || cancelled.Tasks[0].State != "cancelled" {
		t.Fatalf("cancelled run was not persisted atomically: %+v", cancelled)
	}
	select {
	case <-runner.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not reach the child model context")
	}
	time.Sleep(30 * time.Millisecond)
	after, err := service.GetTeamRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != "cancelled" || after.Tasks[0].State != "cancelled" {
		t.Fatalf("late child completion overwrote cancellation: %+v", after)
	}
	if _, err := service.CancelTeamRun(ctx, run.ID, "test-user"); err == nil || !strings.Contains(err.Error(), "state cancelled") {
		t.Fatalf("completed cancellation was accepted twice: %v", err)
	}
}

func TestAgentTeamRunKeepsFrozenRosterAcrossDefinitionEdit(t *testing.T) {
	service, _, _ := testProductService(t)
	block := make(chan struct{})
	runner := &fakeTeamAgent{started: make(chan struct{}, 2), block: block}
	service.WithAgentRunner(runner)
	ctx := context.Background()
	team, err := service.SaveAgentTeam(ctx, AgentTeamInput{Name: "Snapshot team", Actor: "test-user",
		Instructions: "ORIGINAL UNIT RULES", Members: []TeamMemberInput{
			{Name: "Researcher", Role: "research", Instructions: "ORIGINAL RESEARCH INSTRUCTIONS"},
			{Name: "Lead", Role: "synthesis", Instructions: "ORIGINAL LEAD INSTRUCTIONS", IsLead: true},
		}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartTeamRun(ctx, StartTeamRunInput{TeamID: team.ID, Objective: "Prove frozen roles",
		ProviderID: "provider-test", ContextProfile: "certified-64k", Actor: "test-user", MaxParallel: 1})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first frozen task did not start")
	}
	var lead TeamMember
	for _, member := range team.Members {
		if member.IsLead {
			lead = member
		}
	}
	updated, err := service.SaveAgentTeam(ctx, AgentTeamInput{ID: team.ID, ExpectedRevision: team.Revision,
		Name: "Mutated team", Actor: "test-user", Instructions: "MUTATED UNIT RULES", Members: []TeamMemberInput{
			{ID: lead.ID, Name: "Lead renamed", Role: "changed-role", Instructions: "MUTATED LEAD INSTRUCTIONS", IsLead: true},
		}})
	if err != nil {
		t.Fatalf("historical task foreign key blocked a safe roster edit: %v", err)
	}
	if len(updated.Members) != 1 || updated.Members[0].Name != "Lead renamed" {
		t.Fatalf("team definition was not updated independently: %+v", updated)
	}
	close(block)
	deadline := time.Now().Add(3 * time.Second)
	for {
		run, err = service.GetTeamRun(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.State == "completed" || run.State == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("frozen run did not finish: %+v", run)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.State != "completed" || run.TeamName != "Snapshot team" || run.TeamInstructions != "ORIGINAL UNIT RULES" {
		t.Fatalf("run-level team snapshot drifted: %+v", run)
	}
	runner.mu.Lock()
	prompts := append([]string(nil), runner.prompts...)
	runner.mu.Unlock()
	joined := strings.Join(prompts, "\n---\n")
	if !strings.Contains(joined, "ORIGINAL UNIT RULES") || !strings.Contains(joined, "ORIGINAL LEAD INSTRUCTIONS") ||
		strings.Contains(joined, "MUTATED UNIT RULES") || strings.Contains(joined, "MUTATED LEAD INSTRUCTIONS") {
		t.Fatalf("run read mutable roster instructions after it started: %s", joined)
	}
	if len(run.Tasks) != 2 || run.Tasks[0].MemberName == "" || run.Tasks[1].MemberRole == "" {
		t.Fatalf("task provenance lacks frozen member snapshots: %+v", run.Tasks)
	}
}

func TestAgentTeamApprovalSurvivesRecoveryAndResumesSameDAGWithoutReplay(t *testing.T) {
	service, _, _ := testProductService(t)
	runner := &fakeTeamAgent{approvalOnFirst: true}
	service.WithAgentRunner(runner)
	ctx := context.Background()
	team, err := service.SaveAgentTeam(ctx, AgentTeamInput{Name: "Approval team", Actor: "test-user",
		Instructions: "Require exact approval receipts.", Members: []TeamMemberInput{
			{Name: "Writer", Role: "writer", Instructions: "Propose the exact write."},
			{Name: "Lead", Role: "synthesis", Instructions: "Continue after the receipt.", IsLead: true},
		}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartTeamRun(ctx, StartTeamRunInput{TeamID: team.ID, Objective: "Create reviewed evidence",
		ProviderID: "provider-test", ContextProfile: "certified-64k", Actor: "test-user", MaxParallel: 1,
		QualificationReason: "test override snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		run, err = service.GetTeamRun(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.State == "awaiting_approval" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("team did not pause for child approval: %+v", run)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var waitingTask, queuedTask TeamTask
	for _, task := range run.Tasks {
		if task.State == "awaiting_approval" {
			waitingTask = task
		} else if task.State == "queued" {
			queuedTask = task
		}
	}
	if run.QualificationReason != "test override snapshot" || waitingTask.ApprovalID != "approval-team-1" ||
		waitingTask.ApprovalPreview == "" || queuedTask.ID == "" {
		t.Fatalf("approval pause lacks persisted resume evidence: %+v", run)
	}
	if _, err := service.RecoverInterruptedJobs(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.GetTeamRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, task := range recovered.Tasks {
		states[task.ID] = task.State
	}
	if recovered.State != "awaiting_approval" || states[waitingTask.ID] != "awaiting_approval" || states[queuedTask.ID] != "queued" {
		t.Fatalf("durable pending approval was destroyed by restart recovery: %+v", recovered)
	}
	if _, err := service.DecideTeamTaskApproval(ctx, run.ID, waitingTask.ID,
		TeamApprovalDecisionInput{Actor: "test-user", Decision: "approve", Reason: "preview verified"}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		run, err = service.GetTeamRun(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.State == "completed" || run.State == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("team did not resume after approval: %+v", run)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var resolvedTask TeamTask
	for _, task := range run.Tasks {
		if task.ID == waitingTask.ID {
			resolvedTask = task
		}
	}
	if run.State != "completed" || resolvedTask.Result != "approval resolved" ||
		run.PromptTokens != 225 || run.CompletionTokens != 45 {
		t.Fatalf("approval did not resume the exact DAG: %+v", run)
	}
	runner.mu.Lock()
	prompts := append([]string(nil), runner.prompts...)
	approvalCalls, decision := runner.approvalCalls, runner.approvalDecision
	runner.mu.Unlock()
	if len(prompts) != 2 || approvalCalls != 1 || decision.Decision != "approve" || decision.Reason != "preview verified" {
		t.Fatalf("child prompt/effect was replayed or decision provenance lost: prompts=%d calls=%d decision=%+v", len(prompts), approvalCalls, decision)
	}
	if _, err := service.DecideTeamTaskApproval(ctx, run.ID, waitingTask.ID,
		TeamApprovalDecisionInput{Actor: "test-user", Decision: "approve"}); err == nil {
		t.Fatal("resolved team approval was accepted twice")
	}
}

func TestAgentTeamCancellationWinsWhileApprovalIsResolving(t *testing.T) {
	service, _, _ := testProductService(t)
	runner := &fakeTeamAgent{approvalOnFirst: true, approvalStarted: make(chan struct{}, 1),
		approvalStopped: make(chan struct{}, 1), approvalBlock: make(chan struct{})}
	service.WithAgentRunner(runner)
	ctx := context.Background()
	team, err := service.SaveAgentTeam(ctx, AgentTeamInput{Name: "Approval cancellation team", Actor: "test-user",
		Instructions: "Cancellation remains authoritative.", Members: []TeamMemberInput{
			{Name: "Lead", Role: "lead", Instructions: "Request one reviewed effect.", IsLead: true},
		}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartTeamRun(ctx, StartTeamRunInput{TeamID: team.ID, Objective: "Cancel during approval",
		ProviderID: "provider-test", ContextProfile: "certified-64k", Actor: "test-user", MaxParallel: 1})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		run, err = service.GetTeamRun(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.State == "awaiting_approval" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("team did not pause for approval: %+v", run)
		}
		time.Sleep(10 * time.Millisecond)
	}
	decisionDone := make(chan error, 1)
	go func() {
		_, decisionErr := service.DecideTeamTaskApproval(ctx, run.ID, run.Tasks[0].ID,
			TeamApprovalDecisionInput{Actor: "test-user", Decision: "approve", Reason: "reviewed"})
		decisionDone <- decisionErr
	}()
	select {
	case <-runner.approvalStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("approval resolution did not start")
	}
	cancelled, err := service.CancelTeamRun(ctx, run.ID, "test-user")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.State != "cancelled" || cancelled.Tasks[0].State != "cancelled" {
		t.Fatalf("cancellation was not persisted while approval resolved: %+v", cancelled)
	}
	select {
	case <-runner.approvalStopped:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not reach the approval resolution context")
	}
	select {
	case <-decisionDone:
	case <-time.After(2 * time.Second):
		t.Fatal("approval decision did not return after cancellation")
	}
	after, err := service.GetTeamRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != "cancelled" || after.Tasks[0].State != "cancelled" {
		t.Fatalf("late approval result overwrote cancellation: %+v", after)
	}
}

func TestMinimalEnvironmentProvidesGoCachesWithoutLeakingCredentials(t *testing.T) {
	t.Setenv("HERMETRIX_TEST_SECRET", "must-not-leak")
	environment := minimalEnvironment()
	joined := strings.Join(environment, "\n")
	for _, required := range []string{"GOPATH=", "GOMODCACHE=", "GOCACHE="} {
		if !strings.Contains(joined, required) {
			t.Fatalf("minimal environment lacks %s: %v", required, environment)
		}
	}
	if strings.Contains(joined, "HERMETRIX_TEST_SECRET") || strings.Contains(joined, "must-not-leak") {
		t.Fatal("minimal command environment leaked an unrelated credential")
	}
}

func TestBackupIntegrityConflictPreviewAndCandidateOnlyRestore(t *testing.T) {
	service, skillService, _ := testProductService(t)
	ctx := context.Background()
	markdown := "---\nname: backup-skill\ndescription: \"Repeatable backup evidence procedure\"\ntags: [backup]\ntools: [filesystem.read]\n---\n\n# Procedure\n\n1. Verify the evidence.\n"
	candidate, err := skillService.CreateCandidate(ctx, skills.CreateCandidateInput{CanonicalName: "backup-skill", ScopeKind: "user",
		Origin: "user_created", Owner: "user", ChangeKind: "create", CreatedBy: "user", TriggerKind: "manual",
		Reason: "backup test", EvidenceRefs: []string{"session:test"}, Markdown: markdown})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skillService.PromoteCandidate(ctx, candidate.ID, "user", candidate.Revision); err != nil {
		t.Fatal(err)
	}
	run, data, err := service.ExportBackup(ctx, "user")
	if err != nil || run.State != "completed" || len(data) == 0 {
		t.Fatalf("run=%+v size=%d err=%v", run, len(data), err)
	}
	var tampered map[string]any
	if err := json.Unmarshal(data, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered["payload_checksum"] = strings.Repeat("0", 64)
	bad, _ := json.Marshal(tampered)
	if _, err := service.PreviewImport(ctx, bad, "user"); err == nil {
		t.Fatal("tampered backup passed preview")
	}
	preview, err := service.PreviewImport(ctx, data, "user")
	if err != nil {
		t.Fatal(err)
	}
	if preview.SkillConflicts != 1 || preview.State != "awaiting_apply" {
		t.Fatalf("preview=%+v", preview)
	}
	result, err := service.ApplyImport(ctx, preview.ID, "user")
	if err != nil {
		t.Fatal(err)
	}
	if result.Conflicts != 1 || len(result.CandidateIDs) != 1 || result.State != "imported" {
		t.Fatalf("result=%+v", result)
	}
	active, err := skillService.ListSkills(ctx, false)
	if err != nil || len(active) != 1 {
		t.Fatalf("import mutated active skills: %+v err=%v", active, err)
	}
	restored, err := skillService.GetCandidate(ctx, result.CandidateIDs[0])
	if err != nil || restored.Origin != "imported" || !strings.Contains(restored.Reason, "conflict") {
		t.Fatalf("restored candidate=%+v err=%v", restored, err)
	}
	if _, err := service.ApplyImport(ctx, preview.ID, "user"); err == nil {
		t.Fatal("same import preview was applied twice")
	}
}

func waitForJob(t *testing.T, service *Service, id string) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		item, err := service.GetJob(context.Background(), id)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if item.State == "completed" || item.State == "failed" || item.State == "canceled" || item.State == "interrupted" {
			return item
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s did not finish: %+v", id, item)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestImportReportsTheTablesItDidNotRestore covers the gap between what export
// writes and what import reads. Export serialises the whole database -- every
// session, event, provider profile and snapshot. Import reads Skills and turns
// them into candidates. A live restore into an empty instance produced three
// candidates and nothing else from a file carrying 210 events and 4 sessions,
// while reporting state "imported" and zero conflicts.
//
// The asymmetry is a design question. Reporting it is not: a result that says
// only "imported" reads as a restore that worked.
func TestImportReportsTheTablesItDidNotRestore(t *testing.T) {
	tables := map[string][]map[string]any{
		"skills":         {{"canonical_name": "a"}},
		"skill_versions": {{"id": "v1"}},
		"agent_events":   {{"id": "e1"}, {"id": "e2"}},
		"agent_sessions": {{"id": "s1"}},
		"artifacts":      {},
	}
	notRestored := tablesNotRestored(tables)
	if notRestored["skills"] != 0 || notRestored["skill_versions"] != 0 {
		t.Fatalf("tables the import does read were reported as dropped: %v", notRestored)
	}
	if notRestored["agent_events"] != 2 || notRestored["agent_sessions"] != 1 {
		t.Fatalf("not_restored = %v, want agent_events 2 and agent_sessions 1", notRestored)
	}
	if _, present := notRestored["artifacts"]; present {
		t.Fatalf("an empty table was reported as dropped data: %v", notRestored)
	}
	if tablesNotRestored(map[string][]map[string]any{"skills": {{"canonical_name": "a"}}}) != nil {
		t.Fatal("a file holding only restorable tables should report nothing dropped")
	}
}

// TestProjectWithoutCodeIsOrdinaryButHonest covers both halves of the rule: a
// project may have no code, and every tool that needs code must say that is why
// it refused rather than reporting a bad path.
func TestProjectWithoutCodeIsOrdinaryButHonest(t *testing.T) {
	ctx := context.Background()
	service, _, _ := testProductService(t)

	life, err := service.SaveProject(ctx, ProjectInput{Name: "Daily life"})
	if err != nil {
		t.Fatalf("a project with no code folder was refused: %v", err)
	}
	if life.RootPath != "" {
		t.Errorf("root = %q, want empty", life.RootPath)
	}
	if _, err := service.RequireRoot(ctx, life.ID); !errors.Is(err, ErrProjectHasNoCode) {
		t.Errorf("RequireRoot said %v, want ErrProjectHasNoCode", err)
	}

	codeRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(codeRoot, "notes.md"), []byte("# hi\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	code, err := service.SaveProject(ctx, ProjectInput{Name: "Code", RootPath: codeRoot})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RequireRoot(ctx, code.ID); err != nil {
		t.Errorf("RequireRoot refused a project that has a root: %v", err)
	}

	// A path that was typed and is wrong is still an error, and a different one.
	if _, err := service.SaveProject(ctx, ProjectInput{Name: "Missing", RootPath: "/no/such/place"}); err == nil {
		t.Error("a root that does not exist was accepted")
	} else if errors.Is(err, ErrProjectHasNoCode) {
		t.Error("a wrong path was reported as a project with no code folder")
	}

	// RequireRoot being correct in isolation is not the fix: the finding was
	// that nothing called it, so every one of these consumers used to fall
	// through resolveInside's empty-root default and quietly read or write
	// wherever the Hermetrix process happened to be launched from. Each of
	// these has to name the missing folder for `life`, the same as RequireRoot
	// above, and still work normally for `code`, which has one.
	t.Run("BrowseProject", func(t *testing.T) {
		if _, err := service.BrowseProject(ctx, life.ID, "."); !errors.Is(err, ErrProjectHasNoCode) {
			t.Errorf("BrowseProject on a codeless project said %v, want ErrProjectHasNoCode", err)
		}
		files, err := service.BrowseProject(ctx, code.ID, ".")
		if err != nil || len(files) != 1 || files[0].Name != "notes.md" {
			t.Fatalf("BrowseProject on a project with a root: files=%+v err=%v", files, err)
		}
	})

	t.Run("ReadProjectFile", func(t *testing.T) {
		if _, err := service.ReadProjectFile(ctx, life.ID, "notes.md"); !errors.Is(err, ErrProjectHasNoCode) {
			t.Errorf("ReadProjectFile on a codeless project said %v, want ErrProjectHasNoCode", err)
		}
		document, err := service.ReadProjectFile(ctx, code.ID, "notes.md")
		if err != nil || document.Content != "# hi\n" {
			t.Fatalf("ReadProjectFile on a project with a root: document=%+v err=%v", document, err)
		}
	})

	t.Run("WriteProjectFile", func(t *testing.T) {
		if _, err := service.WriteProjectFile(ctx, life.ID, WriteFileInput{Path: "notes.md", Content: "x", Actor: "test-user"}); !errors.Is(err, ErrProjectHasNoCode) {
			t.Errorf("WriteProjectFile on a codeless project said %v, want ErrProjectHasNoCode", err)
		}
		result, err := service.WriteProjectFile(ctx, code.ID, WriteFileInput{Path: "written-by-test.md", Content: "ok", Actor: "test-user"})
		if err != nil || result.Document.Content != "ok" {
			t.Fatalf("WriteProjectFile on a project with a root: result=%+v err=%v", result, err)
		}
	})

	t.Run("StartCommand", func(t *testing.T) {
		if _, err := service.StartCommand(ctx, CommandInput{ProjectID: life.ID, Actor: "user", Executable: "ls"}); !errors.Is(err, ErrProjectHasNoCode) {
			t.Errorf("StartCommand on a codeless project said %v, want ErrProjectHasNoCode", err)
		}
		job, err := service.StartCommand(ctx, CommandInput{ProjectID: code.ID, Actor: "user", Executable: "ls"})
		if err != nil {
			t.Fatalf("StartCommand on a project with a root: %v", err)
		}
		if completed := waitForJob(t, service, job.ID); completed.State != "completed" {
			t.Fatalf("StartCommand on a project with a root did not complete: %+v", completed)
		}
	})

	t.Run("StartTerminal", func(t *testing.T) {
		if _, err := service.StartTerminal(ctx, StartTerminalInput{ProjectID: life.ID, Actor: "user", Shell: "sh"}); !errors.Is(err, ErrProjectHasNoCode) {
			t.Errorf("StartTerminal on a codeless project said %v, want ErrProjectHasNoCode", err)
		}
		terminal, err := service.StartTerminal(ctx, StartTerminalInput{ProjectID: code.ID, Actor: "user", Shell: "sh"})
		if err != nil {
			t.Fatalf("StartTerminal on a project with a root: %v", err)
		}
		if _, err := service.CloseTerminal(ctx, terminal.ID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("BrowserFileURL", func(t *testing.T) {
		if _, err := service.validateBrowserURL(ctx, life.ID, "file:///whatever", false); !errors.Is(err, ErrProjectHasNoCode) {
			t.Errorf("file:// URL against a codeless project said %v, want ErrProjectHasNoCode", err)
		}
		pageURL := (&url.URL{Scheme: "file", Path: filepath.Join(codeRoot, "notes.md")}).String()
		if _, err := service.validateBrowserURL(ctx, code.ID, pageURL, false); err != nil {
			t.Errorf("file:// URL against a project with a root: %v", err)
		}
	})
}

package product

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"hermetrix-harness/internal/identity"
)

var ErrDebuggerConflict = errors.New("debugger state conflict")
var ErrDebuggerUnavailable = errors.New("debugger unavailable")

type DebugRuntimeCapability struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}
type DebugCapabilities struct {
	Runtimes    []DebugRuntimeCapability `json:"runtimes"`
	Limitations []string                 `json:"limitations"`
}
type DebugBreakpoint struct {
	ID       string `json:"id,omitempty"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Verified bool   `json:"verified"`
}
type DebugFrame struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}
type DebugVariable struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}
type DebugSession struct {
	ID              string            `json:"id"`
	ProjectID       string            `json:"project_id"`
	Runtime         string            `json:"runtime"`
	Program         string            `json:"program"`
	State           string            `json:"state"`
	Reason          string            `json:"reason,omitempty"`
	Output          string            `json:"output"`
	OutputTruncated bool              `json:"output_truncated"`
	Error           string            `json:"error,omitempty"`
	ExitCode        *int              `json:"exit_code,omitempty"`
	Stack           []DebugFrame      `json:"stack"`
	Breakpoints     []DebugBreakpoint `json:"breakpoints"`
	Sandbox         commandSandbox    `json:"sandbox"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	ExpiresAt       time.Time         `json:"expires_at"`
}
type DebugStartInput struct {
	ProjectID      string            `json:"project_id"`
	Runtime        string            `json:"runtime"`
	Program        string            `json:"program"`
	Args           []string          `json:"args"`
	Breakpoints    []DebugBreakpoint `json:"breakpoints"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
}

type debugRuntime struct {
	mu            sync.Mutex
	opMu          sync.Mutex
	item          DebugSession
	root          string
	client        *inspectorClient
	dap           *dapClient
	threadID      int
	pauseVersion  int
	cancel        context.CancelFunc
	done          chan struct{}
	scripts       map[string]string
	scopes        map[string][]string
	stopRequested bool
	failure       string
}

func (s *Service) DebugCapabilities() DebugCapabilities {
	_, err := exec.LookPath("node")
	node := DebugRuntimeCapability{ID: "node", Label: "JavaScript · Node.js", Available: err == nil}
	if err != nil {
		node.Reason = "Install Node.js and restart Hermetrix to debug JavaScript."
	}
	_, goErr := debugExecutable("go")
	goRuntime := DebugRuntimeCapability{ID: "go", Label: "Go · Delve", Available: goErr == nil}
	if goErr != nil {
		goRuntime.Reason = "Install Go and Delve; set HERMETRIX_DLV_PATH to the absolute dlv executable path."
	}
	if os.Getenv("HERMETRIX_REQUIRE_OS_SANDBOX") == "1" {
		node.Available = false
		node.Reason = "Interactive debugging is unavailable when a strict OS sandbox is required."
		goRuntime.Available = false
		goRuntime.Reason = node.Reason
	}
	return DebugCapabilities{Runtimes: []DebugRuntimeCapability{goRuntime, node}, Limitations: []string{"Runs saved Go programs or package directories and JavaScript files (.js, .cjs, .mjs); TypeScript source maps and browser debugging are not supported.", "Sessions end when Hermetrix stops; debug programs run with local process permissions and may modify files or use the network.", "At most 4 active sessions; 30-minute default time limit. Local variables are read only; no expression evaluation."}}
}

func (s *Service) StartDebugSession(ctx context.Context, input DebugStartInput) (DebugSession, error) {
	if input.Runtime != "node" && input.Runtime != "go" {
		return DebugSession{}, fmt.Errorf("%w: choose an available Go or Node.js runtime", ErrDebuggerUnavailable)
	}
	if os.Getenv("HERMETRIX_REQUIRE_OS_SANDBOX") == "1" {
		return DebugSession{}, fmt.Errorf("%w: interactive debugging cannot provide the required OS sandbox", ErrDebuggerUnavailable)
	}
	project, err := s.GetProject(ctx, input.ProjectID)
	if err != nil {
		return DebugSession{}, err
	}
	root, err := requireRoot(project)
	if err != nil {
		return DebugSession{}, err
	}
	program, err := resolveInside(root, input.Program, true)
	if err != nil {
		return DebugSession{}, err
	}
	info, err := os.Stat(program)
	if err != nil || (!info.Mode().IsRegular() && !(input.Runtime == "go" && info.IsDir())) {
		return DebugSession{}, errors.New("debug program must be a saved file or Go package in the project")
	}
	if input.Runtime == "go" {
		if !info.IsDir() && strings.ToLower(filepath.Ext(program)) != ".go" {
			return DebugSession{}, errors.New("Go debugging requires a .go file or package directory")
		}
	} else {
		switch strings.ToLower(filepath.Ext(program)) {
		case ".js", ".cjs", ".mjs":
		default:
			return DebugSession{}, errors.New("Node debugging requires a saved .js, .cjs or .mjs file")
		}
	}
	if len(input.Args) > 64 || containsSensitiveArgument(input.Args) {
		return DebugSession{}, errors.New("debug arguments exceed bounds or contain credentials")
	}
	for _, arg := range input.Args {
		if len(arg) > 8192 || strings.ContainsRune(arg, 0) {
			return DebugSession{}, errors.New("invalid debug argument")
		}
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 1800
	}
	if input.TimeoutSeconds < 1 || input.TimeoutSeconds > 3600 {
		return DebugSession{}, errors.New("debug timeout_seconds must be between 1 and 3600")
	}
	if err := validateDebugBreakpoints(root, input.Breakpoints); err != nil {
		return DebugSession{}, err
	}
	executable, err := debugExecutable(input.Runtime)
	if err != nil {
		return DebugSession{}, fmt.Errorf("%w: %s", ErrDebuggerUnavailable, err)
	}
	now := time.Now().UTC()
	runCtx, cancel := context.WithTimeout(s.teamCtx, time.Duration(input.TimeoutSeconds)*time.Second)
	rt := &debugRuntime{item: DebugSession{ID: identity.New("debug"), ProjectID: project.ID, Runtime: input.Runtime, Program: input.Program, State: "starting", Stack: []DebugFrame{}, Breakpoints: []DebugBreakpoint{}, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Duration(input.TimeoutSeconds) * time.Second), Sandbox: commandSandbox{Kind: "process-hardening", WriteScope: "local-process-permissions"}}, root: root, cancel: cancel, done: make(chan struct{}), scripts: map[string]string{}, scopes: map[string][]string{}}
	s.mu.Lock()
	if s.teamCtx.Err() != nil {
		s.mu.Unlock()
		cancel()
		return DebugSession{}, errors.New("service is stopping")
	}
	if s.debuggers == nil {
		s.debuggers = map[string]*debugRuntime{}
	}
	active := 0
	for _, existing := range s.debuggers {
		existing.mu.Lock()
		if !debugTerminalState(existing.item.State) {
			active++
		}
		existing.mu.Unlock()
	}
	if active >= 4 {
		s.mu.Unlock()
		cancel()
		return DebugSession{}, fmt.Errorf("%w: stop an existing debug session before starting another", ErrDebuggerConflict)
	}
	if len(s.debuggers) >= 32 {
		var oldest *debugRuntime
		for _, existing := range s.debuggers {
			existing.mu.Lock()
			if debugTerminalState(existing.item.State) && (oldest == nil || existing.item.CreatedAt.Before(oldest.item.CreatedAt)) {
				oldest = existing
			}
			existing.mu.Unlock()
		}
		if oldest != nil {
			delete(s.debuggers, oldest.item.ID)
		}
	}
	s.debuggers[rt.item.ID] = rt
	s.debugWG.Add(1)
	s.mu.Unlock()
	tempRoot := filepath.Join(s.store.Root, "runtime", "debug", rt.item.ID)
	if err := os.MkdirAll(tempRoot, 0700); err != nil {
		cancel()
		rt.finish(err)
		s.debugWG.Done()
		return rt.snapshot(), err
	}
	args := append([]string{"--inspect-brk=127.0.0.1:0", "--", program}, input.Args...)
	if input.Runtime == "go" {
		args = []string{"dap", "--listen=127.0.0.1:0"}
	}
	command := exec.CommandContext(runCtx, executable, args...)
	command.Dir = root
	command.Env = minimalEnvironment(tempRoot)
	command.WaitDelay = 2 * time.Second
	configureProcessTermination(command)
	ready := make(chan string, 1)
	stdout := &debugOutput{rt: rt}
	stderr := &debugOutput{rt: rt, ready: ready}
	if input.Runtime == "go" {
		stdout.ready = ready
	}
	command.Stdout, command.Stderr = stdout, stderr
	go func() {
		defer s.debugWG.Done()
		defer os.RemoveAll(tempRoot)
		defer cancel()
		err, _ := runCommandProcess(command)
		stdout.flush()
		stderr.flush()
		if runCtx.Err() == context.DeadlineExceeded {
			err = errors.New("debug session time limit reached")
		}
		rt.finish(err)
	}()
	startupTimeout := 10 * time.Second
	if input.Runtime == "go" {
		startupTimeout = 120 * time.Second
	}
	startupCtx, startupCancel := context.WithTimeout(ctx, startupTimeout)
	defer startupCancel()
	var endpoint string
	select {
	case endpoint = <-ready:
	case <-rt.done:
		return rt.snapshot(), errors.New("debug runtime exited before the debugger connected")
	case <-startupCtx.Done():
		cancel()
		return rt.snapshot(), fmt.Errorf("debugger startup: %w", startupCtx.Err())
	}
	if input.Runtime == "go" {
		if err := rt.startGo(startupCtx, endpoint, program, tempRoot, input); err != nil {
			rt.mu.Lock()
			output := strings.TrimSpace(rt.item.Output)
			if len(output) > 4096 {
				output = output[len(output)-4096:]
			}
			if output != "" {
				err = fmt.Errorf("%w\n%s", err, output)
			}
			rt.failure = err.Error()
			rt.item.Error = err.Error()
			if rt.dap != nil {
				rt.dap.close()
			}
			rt.mu.Unlock()
			cancel()
			return rt.snapshot(), err
		}
		return rt.snapshot(), nil
	}
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 3 * time.Second}).DialContext(startupCtx, endpoint, nil)
	if err != nil {
		cancel()
		return rt.snapshot(), fmt.Errorf("connect Node debugger: %w", err)
	}
	client := newInspectorClient(conn, rt.event)
	rt.mu.Lock()
	rt.client = client
	rt.mu.Unlock()
	go rt.watchConnection(client.done)
	for _, method := range []string{"Runtime.enable", "Debugger.enable"} {
		if err = client.call(startupCtx, method, map[string]any{}, nil); err != nil {
			break
		}
	}
	if err == nil {
		err = rt.replaceBreakpoints(startupCtx, input.Breakpoints)
	}
	if err == nil {
		err = client.call(startupCtx, "Runtime.runIfWaitingForDebugger", map[string]any{}, nil)
	}
	if err != nil {
		client.close()
		cancel()
		return rt.snapshot(), err
	}
	return rt.snapshot(), nil
}

func debugTerminalState(state string) bool {
	return state == "exited" || state == "stopped" || state == "failed"
}
func (rt *debugRuntime) snapshot() DebugSession {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	item := rt.item
	item.Stack = append([]DebugFrame{}, item.Stack...)
	item.Breakpoints = append([]DebugBreakpoint{}, item.Breakpoints...)
	return item
}
func (rt *debugRuntime) finish(err error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.item.State = "exited"
	if rt.stopRequested {
		rt.item.State = "stopped"
	} else if rt.failure != "" {
		rt.item.State = "failed"
		rt.item.Error = rt.failure
	} else if err != nil {
		rt.item.State = "failed"
		rt.item.Error = err.Error()
	} else if rt.item.ExitCode != nil && *rt.item.ExitCode != 0 {
		rt.item.State = "failed"
		rt.item.Error = fmt.Sprintf("debug program exited with code %d", *rt.item.ExitCode)
	}
	if rt.item.ExitCode == nil {
		code := 0
		if err != nil {
			code = -1
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				code = exit.ExitCode()
			}
		}
		rt.item.ExitCode = &code
	}
	rt.item.Stack = []DebugFrame{}
	rt.scopes = map[string][]string{}
	rt.item.UpdatedAt = time.Now().UTC()
	if rt.client != nil {
		rt.client.close()
	}
	if rt.dap != nil {
		rt.dap.close()
	}
	close(rt.done)
}

// A lost adapter must not leave an unmanageable program running. Allow normal
// detach/exit to finish first, then terminate the launched process tree.
func (rt *debugRuntime) watchConnection(closed <-chan struct{}) {
	select {
	case <-rt.done:
		return
	case <-closed:
	}
	select {
	case <-rt.done:
		return
	case <-time.After(2 * time.Second):
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !debugTerminalState(rt.item.State) && !rt.stopRequested {
		rt.failure = "Debugger connection ended unexpectedly; the debug program was stopped."
		rt.cancel()
	}
}
func (s *Service) debugRuntime(ctx context.Context, id string) (*debugRuntime, error) {
	s.mu.Lock()
	rt := s.debuggers[id]
	s.mu.Unlock()
	if rt == nil {
		return nil, sql.ErrNoRows
	}
	if _, err := s.GetProject(ctx, rt.item.ProjectID); err != nil {
		return nil, err
	}
	return rt, nil
}
func (s *Service) GetDebugSession(ctx context.Context, id string) (DebugSession, error) {
	rt, err := s.debugRuntime(ctx, id)
	if err != nil {
		return DebugSession{}, err
	}
	return rt.snapshot(), nil
}
func (s *Service) ListDebugSessions(ctx context.Context, projectID string) ([]DebugSession, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	items := []DebugSession{}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rt := range s.debuggers {
		if rt.item.ProjectID == projectID {
			items = append(items, rt.snapshot())
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (s *Service) ControlDebugSession(ctx context.Context, id, action string) (DebugSession, error) {
	rt, err := s.debugRuntime(ctx, id)
	if err != nil {
		return DebugSession{}, err
	}
	rt.opMu.Lock()
	defer rt.opMu.Unlock()
	rt.mu.Lock()
	state, client, dap, threadID := rt.item.State, rt.client, rt.dap, rt.threadID
	version := rt.pauseVersion
	if action == "stop" {
		if !debugTerminalState(state) {
			rt.stopRequested = true
			rt.cancel()
			if client != nil {
				client.close()
			}
			if dap != nil {
				dap.close()
			}
		}
		rt.mu.Unlock()
		select {
		case <-rt.done:
		case <-ctx.Done():
			return rt.snapshot(), ctx.Err()
		case <-time.After(5 * time.Second):
			return rt.snapshot(), errors.New("debug process has not yet stopped")
		}
		return rt.snapshot(), nil
	}
	rt.mu.Unlock()
	methods := map[string]string{"continue": "Debugger.resume", "pause": "Debugger.pause", "step_over": "Debugger.stepOver", "step_in": "Debugger.stepInto", "step_out": "Debugger.stepOut"}
	method := methods[action]
	if method == "" {
		return rt.snapshot(), errors.New("choose continue, pause, step_over, step_in, step_out or stop")
	}
	if (client == nil && dap == nil) || debugTerminalState(state) || (action == "pause" && state != "running") || (action != "pause" && state != "paused") {
		return rt.snapshot(), fmt.Errorf("%w: cannot %s a %s session", ErrDebuggerConflict, action, state)
	}
	if dap != nil {
		commands := map[string]string{"continue": "continue", "pause": "pause", "step_over": "next", "step_in": "stepIn", "step_out": "stepOut"}
		if err := dap.call(ctx, commands[action], map[string]any{"threadId": threadID}, nil); err != nil {
			return rt.snapshot(), err
		}
		// DAP does not require a continued event for the client that issued
		// continue. Preserve a newer stopped event if execution paused already.
		if action != "pause" {
			rt.mu.Lock()
			if rt.pauseVersion == version && rt.item.State == "paused" {
				rt.item.State = "running"
				rt.item.Reason = ""
				rt.item.Stack = []DebugFrame{}
				rt.scopes = map[string][]string{}
				rt.pauseVersion++
				rt.item.UpdatedAt = time.Now().UTC()
			}
			rt.mu.Unlock()
		}
		return rt.snapshot(), nil
	}
	if err := client.call(ctx, method, map[string]any{}, nil); err != nil {
		return rt.snapshot(), err
	}
	return rt.snapshot(), nil
}

func validateDebugBreakpoints(root string, points []DebugBreakpoint) error {
	if len(points) > 100 {
		return errors.New("at most 100 breakpoints are allowed")
	}
	for _, point := range points {
		if point.Line < 1 || point.Line > 1000000 {
			return errors.New("breakpoint lines must be positive")
		}
		path, err := resolveInside(root, point.Path, true)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("breakpoint path must be a saved project file")
		}
	}
	return nil
}
func (s *Service) SetDebugBreakpoints(ctx context.Context, id string, points []DebugBreakpoint) (DebugSession, error) {
	rt, err := s.debugRuntime(ctx, id)
	if err != nil {
		return DebugSession{}, err
	}
	rt.opMu.Lock()
	defer rt.opMu.Unlock()
	if err := validateDebugBreakpoints(rt.root, points); err != nil {
		return rt.snapshot(), err
	}
	if err := rt.replaceBreakpoints(ctx, points); err != nil {
		return rt.snapshot(), err
	}
	return rt.snapshot(), nil
}
func (rt *debugRuntime) replaceBreakpoints(ctx context.Context, points []DebugBreakpoint) error {
	rt.mu.Lock()
	client := rt.client
	old := append([]DebugBreakpoint{}, rt.item.Breakpoints...)
	state := rt.item.State
	dap := rt.dap
	rt.mu.Unlock()
	if (client == nil && dap == nil) || debugTerminalState(state) {
		return fmt.Errorf("%w: debugger is not connected", ErrDebuggerConflict)
	}
	if dap != nil {
		return rt.replaceGoBreakpoints(ctx, points)
	}
	for _, point := range old {
		if err := client.call(ctx, "Debugger.removeBreakpoint", map[string]any{"breakpointId": point.ID}, nil); err != nil {
			return err
		}
	}
	rt.mu.Lock()
	rt.item.Breakpoints = []DebugBreakpoint{}
	rt.mu.Unlock()
	for _, point := range points {
		path, err := resolveInside(rt.root, point.Path, true)
		if err != nil {
			return err
		}
		var result struct {
			BreakpointID string            `json:"breakpointId"`
			Locations    []json.RawMessage `json:"locations"`
		}
		if err := client.call(ctx, "Debugger.setBreakpointByUrl", map[string]any{"lineNumber": point.Line - 1, "url": debugFileURL(path)}, &result); err != nil {
			return err
		}
		point.ID = result.BreakpointID
		point.Verified = len(result.Locations) > 0
		rt.mu.Lock()
		rt.item.Breakpoints = append(rt.item.Breakpoints, point)
		rt.item.UpdatedAt = time.Now().UTC()
		rt.mu.Unlock()
	}
	return nil
}

func debugFileURL(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
func debugRelativePath(root, raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "file" {
		return ""
	}
	path := parsed.Path
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	rel, err := filepath.Rel(root, filepath.FromSlash(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

func (rt *debugRuntime) event(method string, params json.RawMessage) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if debugTerminalState(rt.item.State) {
		return
	}
	switch method {
	case "Debugger.scriptParsed":
		var event struct {
			ScriptID string `json:"scriptId"`
			URL      string `json:"url"`
		}
		if json.Unmarshal(params, &event) == nil && len(rt.scripts) < 10000 {
			rt.scripts[event.ScriptID] = event.URL
		}
	case "Debugger.resumed":
		rt.item.State = "running"
		rt.item.Reason = ""
		rt.item.Stack = []DebugFrame{}
		rt.scopes = map[string][]string{}
	case "Debugger.paused":
		var event struct {
			Reason     string `json:"reason"`
			CallFrames []struct {
				CallFrameID  string `json:"callFrameId"`
				FunctionName string `json:"functionName"`
				URL          string `json:"url"`
				Location     struct {
					ScriptID     string `json:"scriptId"`
					LineNumber   int    `json:"lineNumber"`
					ColumnNumber int    `json:"columnNumber"`
				} `json:"location"`
				ScopeChain []struct {
					Type   string `json:"type"`
					Object struct {
						ObjectID string `json:"objectId"`
					} `json:"object"`
				} `json:"scopeChain"`
			} `json:"callFrames"`
		}
		if json.Unmarshal(params, &event) != nil {
			return
		}
		rt.item.State = "paused"
		rt.item.Reason = event.Reason
		rt.item.Stack = []DebugFrame{}
		rt.scopes = map[string][]string{}
		for i, frame := range event.CallFrames {
			if i >= 32 {
				break
			}
			raw := frame.URL
			if raw == "" {
				raw = rt.scripts[frame.Location.ScriptID]
			}
			path := debugRelativePath(rt.root, raw)
			if path == "" {
				continue
			}
			rt.item.Stack = append(rt.item.Stack, DebugFrame{ID: frame.CallFrameID, Name: frame.FunctionName, Path: path, Line: frame.Location.LineNumber + 1, Column: frame.Location.ColumnNumber + 1})
			rt.scopes[frame.CallFrameID] = []string{}
			for _, scope := range frame.ScopeChain {
				if scope.Type == "local" || scope.Type == "block" || scope.Type == "catch" {
					rt.scopes[frame.CallFrameID] = append(rt.scopes[frame.CallFrameID], scope.Object.ObjectID)
				}
			}
		}
	case "Debugger.breakpointResolved":
		var event struct {
			BreakpointID string `json:"breakpointId"`
		}
		if json.Unmarshal(params, &event) == nil {
			for i := range rt.item.Breakpoints {
				if rt.item.Breakpoints[i].ID == event.BreakpointID {
					rt.item.Breakpoints[i].Verified = true
				}
			}
		}
	}
	rt.item.UpdatedAt = time.Now().UTC()
}

func (s *Service) DebugVariables(ctx context.Context, id, frameID string) ([]DebugVariable, error) {
	rt, err := s.debugRuntime(ctx, id)
	if err != nil {
		return nil, err
	}
	rt.opMu.Lock()
	defer rt.opMu.Unlock()
	rt.mu.Lock()
	objects, found := rt.scopes[frameID]
	objects = append([]string{}, objects...)
	client := rt.client
	dap := rt.dap
	state := rt.item.State
	rt.mu.Unlock()
	if state != "paused" || (client == nil && dap == nil) {
		return nil, fmt.Errorf("%w: pause the program to inspect variables", ErrDebuggerConflict)
	}
	if !found {
		return nil, errors.New("choose a current project stack frame")
	}
	if dap != nil {
		return rt.goVariables(ctx, frameID)
	}
	items := []DebugVariable{}
	seen := map[string]bool{}
	for _, objectID := range objects {
		var result struct {
			Result []struct {
				Name  string `json:"name"`
				Value *struct {
					Type                string          `json:"type"`
					Value               json.RawMessage `json:"value"`
					Description         string          `json:"description"`
					UnserializableValue string          `json:"unserializableValue"`
				} `json:"value"`
			} `json:"result"`
		}
		if err := client.call(ctx, "Runtime.getProperties", map[string]any{"objectId": objectID, "ownProperties": true, "generatePreview": false}, &result); err != nil {
			return nil, err
		}
		for _, property := range result.Result {
			if property.Value == nil || seen[property.Name] {
				continue
			}
			seen[property.Name] = true
			value := property.Value.Description
			if len(property.Value.Value) > 0 {
				value = string(property.Value.Value)
			} else if property.Value.UnserializableValue != "" {
				value = property.Value.UnserializableValue
			}
			if len(value) > 2048 {
				value = value[:2048] + "…"
			}
			items = append(items, DebugVariable{Name: property.Name, Type: property.Value.Type, Value: value})
			if len(items) >= 100 {
				return items, nil
			}
		}
	}
	return items, nil
}

type debugOutput struct {
	mu      sync.Mutex
	rt      *debugRuntime
	ready   chan string
	pending string
}

func (out *debugOutput) Write(data []byte) (int, error) {
	out.mu.Lock()
	defer out.mu.Unlock()
	out.pending += string(data)
	for {
		index := strings.IndexByte(out.pending, '\n')
		if index < 0 {
			if len(out.pending) > 8192 {
				out.append(out.pending[:8192])
				out.pending = out.pending[8192:]
				continue
			}
			break
		}
		line := strings.TrimSuffix(out.pending[:index], "\r")
		out.pending = out.pending[index+1:]
		if out.ready != nil && strings.HasPrefix(line, "DAP server listening at: ") {
			endpoint := strings.TrimPrefix(line, "DAP server listening at: ")
			host, port, err := net.SplitHostPort(endpoint)
			if err == nil && host == "127.0.0.1" && port != "" {
				select {
				case out.ready <- endpoint:
				default:
				}
			}
			continue
		}
		if out.ready != nil && strings.HasPrefix(line, "Debugger listening on ") {
			raw := strings.TrimPrefix(line, "Debugger listening on ")
			parsed, err := url.Parse(raw)
			if err == nil && parsed.Scheme == "ws" && parsed.Hostname() == "127.0.0.1" && parsed.Port() != "" && parsed.User == nil {
				select {
				case out.ready <- raw:
				default:
				}
			}
			continue
		}
		if out.ready != nil && strings.HasPrefix(line, "Waiting for the debugger to disconnect") {
			out.rt.mu.Lock()
			client := out.rt.client
			out.rt.mu.Unlock()
			if client != nil {
				client.close()
			}
			continue
		}
		if out.ready != nil && (line == "Debugger attached." || strings.HasPrefix(line, "For help, see:")) {
			continue
		}
		out.append(line + "\n")
	}
	return len(data), nil
}
func (out *debugOutput) append(value string) {
	out.rt.mu.Lock()
	defer out.rt.mu.Unlock()
	out.rt.appendOutputLocked(value)
}
func (out *debugOutput) flush() {
	out.mu.Lock()
	defer out.mu.Unlock()
	if out.pending != "" {
		out.append(out.pending)
		out.pending = ""
	}
}
func (rt *debugRuntime) appendOutputLocked(value string) {
	rt.item.Output += value
	if len(rt.item.Output) > 128<<10 {
		rt.item.Output = rt.item.Output[len(rt.item.Output)-(128<<10):]
		rt.item.OutputTruncated = true
	}
	rt.item.UpdatedAt = time.Now().UTC()
}
func (s *Service) closeDebuggers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rt := range s.debuggers {
		rt.mu.Lock()
		if !debugTerminalState(rt.item.State) {
			rt.stopRequested = true
			rt.cancel()
			if rt.client != nil {
				rt.client.close()
			}
			if rt.dap != nil {
				rt.dap.close()
			}
		}
		rt.mu.Unlock()
	}
}

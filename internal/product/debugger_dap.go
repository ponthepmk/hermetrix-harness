package product

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type dapMessage struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	RequestSeq int             `json:"request_seq"`
	Success    bool            `json:"success"`
	Message    string          `json:"message"`
	Event      string          `json:"event"`
	Body       json.RawMessage `json:"body"`
}
type dapClient struct {
	conn    net.Conn
	mu      sync.Mutex
	writeMu sync.Mutex
	next    int
	pending map[int]chan dapMessage
	done    chan struct{}
	onEvent func(string, json.RawMessage)
}

func newDAPClient(conn net.Conn, onEvent func(string, json.RawMessage)) *dapClient {
	c := &dapClient{conn: conn, pending: map[int]chan dapMessage{}, done: make(chan struct{}), onEvent: onEvent}
	go c.read()
	return c
}
func (c *dapClient) read() {
	defer close(c.done)
	defer c.conn.Close()
	reader := bufio.NewReader(c.conn)
	for {
		length := 0
		headerBytes := 0
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			headerBytes += len(line)
			if headerBytes > 8192 {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "Content-Length:") {
				length, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:")))
			}
		}
		if length < 1 || length > 4<<20 {
			return
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(reader, data); err != nil {
			return
		}
		var message dapMessage
		if json.Unmarshal(data, &message) != nil {
			return
		}
		if message.Type == "response" {
			c.mu.Lock()
			ch := c.pending[message.RequestSeq]
			c.mu.Unlock()
			if ch != nil {
				ch <- message
			}
		} else if message.Type == "event" && c.onEvent != nil {
			c.onEvent(message.Event, message.Body)
		}
	}
}
func (c *dapClient) call(ctx context.Context, command string, args any, result any) error {
	limit := 10 * time.Second
	if command == "launch" {
		limit = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan dapMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	data, err := json.Marshal(map[string]any{"seq": id, "type": "request", "command": command, "arguments": args})
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = fmt.Fprintf(c.conn, "Content-Length: %d\r\n\r\n%s", len(data), data)
	c.writeMu.Unlock()
	if err != nil {
		return err
	}
	select {
	case message := <-ch:
		if !message.Success {
			var detail struct {
				Error struct {
					Format    string            `json:"format"`
					Variables map[string]string `json:"variables"`
				} `json:"error"`
			}
			text := message.Message
			if json.Unmarshal(message.Body, &detail) == nil && detail.Error.Format != "" {
				text = detail.Error.Format
				for key, value := range detail.Error.Variables {
					text = strings.ReplaceAll(text, "{"+key+"}", value)
				}
			}
			if len(text) > 4096 {
				text = text[:4096] + "…"
			}
			return fmt.Errorf("Go debugger %s: %s", command, text)
		}
		if result != nil {
			return json.Unmarshal(message.Body, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errors.New("Go debugger connection ended")
	}
}
func (c *dapClient) close() { _ = c.conn.Close() }

func findDelve() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("HERMETRIX_DLV_PATH")); configured != "" {
		if !filepath.IsAbs(configured) {
			return "", errors.New("HERMETRIX_DLV_PATH must be absolute")
		}
		if info, err := os.Stat(configured); err == nil && info.Mode().IsRegular() {
			return configured, nil
		}
		return "", errors.New("configured Delve executable does not exist")
	}
	if found, err := exec.LookPath("dlv"); err == nil {
		return found, nil
	}
	names := []string{"dlv.exe", "dlv"}
	roots := []string{}
	if executable, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(executable))
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	for _, root := range roots {
		for _, name := range names {
			candidate := filepath.Join(root, ".hermetrix-tools", name)
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				return candidate, nil
			}
		}
	}
	return "", errors.New("Delve is not installed")
}
func debugExecutable(runtime string) (string, error) {
	if runtime == "go" {
		if _, err := exec.LookPath("go"); err != nil {
			return "", errors.New("Go is not installed")
		}
		return findDelve()
	}
	return exec.LookPath("node")
}
func (rt *debugRuntime) startGo(ctx context.Context, endpoint, program, tempRoot string, input DebugStartInput) error {
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return err
	}
	client := newDAPClient(conn, rt.goEvent)
	rt.mu.Lock()
	rt.dap = client
	rt.mu.Unlock()
	go rt.watchConnection(client.done)
	if err := client.call(ctx, "initialize", map[string]any{"clientID": "hermetrix", "adapterID": "go", "pathFormat": "path", "linesStartAt1": true, "columnsStartAt1": true, "supportsVariableType": true, "supportsRunInTerminalRequest": false}, nil); err != nil {
		return err
	}
	output := filepath.Join(tempRoot, "debug-program")
	if strings.HasSuffix(strings.ToLower(os.Args[0]), ".exe") {
		output += ".exe"
	}
	if err := client.call(ctx, "launch", map[string]any{"request": "launch", "mode": "debug", "program": program, "cwd": rt.root, "dlvCwd": rt.root, "args": input.Args, "stopOnEntry": true, "output": output, "showGlobalVariables": false, "stackTraceDepth": 32}, nil); err != nil {
		return err
	}
	if err := rt.replaceGoBreakpoints(ctx, input.Breakpoints); err != nil {
		return err
	}
	return client.call(ctx, "configurationDone", map[string]any{}, nil)
}
func (rt *debugRuntime) goEvent(event string, body json.RawMessage) {
	rt.mu.Lock()
	if debugTerminalState(rt.item.State) {
		rt.mu.Unlock()
		return
	}
	switch event {
	case "stopped":
		var data struct {
			Reason   string `json:"reason"`
			ThreadID int    `json:"threadId"`
		}
		if json.Unmarshal(body, &data) != nil {
			rt.mu.Unlock()
			return
		}
		rt.threadID = data.ThreadID
		rt.pauseVersion++
		version := rt.pauseVersion
		rt.item.State = "paused"
		rt.item.Reason = data.Reason
		rt.item.Stack = []DebugFrame{}
		rt.scopes = map[string][]string{}
		go rt.refreshGoStack(data.ThreadID, version)
	case "continued":
		rt.item.State = "running"
		rt.item.Reason = ""
		rt.item.Stack = []DebugFrame{}
		rt.scopes = map[string][]string{}
		rt.pauseVersion++
	case "output":
		var data struct {
			Output string `json:"output"`
		}
		if json.Unmarshal(body, &data) == nil {
			rt.appendOutputLocked(data.Output)
		}
	case "exited":
		var data struct {
			ExitCode int `json:"exitCode"`
		}
		if json.Unmarshal(body, &data) == nil {
			rt.item.ExitCode = &data.ExitCode
		}
	case "terminated":
		client := rt.dap
		go func() {
			if client != nil {
				_ = client.call(context.Background(), "disconnect", map[string]any{"terminateDebuggee": true}, nil)
				client.close()
			}
		}()
	}
	rt.item.UpdatedAt = time.Now().UTC()
	rt.mu.Unlock()
}
func (rt *debugRuntime) refreshGoStack(threadID, version int) {
	rt.mu.Lock()
	client := rt.dap
	rt.mu.Unlock()
	if client == nil {
		return
	}
	var result struct {
		StackFrames []struct {
			ID     int    `json:"id"`
			Name   string `json:"name"`
			Line   int    `json:"line"`
			Column int    `json:"column"`
			Source struct {
				Path string `json:"path"`
			} `json:"source"`
		} `json:"stackFrames"`
	}
	if err := client.call(context.Background(), "stackTrace", map[string]any{"threadId": threadID, "startFrame": 0, "levels": 32}, &result); err != nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.item.State != "paused" || version != rt.pauseVersion {
		return
	}
	rt.item.Stack = []DebugFrame{}
	for _, frame := range result.StackFrames {
		path := debugRelativePath(rt.root, debugFileURL(frame.Source.Path))
		if path == "" {
			continue
		}
		id := strconv.Itoa(frame.ID)
		rt.item.Stack = append(rt.item.Stack, DebugFrame{ID: id, Name: frame.Name, Path: path, Line: frame.Line, Column: frame.Column})
		rt.scopes[id] = []string{}
	}
	rt.item.UpdatedAt = time.Now().UTC()
}
func (rt *debugRuntime) replaceGoBreakpoints(ctx context.Context, points []DebugBreakpoint) error {
	rt.mu.Lock()
	client := rt.dap
	old := append([]DebugBreakpoint{}, rt.item.Breakpoints...)
	rt.mu.Unlock()
	byPath := map[string][]DebugBreakpoint{}
	for _, point := range old {
		byPath[point.Path] = nil
	}
	for _, point := range points {
		byPath[point.Path] = append(byPath[point.Path], point)
	}
	all := []DebugBreakpoint{}
	for path, items := range byPath {
		resolved, err := resolveInside(rt.root, path, true)
		if err != nil {
			return err
		}
		locations := []map[string]int{}
		for _, item := range items {
			locations = append(locations, map[string]int{"line": item.Line})
		}
		var result struct {
			Breakpoints []struct {
				ID       int  `json:"id"`
				Verified bool `json:"verified"`
				Line     int  `json:"line"`
			} `json:"breakpoints"`
		}
		if err := client.call(ctx, "setBreakpoints", map[string]any{"source": map[string]any{"path": resolved}, "breakpoints": locations}, &result); err != nil {
			return err
		}
		for index, item := range items {
			if index < len(result.Breakpoints) {
				item.ID = strconv.Itoa(result.Breakpoints[index].ID)
				item.Verified = result.Breakpoints[index].Verified
				if result.Breakpoints[index].Line > 0 {
					item.Line = result.Breakpoints[index].Line
				}
			}
			all = append(all, item)
		}
	}
	rt.mu.Lock()
	rt.item.Breakpoints = all
	rt.item.UpdatedAt = time.Now().UTC()
	rt.mu.Unlock()
	return nil
}
func (rt *debugRuntime) goVariables(ctx context.Context, frameID string) ([]DebugVariable, error) {
	id, err := strconv.Atoi(frameID)
	if err != nil {
		return nil, errors.New("invalid Go frame")
	}
	var scopes struct {
		Scopes []struct {
			Name               string `json:"name"`
			VariablesReference int    `json:"variablesReference"`
			Expensive          bool   `json:"expensive"`
		} `json:"scopes"`
	}
	if err := rt.dap.call(ctx, "scopes", map[string]any{"frameId": id}, &scopes); err != nil {
		return nil, err
	}
	items := []DebugVariable{}
	for _, scope := range scopes.Scopes {
		if scope.Expensive || strings.Contains(strings.ToLower(scope.Name), "global") {
			continue
		}
		var variables struct {
			Variables []DebugVariable `json:"variables"`
		}
		if err := rt.dap.call(ctx, "variables", map[string]any{"variablesReference": scope.VariablesReference, "start": 0, "count": 100 - len(items)}, &variables); err != nil {
			return nil, err
		}
		for _, item := range variables.Variables {
			if len(item.Value) > 2048 {
				item.Value = item.Value[:2048] + "…"
			}
			items = append(items, item)
			if len(items) >= 100 {
				return items, nil
			}
		}
	}
	return items, nil
}

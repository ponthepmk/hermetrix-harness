package product

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/identity"
)

func nodeDebugFixture(t *testing.T, source string) (*Service, Project) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js not installed")
	}
	t.Setenv("HERMETRIX_REQUIRE_OS_SANDBOX", "")
	service, _, _ := testProductService(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fixture.cjs"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := service.SaveProject(context.Background(), ProjectInput{Name: "Debugger fixture", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	return service, project
}
func awaitDebug(t *testing.T, s *Service, id string, match func(DebugSession) bool) DebugSession {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var item DebugSession
	for time.Now().Before(deadline) {
		var err error
		item, err = s.GetDebugSession(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if match(item) {
			return item
		}
		if item.State == "failed" {
			t.Fatalf("debug failed: %+v", item)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("debug state did not arrive: %+v", item)
	return item
}
func TestNodeDebuggerBreakpointVariablesAndStepping(t *testing.T) {
	s, project := nodeDebugFixture(t, "function add(a, b) {\n  const total = a + b;\n  const doubled = total * 2;\n  return doubled;\n}\nconst answer = add(2, 3);\nconsole.log(answer);\n")
	ctx := context.Background()
	item, err := s.StartDebugSession(ctx, DebugStartInput{ProjectID: project.ID, Runtime: "node", Program: "fixture.cjs", Breakpoints: []DebugBreakpoint{{Path: "fixture.cjs", Line: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	awaitDebug(t, s, item.ID, func(item DebugSession) bool { return item.State == "paused" })
	if _, err := s.ControlDebugSession(ctx, item.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	item = awaitDebug(t, s, item.ID, func(item DebugSession) bool {
		return item.State == "paused" && len(item.Stack) > 0 && item.Stack[0].Line == 3
	})
	if len(item.Breakpoints) != 1 || !item.Breakpoints[0].Verified {
		t.Fatalf("breakpoint never resolved: %+v", item.Breakpoints)
	}
	variables, err := s.DebugVariables(ctx, item.ID, item.Stack[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	assertVariable(t, variables, "total", "5")
	if _, err := s.ControlDebugSession(ctx, item.ID, "step_over"); err != nil {
		t.Fatal(err)
	}
	item = awaitDebug(t, s, item.ID, func(item DebugSession) bool {
		return item.State == "paused" && len(item.Stack) > 0 && item.Stack[0].Line == 4
	})
	variables, err = s.DebugVariables(ctx, item.ID, item.Stack[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	assertVariable(t, variables, "doubled", "10")
	if _, err := s.ControlDebugSession(ctx, item.ID, "step_out"); err != nil {
		t.Fatal(err)
	}
	awaitDebug(t, s, item.ID, func(item DebugSession) bool {
		return item.State == "paused" && len(item.Stack) > 0 && item.Stack[0].Name != "add"
	})
	if _, err := s.ControlDebugSession(ctx, item.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	item = awaitDebug(t, s, item.ID, func(item DebugSession) bool { return item.State == "exited" })
	if strings.TrimSpace(item.Output) != "10" || strings.Contains(item.Output, "ws://") {
		t.Fatalf("unexpected output: %q", item.Output)
	}
}
func assertVariable(t *testing.T, items []DebugVariable, name, value string) {
	t.Helper()
	for _, item := range items {
		if item.Name == name && item.Value == value {
			return
		}
	}
	t.Fatalf("variable %s=%s absent: %+v", name, value, items)
}
func TestNodeDebuggerStepInPauseAndStop(t *testing.T) {
	s, project := nodeDebugFixture(t, "function tick() {\n  const count = 1;\n  return count;\n}\ntick();\nsetInterval(tick, 100);\n")
	ctx := context.Background()
	item, err := s.StartDebugSession(ctx, DebugStartInput{ProjectID: project.ID, Runtime: "node", Program: "fixture.cjs"})
	if err != nil {
		t.Fatal(err)
	}
	awaitDebug(t, s, item.ID, func(item DebugSession) bool { return item.State == "paused" })
	if _, err := s.ControlDebugSession(ctx, item.ID, "step_in"); err != nil {
		t.Fatal(err)
	}
	awaitDebug(t, s, item.ID, func(item DebugSession) bool {
		return item.State == "paused" && len(item.Stack) > 0 && item.Stack[0].Name == "tick"
	})
	if _, err := s.ControlDebugSession(ctx, item.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	awaitDebug(t, s, item.ID, func(item DebugSession) bool { return item.State == "running" })
	if _, err := s.ControlDebugSession(ctx, item.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	awaitDebug(t, s, item.ID, func(item DebugSession) bool { return item.State == "paused" })
	item, err = s.ControlDebugSession(ctx, item.ID, "stop")
	if err != nil || item.State != "stopped" {
		t.Fatalf("stop=%+v err=%v", item, err)
	}
	if _, err = s.ControlDebugSession(ctx, item.ID, "continue"); !errors.Is(err, ErrDebuggerConflict) {
		t.Fatalf("exited session continued: %v", err)
	}
}
func TestDebuggerRejectsForeignOwnerAndEscapingPaths(t *testing.T) {
	s, project := nodeDebugFixture(t, "setInterval(() => {}, 100);\n")
	ctx := context.Background()
	for _, program := range []string{"../escape.cjs", filepath.Join(project.RootPath, "fixture.cjs")} {
		if _, err := s.StartDebugSession(ctx, DebugStartInput{ProjectID: project.ID, Runtime: "node", Program: program}); err == nil {
			t.Fatalf("accepted escaping program %q", program)
		}
	}
	item, err := s.StartDebugSession(ctx, DebugStartInput{ProjectID: project.ID, Runtime: "node", Program: "fixture.cjs"})
	if err != nil {
		t.Fatal(err)
	}
	foreign := identity.WithPrincipal(ctx, "other-principal")
	if _, err := s.GetDebugSession(foreign, item.ID); err == nil {
		t.Fatal("foreign principal read debugger")
	}
	if _, err := s.ControlDebugSession(foreign, item.ID, "continue"); err == nil {
		t.Fatal("foreign principal controlled debugger")
	}
	if _, err := s.SetDebugBreakpoints(ctx, item.ID, []DebugBreakpoint{{Path: "../outside.cjs", Line: 1}}); err == nil {
		t.Fatal("accepted escaping breakpoint")
	}
	if _, err := s.DebugVariables(ctx, item.ID, "arbitrary-object-id"); err == nil {
		t.Fatal("accepted arbitrary object ID")
	}
	if _, err := s.ControlDebugSession(ctx, item.ID, "evaluate"); err == nil {
		t.Fatal("accepted arbitrary evaluation")
	}
	t.Setenv("HERMETRIX_REQUIRE_OS_SANDBOX", "1")
	if _, err := s.StartDebugSession(ctx, DebugStartInput{ProjectID: project.ID, Runtime: "node", Program: "fixture.cjs"}); !errors.Is(err, ErrDebuggerUnavailable) {
		t.Fatalf("strict sandbox bypassed: %v", err)
	}
}

func TestNodeDebuggerTimeoutAndBoundedPartialOutput(t *testing.T) {
	s, project := nodeDebugFixture(t, "process.stdout.write('x'.repeat(150000));\nsetInterval(() => {}, 100);\n")
	ctx := context.Background()
	item, err := s.StartDebugSession(ctx, DebugStartInput{ProjectID: project.ID, Runtime: "node", Program: "fixture.cjs", TimeoutSeconds: 2})
	if err != nil {
		t.Fatal(err)
	}
	awaitDebug(t, s, item.ID, func(item DebugSession) bool { return item.State == "paused" })
	if _, err := s.ControlDebugSession(ctx, item.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	// A deadline is an expected failure here, so inspect it without awaitDebug.
	rt, err := s.debugRuntime(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-rt.done:
	case <-time.After(8 * time.Second):
		t.Fatal("timed-out debug process survived")
	}
	item = rt.snapshot()
	if item.State != "failed" || !strings.Contains(item.Error, "time limit") || !item.OutputTruncated || len(item.Output) != 128<<10 {
		t.Fatalf("timeout/output result: state=%s error=%s bytes=%d truncated=%v", item.State, item.Error, len(item.Output), item.OutputTruncated)
	}
}

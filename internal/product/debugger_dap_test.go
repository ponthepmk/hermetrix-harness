package product

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func goDebugFixture(t *testing.T, source string) (*Service, Project) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("Go not installed")
	}
	if _, err := findDelve(); err != nil {
		candidate, _ := filepath.Abs(filepath.Join("..", "..", ".hermetrix-tools", "dlv.exe"))
		if _, err := os.Stat(candidate); err != nil {
			t.Skip("Delve not installed")
		}
		t.Setenv("HERMETRIX_DLV_PATH", candidate)
	}
	t.Setenv("HERMETRIX_REQUIRE_OS_SANDBOX", "")
	service, _, _ := testProductService(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := service.SaveProject(context.Background(), ProjectInput{Name: "Go debugger fixture", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	return service, project
}
func TestGoDebuggerBreakpointVariablesAndStepping(t *testing.T) {
	s, project := goDebugFixture(t, "package main\nimport \"fmt\"\nfunc add(a, b int) int {\n total := a + b\n doubled := total * 2\n return doubled\n}\nfunc main() {\n answer := add(2, 3)\n fmt.Println(answer)\n}\n")
	ctx := context.Background()
	item, err := s.StartDebugSession(ctx, DebugStartInput{ProjectID: project.ID, Runtime: "go", Program: "main.go", Breakpoints: []DebugBreakpoint{{Path: "main.go", Line: 5}}})
	if err != nil {
		t.Fatalf("start: %v; session %+v", err, item)
	}
	awaitDebug(t, s, item.ID, func(item DebugSession) bool { return item.State == "paused" })
	if _, err := s.ControlDebugSession(ctx, item.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	item = awaitDebug(t, s, item.ID, func(item DebugSession) bool {
		return item.State == "paused" && len(item.Stack) > 0 && item.Stack[0].Line == 5
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
		return item.State == "paused" && len(item.Stack) > 0 && item.Stack[0].Line == 6
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
		return item.State == "paused" && len(item.Stack) > 0 && item.Stack[0].Name == "main.main"
	})
	if _, err := s.ControlDebugSession(ctx, item.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	item = awaitDebug(t, s, item.ID, func(item DebugSession) bool { return item.State == "exited" })
	if !strings.Contains(item.Output, "10") || strings.Contains(item.Output, "listening at") {
		t.Fatalf("unexpected output: %q", item.Output)
	}
}
func TestGoDebuggerStepInAndStop(t *testing.T) {
	s, project := goDebugFixture(t, "package main\nimport \"time\"\nfunc tick() {\n value := 1\n _ = value\n}\nfunc main() {\n tick()\n for { time.Sleep(time.Second) }\n}\n")
	ctx := context.Background()
	item, err := s.StartDebugSession(ctx, DebugStartInput{ProjectID: project.ID, Runtime: "go", Program: "main.go", Breakpoints: []DebugBreakpoint{{Path: "main.go", Line: 8}}})
	if err != nil {
		t.Fatalf("start: %v; session %+v", err, item)
	}
	awaitDebug(t, s, item.ID, func(item DebugSession) bool { return item.State == "paused" })
	if _, err := s.ControlDebugSession(ctx, item.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	awaitDebug(t, s, item.ID, func(item DebugSession) bool {
		return item.State == "paused" && len(item.Stack) > 0 && item.Stack[0].Line == 8
	})
	if _, err := s.ControlDebugSession(ctx, item.ID, "step_in"); err != nil {
		t.Fatal(err)
	}
	awaitDebug(t, s, item.ID, func(item DebugSession) bool {
		return item.State == "paused" && len(item.Stack) > 0 && item.Stack[0].Name == "main.tick"
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
}

func TestGoDebuggerReportsCompileFailureAndExitCode(t *testing.T) {
	s, project := goDebugFixture(t, "package main\nfunc main() { missingSymbol() }\n")
	ctx := context.Background()
	item, err := s.StartDebugSession(ctx, DebugStartInput{ProjectID: project.ID, Runtime: "go", Program: "main.go"})
	if err == nil || !strings.Contains(err.Error(), "missingSymbol") {
		t.Fatalf("compile failure lacks actionable detail: %v %+v", err, item)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "main.go"), []byte("package main\nimport \"os\"\nfunc main(){ os.Exit(7) }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	item, err = s.StartDebugSession(ctx, DebugStartInput{ProjectID: project.ID, Runtime: "go", Program: "main.go"})
	if err != nil {
		t.Fatal(err)
	}
	awaitDebug(t, s, item.ID, func(item DebugSession) bool { return item.State == "paused" })
	if _, err := s.ControlDebugSession(ctx, item.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	rt, err := s.debugRuntime(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-rt.done:
	case <-time.After(8 * time.Second):
		t.Fatal("exited program still active")
	}
	item = rt.snapshot()
	if item.State != "failed" || item.ExitCode == nil || *item.ExitCode != 7 {
		t.Fatalf("nonzero program exit falsely succeeded: %+v", item)
	}
}

package agentplatform

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestH1CapabilityBoundaryAndForbiddenCallGuard(t *testing.T) {
	forbiddenImports := map[string]bool{
		"os/exec": true, "hermetrix-harness/internal/providers": true,
		"hermetrix-harness/internal/mcp": true, "hermetrix-harness/internal/agent": true,
	}
	forbiddenCalls := map[string]bool{"PlanEffect": true, "DispatchEffect": true, "BeginRun": true,
		"BeginStepAttempt": true, "StartCommand": true, "StreamChat": true, "ExecuteCapability": true,
		"RunTurn": true, "AutoPlan": true, "WriteProjectFile": true, "WriteProjectFiles": true,
		"RecoverInterruptedJobs": true}
	fset := token.NewFileSet()
	packages, err := parser.ParseDir(fset, ".", func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range packages {
		for name, file := range pkg.Files {
			for _, item := range file.Imports {
				path := strings.Trim(item.Path.Value, `"`)
				if forbiddenImports[path] {
					t.Fatalf("forbidden import %s in %s", path, name)
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				if selector, ok := node.(*ast.SelectorExpr); ok && forbiddenCalls[selector.Sel.Name] {
					t.Fatalf("forbidden call reference %s in %s", selector.Sel.Name, name)
				}
				return true
			})
		}
	}
	typeOfService := reflect.TypeOf(Service{})
	for i := 0; i < typeOfService.NumField(); i++ {
		field := typeOfService.Field(i)
		name := strings.ToLower(field.Name + " " + field.Type.String())
		for _, forbidden := range []string{"executor", "provider", "command", "workspace", "mcp", "agent.service", "product.service"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("Service has forbidden capability field %s (%s)", field.Name, field.Type)
			}
		}
	}
	if _, err := os.Stat(filepath.Join("..", "..", "cmd", "hermetrix-h1")); err == nil {
		// The command is also scanned textually because its composition root must
		// never gain a normal execution entry point.
		entries, walkErr := filepath.Glob(filepath.Join("..", "..", "cmd", "hermetrix-h1", "*.go"))
		if walkErr != nil {
			t.Fatal(walkErr)
		}
		for _, path := range entries {
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			for call := range forbiddenCalls {
				if strings.Contains(string(body), "."+call+"(") {
					t.Fatalf("fixture command references forbidden call %s", call)
				}
			}
		}
	}
}

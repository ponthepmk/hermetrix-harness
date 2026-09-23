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

func ideProject(t *testing.T, service *Service, name string) Project {
	t.Helper()
	project, err := service.SaveProject(context.Background(), ProjectInput{Name: name, RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return project
}

func TestIDEFormatUsesUnsavedBufferWithoutChangingDisk(t *testing.T) {
	service, _, _ := testProductService(t)
	project := ideProject(t, service, "format")
	ctx := context.Background()
	original := "package main\n\nconst OnDisk = 1\n"
	path := filepath.Join(project.RootPath, "main.go")
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := service.FormatIDEBuffer(ctx, project.ID, IDEFormatInput{Path: "main.go", Content: "package main\nconst Unsaved=2\n"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != "main.go" || result.Content != "package main\n\nconst Unsaved = 2\n" {
		t.Fatalf("format result=%+v", result)
	}
	disk, err := os.ReadFile(path)
	if err != nil || string(disk) != original {
		t.Fatalf("format wrote the file: %q %v", disk, err)
	}
	if err := os.Mkdir(filepath.Join(project.RootPath, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	newPath := "nested/new.go"
	created, err := service.FormatIDEBuffer(ctx, project.ID, IDEFormatInput{Path: newPath, Content: "package nested\nfunc Value()int{return 42}\n"})
	if err != nil || !strings.Contains(created.Content, "func Value() int") {
		t.Fatalf("new file=%+v err=%v", created, err)
	}
	if _, err := os.Stat(filepath.Join(project.RootPath, filepath.FromSlash(newPath))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("format created an unsaved file: %v", err)
	}
}

func TestIDEFormatRejectsInvalidBuffersAndEscapingPaths(t *testing.T) {
	service, _, _ := testProductService(t)
	project := ideProject(t, service, "invalid format")
	ctx := context.Background()
	cases := []struct{ name, path, content string }{
		{"syntax", "main.go", "package main\nfunc ("},
		{"traversal", "../outside.go", "package main\n"},
		{"absolute", filepath.Join(project.RootPath, "absolute.go"), "package main\n"},
		{"empty path", "", "package main\n"},
		{"missing parent", "not-created/new.go", "package main\n"},
		{"path NUL", "bad\x00.go", "package main\n"},
		{"path invalid UTF8", "bad\xff.go", "package main\n"},
		{"oversize", "main.go", strings.Repeat("x", maxWorkbenchFileBytes+1)},
		{"invalid UTF8", "main.go", "package main\n\xff"},
		{"content NUL", "main.go", "package main\n\x00"},
		{"unsupported", "main.py", "print('hello')\n"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if _, err := service.FormatIDEBuffer(ctx, project.ID, IDEFormatInput{Path: item.path, Content: item.content}); err == nil {
				t.Fatal("invalid format request accepted")
			}
		})
	}
	entries, err := os.ReadDir(project.RootPath)
	if err != nil || len(entries) != 0 {
		t.Fatalf("rejected previews changed the project: %v %v", entries, err)
	}
}

func TestIDEOperationsRequireOwnedCodeProject(t *testing.T) {
	service, _, _ := testProductService(t)
	project := ideProject(t, service, "owned IDE")
	ctx := context.Background()
	foreign := identity.WithPrincipal(ctx, "unknown-principal")
	input := IDEFormatInput{Path: "main.go", Content: "package main\n"}
	if _, err := service.FormatIDEBuffer(foreign, project.ID, input); err == nil {
		t.Fatal("foreign principal formatted project content")
	}
	if _, err := service.ProjectIDECapabilities(foreign, project.ID); err == nil {
		t.Fatal("foreign principal read project capabilities")
	}
	if _, err := service.IDEJob(foreign, project.ID, "missing-job"); err == nil {
		t.Fatal("foreign principal read a project job")
	}
	plain, err := service.SaveProject(ctx, ProjectInput{Name: "no code"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.FormatIDEBuffer(ctx, plain.ID, input); !errors.Is(err, ErrProjectHasNoCode) {
		t.Fatalf("codeless format=%v", err)
	}
	if _, err := service.ProjectIDECapabilities(ctx, plain.ID); !errors.Is(err, ErrProjectHasNoCode) {
		t.Fatalf("codeless capabilities=%v", err)
	}
	capabilities, err := service.ProjectIDECapabilities(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !capabilities.Tools["go"] {
		t.Fatal("installed Go toolchain not detected")
	}
}

func TestIDEJobStreamsBoundedOutputWithinProjectAndCancels(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required for this real command test")
	}
	service, _, _ := testProductService(t)
	project := ideProject(t, service, "live IDE job")
	other := ideProject(t, service, "other project")
	ctx := context.Background()
	createCtx, cancelCreate := context.WithCancel(ctx)
	job, err := service.StartCommand(createCtx, CommandInput{ProjectID: project.ID, WorkingDir: ".", Actor: "test-user", Executable: "node",
		Arguments: []string{"-e", "process.stdout.write('LIVE_IDE_MARKER\\n' + 'x'.repeat(2 * 1024 * 1024)); setInterval(() => {}, 1000)"}, TimeoutSeconds: 30})
	cancelCreate()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = service.CancelJob(ctx, job.ID); waitForJob(t, service, job.ID) })
	if _, err := service.IDEJob(ctx, other.ID, job.ID); err == nil {
		t.Fatal("other project read the job")
	}
	if _, err := service.IDEJob(identity.WithPrincipal(ctx, "unknown-principal"), project.ID, job.ID); err == nil {
		t.Fatal("foreign principal read live output")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		live, err := service.IDEJob(ctx, project.ID, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		output, _ := live.Result["output"].(string)
		if live.State == "running" && live.Result["truncated"] == true {
			if !strings.HasPrefix(output, "LIVE_IDE_MARKER\n") || len(output) != maxCommandOutput {
				t.Fatalf("live output was not bounded: prefix=%q bytes=%d", output[:min(len(output), 30)], len(output))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live output never arrived: state=%s bytes=%d error=%s", live.State, len(output), live.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := service.CancelJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	completed := waitForJob(t, service, job.ID)
	if completed.State != "canceled" || !completed.CancelRequested {
		t.Fatalf("cancellation failed: %+v", completed)
	}
	final, err := service.IDEJob(ctx, project.ID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	output, _ := final.Result["output"].(string)
	if !strings.HasPrefix(output, "LIVE_IDE_MARKER\n") || len(output) != maxCommandOutput || final.Result["truncated"] != true {
		t.Fatalf("final receipt lost bounded output: bytes=%d truncated=%v", len(output), final.Result["truncated"])
	}
	if final.Result["artifact_id"] == nil {
		t.Fatal("completed receipt has no durable log artifact")
	}
}

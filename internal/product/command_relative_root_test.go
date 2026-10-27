package product

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
)

// A real Go -C command changes directory before resolving TEMP/TMP. A relative
// data root must not make the command's isolated temp directory relative to it.
func TestCommandWithRelativeDataRootAndGoChangedDirectory(t *testing.T) {
	root, err := os.MkdirTemp(".", "relative-command-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	dataStore, err := store.Open(context.Background(), filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := NewService(dataStore, skills.NewService(dataStore))
	defer service.Close()
	projectRoot := t.TempDir()
	backend := filepath.Join(projectRoot, "backend")
	if err := os.MkdirAll(backend, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"go.mod": "module commandtempfixture\n\ngo 1.23\n",
		"temp_test.go": `package commandtempfixture
import ("os"; "testing")
func TestTemp(t *testing.T) {
 p, err := os.MkdirTemp("", "changed-dir-")
 if err != nil { t.Fatal(err) }
 defer os.RemoveAll(p)
}
`,
	} {
		if err := os.WriteFile(filepath.Join(backend, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	project, err := service.SaveProject(context.Background(), ProjectInput{Name: "relative temp", RootPath: projectRoot})
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.StartCommand(context.Background(), CommandInput{ProjectID: project.ID, Actor: "test", Executable: "go", Arguments: []string{"-C", "backend", "test", "./...", "-count=1"}, TimeoutSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(65 * time.Second)
	for {
		completed, err := service.GetJob(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if completed.State != "queued" && completed.State != "running" {
			if completed.State != "completed" || completed.Result["exit_code"] != float64(0) {
				t.Fatalf("Go command with relative data root: %+v", completed)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("command did not settle: %+v", completed)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

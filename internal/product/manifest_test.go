package product

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectFileManifestExcludesSecretsGeneratedTreesAndSymlinks(t *testing.T) {
	service, _, _ := testProductService(t)
	root := t.TempDir()
	for path, content := range map[string]string{
		"cmd/app/main.go":       "package main\n",
		"internal/app/app.go":   "package app\n",
		"README.md":             "# project\n",
		".env.local":            "TOKEN=secret\n",
		"server.pem":            "secret\n",
		"credentials.json":      "{}\n",
		"secrets/client.go":     "package secrets\n",
		".aws/config":           "credential_process=x\n",
		"node_modules/pkg/x.js": "generated\n",
		"dist/bundle.js":        "generated\n",
		"image.png":             "not source\n",
	} {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "internal", "app", "app.go"), filepath.Join(root, "linked.go")); err != nil {
		t.Fatal(err)
	}
	project, err := service.SaveProject(context.Background(), ProjectInput{Name: "manifest", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.ProjectFileManifest(context.Background(), project.ID, 2000)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"README.md", "cmd/app/main.go", "internal/app/app.go"}
	if len(items) != len(want) {
		t.Fatalf("manifest=%+v", items)
	}
	for index := range want {
		if items[index].Path != want[index] || items[index].Directory || items[index].Symlink {
			t.Fatalf("manifest[%d]=%+v want=%q", index, items[index], want[index])
		}
	}
	limited, err := service.ProjectFileManifest(context.Background(), project.ID, 2)
	if err != nil || len(limited) != 2 {
		t.Fatalf("limited=%+v err=%v", limited, err)
	}
}

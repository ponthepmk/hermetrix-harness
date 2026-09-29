package skills

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseSkillArchive(t *testing.T) {
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	for name, body := range map[string]string{
		"sample-skill/SKILL.md":            "---\nname: sample-skill\ndescription: |\n  Helps with a task.\n  Uses evidence first.\n---\n# Sample\n",
		"sample-skill/references/guide.md": "Reference data, not instructions for the importer.",
	} {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	name, pkg, err := ParseSkillArchive(archive.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if name != "sample-skill" || len(pkg.Files) != 2 {
		t.Fatalf("name=%q files=%d", name, len(pkg.Files))
	}
	if description := ParseManifest(pkg.Markdown()).Description; description != "Helps with a task. Uses evidence first." {
		t.Fatalf("description=%q", description)
	}
	if !CheckPackage(name, pkg).LintPassed {
		t.Fatal("imported package did not pass lint")
	}
}

func TestParseSkillArchiveRejectsTraversal(t *testing.T) {
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	f, err := w.Create("../SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("---\nname: sample-skill\n---\n"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseSkillArchive(archive.Bytes()); err == nil {
		t.Fatal("accepted path traversal")
	}
}

// Set HERMETRIX_SKILL_FIXTURE to inspect a user-supplied archive without
// making a machine-specific path part of the regular test suite.
func TestParseExternalSkillFixture(t *testing.T) {
	path := os.Getenv("HERMETRIX_SKILL_FIXTURE")
	if path == "" {
		t.Skip("no external fixture")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	name, pkg, err := ParseSkillArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(name, "neurix") || len(pkg.Files) < 2 {
		t.Fatalf("unexpected fixture: %s (%d files)", name, len(pkg.Files))
	}
	t.Logf("parsed %s with %d files; checks: %+v", name, len(pkg.Files), CheckPackage(name, pkg))
}

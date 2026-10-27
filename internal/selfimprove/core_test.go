package selfimprove

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreEvidenceRequiresPassingAnchorsAndRealTests(t *testing.T) {
	for _, mode := range []string{"passed", "skipped", "failed", "empty", "wrong-package"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "go.jsonl")
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			encoder := json.NewEncoder(f)
			if mode != "empty" {
				for _, anchor := range requiredCoreTests {
					parts := strings.Split(anchor, "/")
					name := parts[len(parts)-1]
					pkg := strings.Join(parts[:len(parts)-1], "/")
					action := "pass"
					if mode == "skipped" {
						action = "skip"
					}
					if mode == "wrong-package" {
						pkg = "untrusted/lab"
					}
					_ = encoder.Encode(map[string]string{"Action": action, "Package": pkg, "Test": name})
				}
				_ = encoder.Encode(map[string]string{"Action": "pass", "Package": "hermetrix-harness/internal/web"})
			}
			if mode == "failed" {
				_ = encoder.Encode(map[string]string{"Action": "fail", "Package": "core"})
			}
			_ = f.Close()
			_, _, _, err = InspectGoLog(path)
			if (err == nil) != (mode == "passed") {
				t.Fatalf("%s: %v", mode, err)
			}
		})
	}
}

func TestFingerprintProtectsCoreAndTestsWithoutMixingLabArtifacts(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"cmd/hermetrix", "cmd/hermetrix-lab", "internal/core", "internal/selfimprove"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"go.mod", "go.sum", "cmd/hermetrix/main.go", "internal/core/core.go", "internal/core/core_test.go"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte("baseline"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	b, tests, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, "internal/selfimprove/evidence.json"), []byte("lab only"), 0600)
	a, ta, err := Fingerprint(root)
	if err != nil || a != b || ta != tests {
		t.Fatal("lab artifact changed protected core hash")
	}
	_ = os.WriteFile(filepath.Join(root, "internal/core/core.go"), []byte("changed"), 0600)
	a, ta, err = Fingerprint(root)
	if err != nil || a == b || ta != tests {
		t.Fatal("implementation change was not detected separately")
	}
	_ = os.WriteFile(filepath.Join(root, "internal/core/core_test.go"), []byte("weakened"), 0600)
	_, ta, err = Fingerprint(root)
	if err != nil || ta == tests {
		t.Fatal("test change was not detected")
	}
}

func TestFrontendEvidenceRefusesZeroSkippedOrCancelledTests(t *testing.T) {
	for _, mode := range []string{"passed", "zero", "skipped", "cancelled", "missing"} {
		t.Run(mode, func(t *testing.T) {
			raw := "# tests 2\n# pass 2\n# fail 0\n# cancelled 0\n# skipped 0\n"
			switch mode {
			case "zero":
				raw = strings.ReplaceAll(raw, "2", "0")
			case "skipped":
				raw = strings.Replace(raw, "skipped 0", "skipped 1", 1)
			case "cancelled":
				raw = strings.Replace(raw, "cancelled 0", "cancelled 1", 1)
			case "missing":
				raw = "looks fine"
			}
			path := filepath.Join(t.TempDir(), "node.txt")
			_ = os.WriteFile(path, []byte(raw), 0600)
			_, _, err := inspectNodeLog(path)
			if (err == nil) != (mode == "passed") {
				t.Fatalf("%s: %v", mode, err)
			}
		})
	}
}

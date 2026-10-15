package taskcoord

import (
	"crypto/sha256"
	"fmt"
	"hermetrix-harness/internal/worker"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileAssertionsRejectRealNegativeFixtures(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	root := t.TempDir()
	good := "function sum(a,b){return a+b;}\r\n"
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(good)))
	run := func(argv []string, content string) bool {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "sum.js"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		command, err := compileFileAssertion(argv)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, command[1:]...)
		cmd.Dir = root
		_, err = cmd.CombinedOutput()
		return err == nil
	}
	for _, argv := range [][]string{{"assert-file-sha256", "sum.js", hash}, {"assert-file-contains", "sum.js", "return a+b"}} {
		if !run(argv, good) {
			t.Fatal("correct fixture failed")
		}
		if run(argv, strings.Replace(good, "a+b", "a-b", 1)) {
			t.Fatal("wrong arithmetic accepted")
		}
	}
	if run([]string{"assert-file-sha256", "sum.js", hash}, strings.ReplaceAll(good, "\r\n", "\n")) {
		t.Fatal("byte change accepted")
	}
	task := worker.PlanTask{AllowedExecutables: []string{"node"}, RepositoryFiles: []worker.PlanFile{{Path: "sum.js", SHA256: hash}}}
	result := worker.PlanResult{Steps: []worker.PlannedStep{{Key: "fix", Checks: []string{"assert-file-sha256 sum.js " + hash}}}}
	if err := validatePlannedChecks(result, task); err != nil {
		t.Fatal(err)
	}
	result.Steps[0].Checks = []string{"assert-file-sha256 sum.js " + strings.Repeat("0", 64)}
	if err := validatePlannedChecks(result, task); err == nil {
		t.Fatal("invented baseline accepted")
	}
	for _, p := range []string{"../sum.js", "D:/secret", "/secret", "a/../sum.js", "a\\sum.js"} {
		if _, err := compileFileAssertion([]string{"assert-file-sha256", p, hash}); err == nil {
			t.Fatalf("unsafe path %q accepted", p)
		}
	}
}

func TestEmptyDiscoveryCannotVerifyAcceptance(t *testing.T) {
	for _, c := range []struct {
		exe   string
		args  []string
		out   string
		empty bool
	}{
		{"node", []string{"--test"}, "TAP version 13\n# tests 0\n# pass 0\n", true},
		{"node", []string{"--test"}, "# tests 2\n# pass 2\n", false},
		{"python", []string{"-m", "unittest", "discover"}, "Ran 0 tests in 0.000s\nOK", true},
		{"node", []string{"app.js"}, "# tests 0", false},
		{"npm", []string{"test"}, "custom output", false},
		{"go", []string{"test", "./..."}, "?\tapp [no test files]\n", true},
		{"go", []string{"test", "./..."}, "?\tapp [no test files]\nok\tapp/core 0.5s\n", false},
		{"go", []string{"test", "-run", "Missing", "./..."}, "ok\tapp/core 0.5s [no tests to run]\n", true},
	} {
		if got := emptyTestDiscovery(c.exe, c.args, c.out); got != c.empty {
			t.Fatalf("%+v got %v", c, got)
		}
	}
}

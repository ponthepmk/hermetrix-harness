package selfimprove

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type CoreReceipt struct {
	Passed      bool     `json:"passed"`
	SourceHash  string   `json:"source_hash"`
	TestHash    string   `json:"test_hash"`
	GoPassed    int      `json:"go_passed"`
	GoPackages  int      `json:"go_packages"`
	NodePassed  int      `json:"node_passed"`
	NodeSkipped int      `json:"node_skipped"`
	Skipped     []string `json:"skipped"`
	GoLog       string   `json:"go_log"`
	NodeLog     string   `json:"node_log"`
	Errors      []string `json:"errors"`
}

// Fingerprint excludes only this additive lab; it includes all original core
// implementation/test bytes, embedded assets, fixtures, module and dependency
// locks. A changed core cannot inherit a passing pre-experiment receipt.
func Fingerprint(workspace string) (string, string, error) {
	var paths []string
	for _, root := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(workspace, root), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(workspace, path)
			rel = filepath.ToSlash(rel)
			if d.IsDir() && (rel == "cmd/hermetrix-lab" || rel == "internal/selfimprove") {
				return filepath.SkipDir
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("core symlink is not supported: %s", rel)
			}
			if !d.IsDir() {
				paths = append(paths, rel)
			}
			return nil
		})
		if err != nil {
			return "", "", err
		}
	}
	paths = append(paths, "go.mod", "go.sum")
	sort.Strings(paths)
	source, tests := sha256.New(), sha256.New()
	for _, rel := range paths {
		raw, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(rel)))
		if err != nil {
			return "", "", err
		}
		sum := sha256.Sum256(raw)
		fmt.Fprintf(source, "%s\x00%x\n", rel, sum)
		if strings.HasSuffix(rel, "_test.go") || strings.HasSuffix(rel, ".test.js") {
			fmt.Fprintf(tests, "%s\x00%x\n", rel, sum)
		}
	}
	return hex.EncodeToString(source.Sum(nil)), hex.EncodeToString(tests.Sum(nil)), nil
}

var requiredCoreTests = []string{
	"hermetrix-harness/internal/taskengine/TestDecisionRequiresReconciliationBeforeAnyNewWork",
	"hermetrix-harness/internal/taskengine/TestUserAcceptanceBindsToLatestEvidence",
	"hermetrix-harness/internal/learning/TestProjectTaskReviewCannotPromoteGlobalSkill",
	"hermetrix-harness/internal/agent/TestSessionSkillCatalogHonorsWorkspaceScope",
	"hermetrix-harness/internal/skills/TestPromotionRefusesACandidateThatWasNeverEvaluated",
	"hermetrix-harness/internal/skills/TestAuthorityManualDefaultGatedPromotionAndRollback",
	"hermetrix-harness/internal/web/TestCockpitHydratesInARealBrowser",
}

func InspectGoLog(path string) (int, int, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 65536), 8*1024*1024)
	passed, packages := 0, 0
	var skips []string
	seen := map[string]bool{}
	failed := false
	for scanner.Scan() {
		var e struct{ Action, Package, Test string }
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return 0, 0, nil, fmt.Errorf("invalid Go test evidence: %w", err)
		}
		switch e.Action {
		case "pass":
			if e.Test != "" {
				passed++
				seen[e.Package+"/"+e.Test] = true
			} else {
				packages++
			}
		case "fail":
			failed = true
		case "skip":
			if e.Test != "" {
				skips = append(skips, e.Package+"/"+e.Test)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return passed, packages, skips, err
	}
	if failed || passed == 0 || packages == 0 {
		return passed, packages, skips, fmt.Errorf("core test run failed or contains no passing tests")
	}
	for _, name := range requiredCoreTests {
		if !seen[name] {
			return passed, packages, skips, fmt.Errorf("required core test did not pass: %s", name)
		}
	}
	return passed, packages, skips, nil
}

var nodeCounts = regexp.MustCompile(`(?m)^# (tests|pass|fail|cancelled|skipped) ([0-9]+)\r?$`)

func inspectNodeLog(path string) (int, int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	counts := map[string]int{}
	for _, match := range nodeCounts.FindAllSubmatch(raw, -1) {
		n, _ := strconv.Atoi(string(match[2]))
		counts[string(match[1])] = n
	}
	if len(counts) != 5 || counts["tests"] == 0 || counts["pass"] == 0 || counts["fail"] != 0 || counts["cancelled"] != 0 || counts["skipped"] != 0 || counts["tests"] != counts["pass"] {
		return counts["pass"], counts["skipped"], fmt.Errorf("frontend test evidence is missing, failed or skipped")
	}
	return counts["pass"], counts["skipped"], nil
}

func commandLog(ctx context.Context, workspace, path, command string, args []string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "HERMETRIX_REQUIRE_BROWSER_E2E=1")
	cmd.Stdout = f
	// Go emits structured test output on stdout; compiler/tool errors on stderr
	// are kept separate so a malformed/build-failed stream cannot look passing.
	stderr, err := os.OpenFile(path+".stderr", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer stderr.Close()
	cmd.Stderr = stderr
	return cmd.Run()
}

func CheckCore(ctx context.Context, workspace, outDir, prefix string) CoreReceipt {
	r := CoreReceipt{Errors: []string{}, Skipped: []string{}, GoLog: filepath.Join(outDir, prefix+"-go.jsonl"), NodeLog: filepath.Join(outDir, prefix+"-node.txt")}
	var err error
	r.SourceHash, r.TestHash, err = Fingerprint(workspace)
	if err != nil {
		r.Errors = append(r.Errors, err.Error())
		return r
	}
	if err = commandLog(ctx, workspace, r.GoLog, "go", []string{"test", "./...", "-count=1", "-json"}); err != nil {
		r.Errors = append(r.Errors, "Go core check: "+err.Error())
	}
	r.GoPassed, r.GoPackages, r.Skipped, err = InspectGoLog(r.GoLog)
	if err != nil {
		r.Errors = append(r.Errors, err.Error())
	}
	files, err := filepath.Glob(filepath.Join(workspace, "internal", "web", "ui", "*.test.js"))
	if err != nil || len(files) == 0 {
		r.Errors = append(r.Errors, "no frontend tests discovered")
	} else {
		args := append([]string{"--test"}, files...)
		if err = commandLog(ctx, workspace, r.NodeLog, "node", args); err != nil {
			r.Errors = append(r.Errors, "frontend core check: "+err.Error())
		}
		r.NodePassed, r.NodeSkipped, err = inspectNodeLog(r.NodeLog)
		if err != nil {
			r.Errors = append(r.Errors, err.Error())
		}
	}
	after, testAfter, err := Fingerprint(workspace)
	if err != nil || after != r.SourceHash || testAfter != r.TestHash {
		r.Errors = append(r.Errors, "core bytes changed during tests")
	}
	r.Passed = len(r.Errors) == 0
	return r
}

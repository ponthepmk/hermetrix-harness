//go:build windows

package scripts

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The launcher starts this test binary as its isolated fake harness. No live
// database, real model or installed harness executable is used by these tests.
func TestMain(m *testing.M) {
	if os.Getenv("HERMETRIX_LAUNCHER_TEST_HELPER") == "1" && len(os.Args) > 1 && os.Args[1] == "serve" {
		args := map[string]string{}
		for i := 2; i+1 < len(os.Args); i += 2 {
			args[os.Args[i]] = os.Args[i+1]
		}
		if args["--reviewer-provider-id"] != "fixture-reviewer" {
			fmt.Fprintln(os.Stderr, "configured reviewer is unavailable; no fallback")
			os.Exit(2)
		}
		if strings.Contains(args["--data"], "timeout fixture") {
			time.Sleep(10 * time.Minute)
			os.Exit(4)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "service": "hermetrix-harness"})
		})
		mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{{"root_path": args["--workspace"]}})
		})
		mux.HandleFunc("/api/providers", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{{"id": "fixture-reviewer", "name": "Offline fixture",
				"enabled": true, "credential_ready": true, "base_url": "http://127.0.0.1:1/v1"}})
		})
		if err := http.ListenAndServe(args["--listen"], mux); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type launcherFixture struct {
	script, workspace, data, listen, reviewer string
}

func newLauncherFixture(t *testing.T) launcherFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo with spaces")
	f := launcherFixture{script: filepath.Join(root, "scripts", "start-easiiot-harness.ps1"),
		workspace: filepath.Join(root, "workspace with spaces"), data: filepath.Join(root, "data with spaces"), reviewer: "fixture-reviewer"}
	for _, path := range []string{filepath.Dir(f.script), f.workspace, f.data, filepath.Join(root, ".tmp-ui")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	copyFile := func(from, to string) {
		body, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, body, 0700); err != nil {
			t.Fatal(err)
		}
	}
	copyFile("start-easiiot-harness.ps1", f.script)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	copyFile(binary, filepath.Join(root, ".tmp-ui", "hermetrix-easiiot-pilot.exe"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.listen = listener.Addr().String()
	listener.Close()
	return f
}

func (f launcherFixture) run(t *testing.T, extra ...string) (string, error) {
	t.Helper()
	args := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", f.script,
		"-WorkspaceRoot", f.workspace, "-DataRoot", f.data, "-Listen", f.listen,
		"-ReviewerProviderId", f.reviewer, "-StartupTimeoutSec", "5"}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", append(args, extra...)...)
	cmd.Env = append(os.Environ(), "HERMETRIX_LAUNCHER_TEST_HELPER=1")
	// Start-Process children can inherit pipe handles on Windows. A file records
	// the launcher output without making cmd.Wait depend on the detached server.
	logPath := filepath.Join(t.TempDir(), "launcher-output.txt")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	err = cmd.Run()
	log.Close()
	body, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(body), err
}

func requireLauncherFailure(t *testing.T, output string, err error, message string) {
	t.Helper()
	if err == nil || !strings.Contains(output, message) {
		t.Fatalf("expected %q failure, err=%v output=%s", message, err, output)
	}
}

func launcherPID(t *testing.T, output string) int {
	t.Helper()
	match := regexp.MustCompile(`\(PID (\d+)\)`).FindStringSubmatch(output)
	if len(match) != 2 {
		t.Fatalf("missing launched PID: %s", output)
	}
	pid, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func requireLaunchedProcessStopped(t *testing.T, output string) {
	t.Helper()
	pid := launcherPID(t, output)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		fmt.Sprintf("[bool](Get-Process -Id %d -ErrorAction SilentlyContinue)", pid))
	body, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(body)) != "False" {
		if process, findErr := os.FindProcess(pid); findErr == nil {
			process.Kill()
		}
		t.Fatalf("failed startup left its child running: %s %v", body, err)
	}
}

func TestLauncherDetachedReuseAndIdentity(t *testing.T) {
	f := newLauncherFixture(t)
	output, err := f.run(t)
	if err != nil || !strings.Contains(output, "Harness ready:") || !strings.Contains(output, "reviewer is offline") {
		t.Fatalf("offline reviewer must allow detached UI startup: err=%v output=%s", err, output)
	}
	pid := launcherPID(t, output)
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { process.Kill() })
	output, err = f.run(t, "-CheckOnly")
	if err != nil || !strings.Contains(output, "Harness ready:") || strings.Contains(output, "Starting Hermetrix") {
		t.Fatalf("must reuse surviving instance: err=%v output=%s", err, output)
	}
	output, err = f.run(t, "-Rebuild")
	requireLauncherFailure(t, output, err, "Stop this running instance explicitly")
	other := f
	other.reviewer = "different-reviewer"
	output, err = other.run(t)
	requireLauncherFailure(t, output, err, "reviewer selection")
	other = f
	other.data = filepath.Join(filepath.Dir(f.data), "other data")
	os.MkdirAll(other.data, 0700)
	output, err = other.run(t)
	requireLauncherFailure(t, output, err, "It was left untouched")
	other = f
	other.workspace = filepath.Join(filepath.Dir(f.workspace), "other workspace")
	os.MkdirAll(other.workspace, 0700)
	output, err = other.run(t)
	requireLauncherFailure(t, output, err, "another harness workspace")
	output, err = f.run(t, "-CheckOnly")
	if err != nil {
		t.Fatalf("identity/rebuild rejection stopped original instance: %s", output)
	}
}

func TestLauncherForeignPortIsUntouched(t *testing.T) {
	f := newLauncherFixture(t)
	listener, err := net.Listen("tcp", f.listen)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{ "ok": false }`) })}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	output, err := f.run(t)
	requireLauncherFailure(t, output, err, "occupied")
	response, err := http.Get("http://" + f.listen)
	if err != nil {
		t.Fatal("foreign server was stopped:", err)
	}
	response.Body.Close()
}

func TestLauncherStartupTimeoutIsReported(t *testing.T) {
	f := newLauncherFixture(t)
	f.data = filepath.Join(filepath.Dir(f.data), "timeout fixture")
	os.MkdirAll(f.data, 0700)
	output, err := f.run(t)
	requireLauncherFailure(t, output, err, "did not become ready")
	if !strings.Contains(output, ".error.log") {
		t.Fatal("missing retained diagnostic log")
	}
	requireLaunchedProcessStopped(t, output)
}

func TestLauncherMissingReviewerFailsWithoutFallback(t *testing.T) {
	f := newLauncherFixture(t)
	f.reviewer = "missing-reviewer"
	output, err := f.run(t)
	requireLauncherFailure(t, output, err, "Harness exited with code 2")
	requireLaunchedProcessStopped(t, output)
	logs, err := filepath.Glob(filepath.Join(filepath.Dir(filepath.Dir(f.script)), ".tmp-ui", "*.error.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("startup failure must retain one diagnostic log: %v %v", logs, err)
	}
	body, err := os.ReadFile(logs[0])
	if err != nil || !strings.Contains(string(body), "no fallback") {
		t.Fatalf("lost exact failure: %s %v", body, err)
	}
}

func TestLauncherUnexpectedHealthyProcessIsUntouched(t *testing.T) {
	f := newLauncherFixture(t)
	listener, err := net.Listen("tcp", f.listen)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{ "ok": true, "service": "hermetrix-harness" }`)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	output, err := f.run(t)
	requireLauncherFailure(t, output, err, "It was left untouched")
	response, err := http.Get("http://" + f.listen)
	if err != nil {
		t.Fatal("unexpected healthy server was stopped:", err)
	}
	response.Body.Close()
}

func TestLauncherCheckOnlyDoesNotStart(t *testing.T) {
	f := newLauncherFixture(t)
	output, err := f.run(t, "-CheckOnly")
	requireLauncherFailure(t, output, err, "Harness is not running")
	if strings.Contains(output, "Starting Hermetrix") {
		t.Fatal("CheckOnly started a process")
	}
	listener, err := net.Listen("tcp", f.listen)
	if err != nil {
		t.Fatal("CheckOnly occupied the port:", err)
	}
	listener.Close()
}

func TestLauncherInvalidPortDoesNotStart(t *testing.T) {
	f := newLauncherFixture(t)
	f.listen = "127.0.0.1:65536"
	output, err := f.run(t)
	requireLauncherFailure(t, output, err, "between 1 and 65535")
	if strings.Contains(output, "Starting Hermetrix") {
		t.Fatal("invalid port started a process")
	}
}

func TestDeliveryLauncherOpensOfflineAndPreservesExactInstance(t *testing.T) {
	f := newLauncherFixture(t)
	root := filepath.Dir(filepath.Dir(f.script))
	f.script = filepath.Join(root, "scripts", "start-delivery-harness.ps1")
	body, err := os.ReadFile("start-delivery-harness.ps1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.script, body, 0700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(filepath.Join(root, ".tmp-ui", "hermetrix-easiiot-pilot.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".tmp-ui", "hermetrix-delivery-final.exe"), binary, 0700); err != nil {
		t.Fatal(err)
	}
	output, err := f.run(t)
	if err != nil || !strings.Contains(output, "Harness ready:") || !strings.Contains(output, "reviewer is offline") {
		t.Fatalf("Delivery must open existing work without inference: %v %s", err, output)
	}
	pid := launcherPID(t, output)
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { process.Kill() })
	output, err = f.run(t, "-CheckOnly")
	if err != nil || strings.Contains(output, "Starting Hermetrix") {
		t.Fatalf("Delivery did not reuse exact surviving instance: %v %s", err, output)
	}
	output, err = f.run(t, "-Rebuild")
	requireLauncherFailure(t, output, err, "Stop this running instance explicitly")
	other := f
	other.reviewer = "different-reviewer"
	output, err = other.run(t)
	requireLauncherFailure(t, output, err, "reviewer selection")
	other = f
	other.data = filepath.Join(root, "wrong data")
	os.MkdirAll(other.data, 0700)
	output, err = other.run(t)
	requireLauncherFailure(t, output, err, "It was left untouched")
	output, err = f.run(t, "-CheckOnly")
	if err != nil {
		t.Fatalf("Delivery rebuild/scope rejection stopped the original: %v %s", err, output)
	}
}

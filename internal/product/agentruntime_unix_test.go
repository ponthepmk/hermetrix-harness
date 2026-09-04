//go:build !windows

package product

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// grandchildScript makes the node process StartRun launches spawn a second,
// unrelated node process:
//   - stdio: 'ignore' means the grandchild does not hold the parent's
//     stdout/stderr pipes open. Without that, Cmd's WaitDelay would drain and
//     forcibly close those pipes on its own 2-second grace period regardless
//     of whether configureProcessTermination works at all, which would let
//     this test pass even against a broken kill.
//   - No `detached: true` on the spawn: the grandchild deliberately stays a
//     member of the parent's process group rather than starting its own. The
//     whole point of this test is to prove a group-kill reaches the entire
//     subtree; giving the grandchild its own group would prove the opposite
//     thing (that a process can escape one on purpose) and the group-kill
//     would legitimately never reach it.
//
// It writes its own pid to the path given as the first CLI argument and then
// loops forever, just like the parent, so it is still alive at the timeout
// unless something actually reaps it.
const grandchildScript = `
const { spawn } = require('child_process');
const fs = require('fs');
const child = spawn(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], { stdio: 'ignore' });
child.unref();
fs.writeFileSync(process.argv[1], String(child.pid));
setInterval(() => {}, 1000);
`

// TestStartRunTerminatesTheWholeProcessGroupNotJustTheDirectChild is the bound
// TestStartRunTerminatesACommandThatWillNotFinish cannot see: that test only
// asserts final.State and final.Error, both of which come from ctx.Err() and
// stay "failed"/"timed out" even with configureProcessTermination deleted
// outright, because exec.CommandContext's default Cancel (Process.Kill) still
// kills the direct node child on its own. That mutation, and a second one
// that leaves Cancel a no-op and lets Cmd's WaitDelay fallback do the killing
// instead, both touch nothing but the direct child -- neither is visible
// unless something outside that one pid is checked.
//
// A grandchild the direct child spawns, and detaches from its own stdio (but
// not its process group -- see grandchildScript), is that something: it is
// reparented to init the instant its parent dies, so it only stops existing
// if the whole process *group*, not just the one pid, is signaled.
func TestStartRunTerminatesTheWholeProcessGroupNotJustTheDirectChild(t *testing.T) {
	service, project, cleanup := newProductTestServiceWithProject(t)
	defer cleanup()
	pidFile := filepath.Join(project.RootPath, "grandchild.pid")
	request := agentRunRequest(project.ID)
	request.Executable = "node"
	request.Arguments = []string{"-e", grandchildScript, pidFile}
	request.TimeoutSeconds = 1

	// Registered before waitForRun, and re-reads the pid file itself rather
	// than closing over a pid variable set later: waitForRun can itself run
	// up to its own 30s timeout without returning, and every t.Fatal inside
	// readGrandchildPID below -- including "the pid file never appeared" --
	// exits this test before either of those would otherwise register a
	// cleanup. Only a cleanup registered here, first, runs on all of those
	// paths.
	t.Cleanup(func() { killIfStillAlive(pidFile) })

	final := waitForRun(t, service, mustStart(t, service, request).JobID)

	pid := readGrandchildPID(t, pidFile)

	if final.State != "failed" {
		t.Fatalf("state = %q, want failed", final.State)
	}
	if !strings.Contains(final.Error, "timed out") {
		t.Fatalf("error = %q, want it to say the command timed out", final.Error)
	}

	// Reaping an orphaned, killed grandchild is not instantaneous -- its new
	// parent (init/launchd) has to notice and collect it -- so retry for a
	// few seconds rather than checking once.
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = syscall.Kill(pid, 0)
		if lastErr == syscall.ESRCH {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("grandchild pid %d still answers to signal 0 after 5s (err = %v): "+
		"the process group was not terminated, only the direct child was", pid, lastErr)
}

// readGrandchildPID waits for grandchildScript to have written its pid file,
// which happens within milliseconds of the command starting -- long before
// the 1 second timeout -- and fails the test if it never shows up. It is
// called only after waitForRun has already returned, so by the time this
// polls, the file (if it exists at all) was written well in the past; the
// short retry here is only a guard against filesystem-visibility lag, not a
// race with the process's own lifetime.
func readGrandchildPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr != nil {
				t.Fatalf("grandchild pid file contained %q: %v", data, convErr)
			}
			return pid
		}
		if !os.IsNotExist(err) {
			t.Fatalf("reading grandchild pid file: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("grandchild never wrote its pid file")
	return 0
}

// killIfStillAlive is the cleanup backstop. It re-reads the pid file itself
// rather than trusting a pid variable captured earlier in the test, so it
// still runs correctly on every early-exit path: waitForRun timing out
// without ever returning, or any t.Fatal inside readGrandchildPID -- up to
// and including the pid file never having been written at all, in which
// case there is nothing to read and this is a no-op.
//
// It re-checks aliveness with Kill(pid, 0) before signaling anything, rather
// than killing unconditionally: by the time this runs, the real group-kill
// under test has very likely already reaped the grandchild, and on a
// pid-recycling OS an unconditional SIGKILL some seconds later could land on
// whatever unrelated process the OS has since handed that pid to -- on a
// developer's own machine, not just in CI.
func killIfStillAlive(pidFile string) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return
	}
	if syscall.Kill(pid, 0) == syscall.ESRCH {
		return // already reaped by the bound under test -- nothing to do
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

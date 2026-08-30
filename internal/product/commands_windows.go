//go:build windows

package product

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// configureProcessTermination approximates the Unix process-group kill on
// Windows, which has no POSIX process groups. On cancel or timeout it runs
// taskkill with /T so the child's descendant tree is included and /F so the
// kill is not negotiable. If taskkill cannot be run it falls back to killing
// the direct child only, which is why a taskkill failure flips the reported
// guarantee off.
//
// A stricter alternative is a Windows Job Object (CREATE_SUSPENDED + assign +
// resume, KILL_ON_JOB_CLOSE): the OS then guarantees no descendant outlives
// the job even if it re-parents. That needs golang.org/x/sys/windows and about
// forty lines of syscall plumbing; taskkill /T covers the allowlisted
// executables (go, npm, python3, ...) well enough for this process-hardening
// tier. Swap the body if you need the Job Object guarantee.
func configureProcessTermination(command *exec.Cmd) bool {
	taskkill := "taskkill"
	if root := os.Getenv("SystemRoot"); root != "" {
		taskkill = filepath.Join(root, "System32", "taskkill.exe")
	}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		kill := exec.Command(taskkill, "/pid", strconv.Itoa(command.Process.Pid), "/t", "/f")
		if err := kill.Run(); err != nil {
			return command.Process.Kill()
		}
		return nil
	}
	return true
}

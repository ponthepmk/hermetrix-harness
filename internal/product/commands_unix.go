//go:build !windows

package product

import (
	"os/exec"
	"syscall"
)

// isolateProcessGroup puts a command in its own process group and kills the
// whole group on cancel.
//
// Without it, cancelling a timed-out `go test` kills the parent and leaves its
// children running against the workspace -- the timeout stops being a bound on
// what the command can do.
//
// It lives in a build-tagged file because syscall.Kill and SysProcAttr.Setpgid
// do not exist on Windows at all. Guarding the call with runtime.GOOS was not
// enough: the reference is resolved when the package compiles, so the whole
// binary failed to build for Windows even though the branch could never run
// there. A runtime check cannot hide a compile-time symbol.
func isolateProcessGroup(command *exec.Cmd) bool {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	return true
}

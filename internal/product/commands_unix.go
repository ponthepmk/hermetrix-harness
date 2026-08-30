//go:build !windows

package product

import (
	"os/exec"
	"syscall"
)

<<<<<<< HEAD
// configureProcessTermination puts the child into its own process group so a
// cancel or timeout can take the whole subtree down with a single signal to
// the negated group id. It returns true because that subtree kill is
// guaranteed on POSIX.
func configureProcessTermination(command *exec.Cmd) bool {
=======
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
>>>>>>> c7c3083f6d0b28b6e40a2c64796b5637fce5b8fd
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	return true
}

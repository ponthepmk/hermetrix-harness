//go:build !windows

package product

import (
	"os/exec"
	"syscall"
)

// configureProcessTermination puts the child into its own process group so a
// cancel or timeout can take the whole subtree down with a single signal to
// the negated group id. It returns true because that subtree kill is
// guaranteed on POSIX.
func configureProcessTermination(command *exec.Cmd) bool {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	return true
}

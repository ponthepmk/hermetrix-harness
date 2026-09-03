//go:build !windows

package mcp

import (
	"os/exec"
	"syscall"
)

// configureStdioTermination puts the MCP server in its own process group so
// stopping it takes its children with it. An `npx` launcher is a shim in front
// of the real server process, and killing only the shim leaves that server
// running with a closed stdin.
func configureStdioTermination(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}

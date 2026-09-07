//go:build !darwin && !linux

package product

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

func prepareSandboxCommand(ctx context.Context, executable string, arguments []string, workingDir, _ string) (*exec.Cmd, commandSandbox, func(), error) {
	status := commandSandbox{Kind: "unavailable", WriteScope: "process-hardening-only"}
	if os.Getenv("HERMETRIX_REQUIRE_OS_SANDBOX") == "1" {
		return nil, status, func() {}, fmt.Errorf("an OS command sandbox is not available on this platform")
	}
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = workingDir
	return command, status, func() {}, nil
}

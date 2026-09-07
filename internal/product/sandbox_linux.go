//go:build linux

package product

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func prepareSandboxCommand(ctx context.Context, executable string, arguments []string, workingDir, projectRoot string) (*exec.Cmd, commandSandbox, func(), error) {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		status := commandSandbox{Kind: "unavailable", WriteScope: "process-hardening-only"}
		if os.Getenv("HERMETRIX_REQUIRE_OS_SANDBOX") == "1" {
			return nil, status, func() {}, fmt.Errorf("bubblewrap is required but was not found")
		}
		command := exec.CommandContext(ctx, executable, arguments...)
		command.Dir = workingDir
		return command, status, func() {}, nil
	}
	status := commandSandbox{Kind: "linux-bubblewrap", Enforced: true, NetworkIsolated: true, WriteScope: "project-and-runtime-caches"}
	args := []string{"--die-with-parent", "--unshare-net", "--new-session", "--ro-bind", "/", "/",
		"--dev-bind", "/dev", "/dev", "--proc", "/proc", "--bind", projectRoot, projectRoot}
	seen := map[string]bool{filepath.Clean(projectRoot): true}
	writeRoots := []string{os.TempDir()}
	for _, key := range []string{"TMPDIR", "GOCACHE", "GOMODCACHE", "GOPATH"} {
		root := strings.TrimSpace(os.Getenv(key))
		if root != "" {
			writeRoots = append(writeRoots, root)
		}
	}
	if userRoot, userErr := os.UserHomeDir(); userErr == nil {
		writeRoots = append(writeRoots, filepath.Join(userRoot, "go"), filepath.Join(userRoot, ".cache", "go-build"))
	}
	for _, root := range writeRoots {
		root = filepath.Clean(root)
		if seen[root] {
			continue
		}
		if _, statErr := os.Stat(root); statErr != nil {
			continue
		}
		seen[root] = true
		args = append(args, "--bind", root, root)
	}
	args = append(args, "--chdir", workingDir, executable)
	args = append(args, arguments...)
	command := exec.CommandContext(ctx, bwrap, args...)
	command.Dir = workingDir
	return command, status, func() {}, nil
}

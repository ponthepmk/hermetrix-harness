//go:build darwin

package product

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func prepareSandboxCommand(ctx context.Context, executable string, arguments []string, workingDir, projectRoot string) (*exec.Cmd, commandSandbox, func(), error) {
	status := commandSandbox{Kind: "macos-seatbelt", Enforced: true, NetworkIsolated: true, WriteScope: "project-and-runtime-caches"}
	profile, err := os.CreateTemp("", "hermetrix-command-*.sb")
	if err != nil {
		return nil, status, func() {}, err
	}
	cleanup := func() { _ = os.Remove(profile.Name()) }
	writeRoots := []string{projectRoot, os.TempDir()}
	for _, key := range []string{"TMPDIR", "GOCACHE", "GOMODCACHE", "GOPATH"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			writeRoots = append(writeRoots, value)
		}
	}
	if userRoot, userErr := os.UserHomeDir(); userErr == nil {
		writeRoots = append(writeRoots, filepath.Join(userRoot, "go"), filepath.Join(userRoot, "Library", "Caches", "go-build"))
	}
	var policy strings.Builder
	policy.WriteString("(version 1)\n(deny default)\n(allow process*)\n(allow file-read*)\n")
	policy.WriteString("(allow sysctl-read)\n(allow mach-lookup)\n")
	// Child processes commonly attach ignored stdio to /dev/null. Permit only
	// that device; without it safe process-tree execution fails before the
	// child can start.
	policy.WriteString("(allow file-write* (literal \"/dev/null\"))\n")
	for _, root := range writeRoots {
		absolute, pathErr := filepath.Abs(root)
		if pathErr != nil {
			cleanup()
			return nil, status, func() {}, pathErr
		}
		// Seatbelt compares the kernel's canonical path. On macOS /var and
		// /tmp normally traverse /private, so an otherwise-correct lexical
		// rule would deny writes to Go/test temporary directories.
		if canonical, evalErr := filepath.EvalSymlinks(absolute); evalErr == nil {
			absolute = canonical
		}
		policy.WriteString("(allow file-write* (subpath ")
		policy.WriteString(strconv.Quote(absolute))
		policy.WriteString("))\n")
	}
	if _, err := profile.WriteString(policy.String()); err != nil {
		_ = profile.Close()
		cleanup()
		return nil, status, func() {}, err
	}
	if err := profile.Close(); err != nil {
		cleanup()
		return nil, status, func() {}, err
	}
	args := append([]string{"-f", profile.Name(), executable}, arguments...)
	command := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", args...)
	command.Dir = workingDir
	return command, status, cleanup, nil
}

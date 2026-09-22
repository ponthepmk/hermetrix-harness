//go:build windows

package product

func platformCapabilities() RuntimeCapabilities {
	return RuntimeCapabilities{
		InteractiveTerminal:    windowsTerminalAvailable(),
		ManagedBrowser:         true,
		CommandSandbox:         "process-hardening-only",
		ProcessTreeContainment: "windows-job-object",
	}
}

//go:build !windows && !linux && !darwin

package product

func platformCapabilities() RuntimeCapabilities {
	return RuntimeCapabilities{
		InteractiveTerminal:    false,
		ManagedBrowser:         false,
		CommandSandbox:         "unavailable",
		ProcessTreeContainment: "root-process-only",
	}
}

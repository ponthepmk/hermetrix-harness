//go:build linux

package product

func platformCapabilities() RuntimeCapabilities {
	return RuntimeCapabilities{
		InteractiveTerminal:    true,
		ManagedBrowser:         true,
		CommandSandbox:         "linux-bubblewrap-when-installed",
		ProcessTreeContainment: "process-group",
	}
}

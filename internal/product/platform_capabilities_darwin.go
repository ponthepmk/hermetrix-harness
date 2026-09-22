//go:build darwin

package product

func platformCapabilities() RuntimeCapabilities {
	return RuntimeCapabilities{
		InteractiveTerminal:    true,
		ManagedBrowser:         true,
		CommandSandbox:         "macos-seatbelt",
		ProcessTreeContainment: "process-group",
	}
}

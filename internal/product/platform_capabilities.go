package product

import "errors"

var ErrInteractiveTerminalUnavailable = errors.New("interactive terminal is not supported on this platform build")

// RuntimeCapabilities describes properties of the running build and host that
// materially change what a request can do. Values are explicit instead of a
// single platform name so clients never infer support from GOOS.
type RuntimeCapabilities struct {
	InteractiveTerminal    bool   `json:"interactive_terminal"`
	ManagedBrowser         bool   `json:"managed_browser"`
	CommandSandbox         string `json:"command_sandbox"`
	ProcessTreeContainment string `json:"process_tree_containment"`
	CredentialProtection   string `json:"credential_protection"`
}

func PlatformCapabilities(credentialProtection string) RuntimeCapabilities {
	capabilities := platformCapabilities()
	capabilities.CredentialProtection = credentialProtection
	return capabilities
}

package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// browserCommand returns the OS handoff that opens url in the user's default
// browser. It is split out from openBrowser so the per-platform choice can be
// tested without actually launching anything.
func browserCommand(goos, url string) (name string, args []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "windows":
		// rundll32 hands the URL straight to the shell's protocol handler
		// without spawning a cmd window or going through start's quoting.
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}

// openBrowser asks the OS to open url. Every supported launcher returns as soon
// as the browser has been handed the URL, so Run does not block on the browser
// staying open. A failure is returned, not fatal: `serve --open` is a
// convenience over a server that is already listening.
func openBrowser(url string) error {
	name, args := browserCommand(runtime.GOOS, url)
	if err := exec.Command(name, args...).Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

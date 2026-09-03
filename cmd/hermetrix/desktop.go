package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// errNoDesktopBrowser reports that no Chromium-family browser was found. It is
// a sentinel rather than a fatal error: `serve --desktop` degrades to the same
// default-browser handoff `--open` uses, because the server is the product and
// the window is a presentation choice.
var errNoDesktopBrowser = errors.New("no Chromium-family browser found for desktop mode")

// desktopWindowSize is the initial app window. It is deliberately smaller than
// a desktop display so the window still fits a 13" MacBook without the user
// having to resize it on first launch; the compact density breakpoint in the UI
// is what makes that width usable.
const desktopWindowSize = "1440,900"

// desktopCandidates lists the Chromium-family binaries to try, in preference
// order, for goos. env resolves environment variables so the Windows install
// locations can be expanded without the function reading the real environment,
// which is what keeps desktopCommand testable on any host.
func desktopCandidates(goos string, env func(string) string) []string {
	switch goos {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
	case "windows":
		var candidates []string
		for _, install := range []struct{ variable, suffix string }{
			{"ProgramFiles", `Google\Chrome\Application\chrome.exe`},
			{"ProgramFiles(x86)", `Google\Chrome\Application\chrome.exe`},
			{"LocalAppData", `Google\Chrome\Application\chrome.exe`},
			{"ProgramFiles", `Microsoft\Edge\Application\msedge.exe`},
			{"ProgramFiles(x86)", `Microsoft\Edge\Application\msedge.exe`},
		} {
			root := env(install.variable)
			if root == "" {
				continue
			}
			candidates = append(candidates, filepath.Join(root, install.suffix))
		}
		// PATH lookups last: an install location we can name is a stronger
		// signal than whatever a shell alias happens to resolve to.
		return append(candidates, "chrome.exe", "msedge.exe")
	default:
		return []string{
			"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
			"microsoft-edge", "brave-browser",
		}
	}
}

// desktopCommand returns the argv that opens url as its own application window,
// using the first candidate that exists reports present. ok is false when no
// candidate is installed, which is the caller's signal to fall back.
//
// The profile directory is per-install and separate from the user's everyday
// browser profile: desktop mode must not inherit that profile's extensions,
// cookies or session, and must not be evicted when the user clears it.
func desktopCommand(goos string, env func(string) string, exists func(string) bool,
	url, profileDir string) (name string, args []string, ok bool) {
	for _, candidate := range desktopCandidates(goos, env) {
		if !exists(candidate) {
			continue
		}
		return candidate, []string{
			"--user-data-dir=" + profileDir,
			"--window-size=" + desktopWindowSize,
			"--no-first-run",
			"--no-default-browser-check",
			// Last, and the only argument carrying the URL, so the address is
			// never positional and can never be read as a flag.
			"--app=" + url,
		}, true
	}
	return "", nil, false
}

// desktopBinaryExists answers whether a candidate from desktopCandidates is
// installed. Absolute paths are checked directly; bare names go through PATH.
func desktopBinaryExists(candidate string) bool {
	if filepath.IsAbs(candidate) {
		info, err := os.Stat(candidate)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(candidate)
	return err == nil
}

// openDesktopWindow launches the control center in its own window. It returns
// once the window has been started, not when it closes: the server outlives the
// window on purpose, so closing it never cancels an in-flight agent turn or a
// background job.
func openDesktopWindow(url, profileDir string) error {
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return fmt.Errorf("create desktop profile directory: %w", err)
	}
	name, args, ok := desktopCommand(runtime.GOOS, os.Getenv, desktopBinaryExists, url, profileDir)
	if !ok {
		return errNoDesktopBrowser
	}
	command := exec.Command(name, args...)
	if err := command.Start(); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	// Reap the window process so a long-lived server does not accumulate a
	// zombie for every desktop window it opened.
	go func() { _ = command.Wait() }()
	return nil
}

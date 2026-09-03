package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const desktopTestURL = "http://127.0.0.1:7331"

// TestDesktopCommandOpensAnApplicationWindowPerPlatform pins the argv shape.
// The URL must ride on --app= and be the final argument, the profile must be
// the one we passed rather than the user's everyday browser profile, and the
// first installed candidate must win on every platform.
func TestDesktopCommandOpensAnApplicationWindowPerPlatform(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "desktop-profile")
	env := func(name string) string {
		switch name {
		case "ProgramFiles":
			return `C:\Program Files`
		case "ProgramFiles(x86)":
			return `C:\Program Files (x86)`
		case "LocalAppData":
			return `C:\Users\dev\AppData\Local`
		}
		return ""
	}
	cases := map[string]struct {
		installed string
		want      string
	}{
		"darwin":  {"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"},
		"windows": {filepath.Join(`C:\Program Files`, `Google\Chrome\Application\chrome.exe`), filepath.Join(`C:\Program Files`, `Google\Chrome\Application\chrome.exe`)},
		"linux":   {"chromium", "chromium"},
		"freebsd": {"google-chrome", "google-chrome"},
	}
	for goos, want := range cases {
		name, args, ok := desktopCommand(goos, env, func(candidate string) bool {
			return candidate == want.installed
		}, desktopTestURL, profile)
		if !ok {
			t.Fatalf("desktopCommand(%q) found no browser although %q is installed", goos, want.installed)
		}
		if name != want.want {
			t.Errorf("desktopCommand(%q) = %q, want %q", goos, name, want.want)
		}
		if last := args[len(args)-1]; last != "--app="+desktopTestURL {
			t.Errorf("desktopCommand(%q) does not pass the URL last on --app=: %v", goos, args)
		}
		if !slices.Contains(args, "--user-data-dir="+profile) {
			t.Errorf("desktopCommand(%q) did not isolate the profile directory: %v", goos, args)
		}
		for _, argument := range args[:len(args)-1] {
			if strings.Contains(argument, desktopTestURL) {
				t.Errorf("desktopCommand(%q) repeated the URL outside --app=: %q", goos, argument)
			}
		}
	}
}

// TestDesktopCommandPrefersTheEarlierCandidate proves preference order is real
// and not an artefact of only one browser being installed.
func TestDesktopCommandPrefersTheEarlierCandidate(t *testing.T) {
	everythingInstalled := func(string) bool { return true }
	for goos, want := range map[string]string{
		"darwin":  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"windows": filepath.Join(`C:\PF`, `Google\Chrome\Application\chrome.exe`),
		"linux":   "google-chrome",
	} {
		name, _, ok := desktopCommand(goos, func(string) string { return `C:\PF` }, everythingInstalled,
			desktopTestURL, "profile")
		if !ok || name != want {
			t.Errorf("desktopCommand(%q) = %q (ok=%v), want %q", goos, name, ok, want)
		}
	}
}

// TestDesktopCommandReportsNoBrowserRatherThanGuessing keeps the fallback path
// honest: with nothing installed the caller must be told, not handed a command
// that will fail at exec time.
func TestDesktopCommandReportsNoBrowserRatherThanGuessing(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		name, args, ok := desktopCommand(goos, func(string) string { return "" },
			func(string) bool { return false }, desktopTestURL, "profile")
		if ok || name != "" || args != nil {
			t.Errorf("desktopCommand(%q) invented %q %v with no browser installed", goos, name, args)
		}
	}
}

// TestDesktopCandidatesSkipUnsetWindowsRoots stops an unset ProgramFiles(x86)
// from producing a relative path that PATH lookup could resolve elsewhere.
func TestDesktopCandidatesSkipUnsetWindowsRoots(t *testing.T) {
	candidates := desktopCandidates("windows", func(name string) string {
		if name == "ProgramFiles" {
			return `C:\Program Files`
		}
		return ""
	})
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, string(filepath.Separator)) {
			t.Errorf("candidate %q was built from an unset environment root", candidate)
		}
	}
	if !slices.Contains(candidates, "chrome.exe") || !slices.Contains(candidates, "msedge.exe") {
		t.Errorf("windows candidates lost their PATH fallbacks: %v", candidates)
	}
}

package main

import (
	"slices"
	"testing"
)

// TestBrowserCommandHandsTheURLToEachPlatformLauncher pins the launcher choice
// per OS. The URL must be the last argument on every platform so a value that
// starts with a dash cannot be read as a flag, and it must be passed as one
// argv entry rather than concatenated into a shell string.
func TestBrowserCommandHandsTheURLToEachPlatformLauncher(t *testing.T) {
	const url = "http://127.0.0.1:7331"
	cases := map[string]struct {
		name string
		args []string
	}{
		"darwin":  {"open", []string{url}},
		"windows": {"rundll32", []string{"url.dll,FileProtocolHandler", url}},
		"linux":   {"xdg-open", []string{url}},
		"freebsd": {"xdg-open", []string{url}},
	}
	for goos, want := range cases {
		name, args := browserCommand(goos, url)
		if name != want.name || !slices.Equal(args, want.args) {
			t.Errorf("browserCommand(%q) = %q %v, want %q %v", goos, name, args, want.name, want.args)
		}
		if args[len(args)-1] != url {
			t.Errorf("browserCommand(%q) does not pass the URL last: %v", goos, args)
		}
	}
}

package tools

import "testing"

func TestBrowserNeedsApproval(t *testing.T) {
	cases := []struct {
		action string
		url    string
		want   bool
	}{
		{"open", "http://localhost:8765/", false},
		{"open", "http://127.0.0.1:3000/app", false},
		{"open", "http://[::1]:5173/", false},
		{"navigate", "http://127.0.0.42:9/", false},
		{"open", "https://example.com/", true},
		{"navigate", "https://example.com/", true},
		{"open", "http://192.168.1.10/", true},
		{"open", "http://localhost.attacker.example/", true},
		{"open", "file:///etc/passwd", true},
		{"open", "not a url", true},
		{"open", "", true},
		{"read", "", false},
		{"click", "", false},
		{"type", "", false},
		{"capture", "", false},
		{"back", "", false},
		{"close", "", false},
		// Userinfo carries no bearing on where the browser actually goes, so it
		// cannot be allowed to change the answer either way. This row is the one
		// that actually distinguishes the parsed.User check from the hostname
		// check alone: the host here is 127.0.0.1 -- genuinely loopback -- so
		// without the userinfo guard this would read as approval-free.
		{"open", "http://attacker@127.0.0.1/", true},
		// Same guard, the other direction: a userinfo that looks like the
		// developer's own host does not make the real destination trustworthy.
		{"open", "http://localhost@evil.example/", true},
		// 0.0.0.0 is the "any address", not loopback -- it must still ask.
		{"open", "http://0.0.0.0/", true},
		// "127.1" is a legacy shorthand some C resolvers expand to 127.0.0.1,
		// but net.ParseIP correctly refuses it as a dotted-quad, so it falls
		// through to asking rather than being silently treated as loopback.
		{"open", "http://127.1/", true},
		// No host at all -- an empty Hostname() must not be confused with "".
		{"open", "http://:8080/", true},
		// net/url lowercases the scheme during parsing, so an uppercase scheme
		// still reads as http and the loopback host still reads as free.
		{"open", "HTTP://localhost/", false},
	}
	for _, item := range cases {
		if got := BrowserNeedsApproval(item.action, item.url); got != item.want {
			t.Errorf("BrowserNeedsApproval(%q, %q) = %v, want %v", item.action, item.url, got, item.want)
		}
	}
}

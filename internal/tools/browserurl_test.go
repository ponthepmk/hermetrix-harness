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
	}
	for _, item := range cases {
		if got := BrowserNeedsApproval(item.action, item.url); got != item.want {
			t.Errorf("BrowserNeedsApproval(%q, %q) = %v, want %v", item.action, item.url, got, item.want)
		}
	}
}

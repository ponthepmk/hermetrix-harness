package tools

import (
	"net"
	"net/url"
	"strings"
)

// BrowserNeedsApproval reports whether a browser action has to be approved
// before it runs. Only open and navigate carry a destination; the rest act on a
// tab the user already approved.
//
// Loopback is free because it is the developer's own dev server -- the thing
// they asked the agent to look at. Everything else is the open internet.
//
// The test is on the literal URL, with no name resolution. A hostname that
// resolves to loopback still asks. That is stricter than it strictly has to be,
// and stricter is the right direction: resolving here would mean a DNS lookup
// inside an approval decision, where a slow or hostile resolver decides whether
// the user gets asked.
func BrowserNeedsApproval(action, rawURL string) bool {
	action = strings.TrimSpace(action)
	if action != "open" && action != "navigate" {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" {
		// Anything unparsable, and every scheme other than http and https --
		// file:// included -- goes to the user rather than being guessed at.
		return true
	}
	if parsed.User != nil {
		return true
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return false
	}
	address := net.ParseIP(host)
	return address == nil || !address.IsLoopback()
}

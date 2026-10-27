package web

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// NewAuthenticatedLoopbackProxy adds HTTPS request boundaries and the existing
// session login to a running local instance without opening its data directory.
// The caller must serve this handler with TLS and expose it only on the tailnet.
func NewAuthenticatedLoopbackProxy(upstreamURL, publicHost, token, principal string) (http.Handler, error) {
	upstream, err := url.Parse(upstreamURL)
	if err != nil || upstream.Scheme != "http" || upstream.User != nil || upstream.RawQuery != "" ||
		upstream.Fragment != "" || (upstream.Path != "" && upstream.Path != "/") {
		return nil, fmt.Errorf("upstream must be a loopback HTTP origin")
	}
	ip := net.ParseIP(upstream.Hostname())
	if ip == nil || !ip.IsLoopback() || upstream.Port() == "" {
		return nil, fmt.Errorf("upstream must use a literal loopback IP and port")
	}
	if len(token) < 32 || strings.TrimSpace(principal) == "" || len(principal) > 120 {
		return nil, fmt.Errorf("proxy requires a token of at least 32 characters and a principal")
	}
	host, port, err := net.SplitHostPort(publicHost)
	if err != nil || host == "" || port == "" {
		return nil, fmt.Errorf("public host must include its port")
	}
	boundary, err := newRequestBoundary(net.JoinHostPort(host, port), true, nil)
	if err != nil {
		return nil, err
	}
	auth := newAuthenticator(token, principal, true)
	proxy := &httputil.ReverseProxy{
		FlushInterval: -1, // Preserve live chat events rather than buffer responses.
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(upstream)
			r.Out.Host = upstream.Host
			// The outer boundary already proved the browser origin. Translate it
			// to the origin accepted by the unchanged loopback backend.
			if r.In.Header.Get("Origin") != "" {
				r.Out.Header.Set("Origin", upstream.Scheme+"://"+upstream.Host)
			}
			r.Out.Header.Del("Authorization")
			r.Out.Header.Del("Cookie")
			r.Out.Header.Del("Forwarded")
			r.Out.Header.Del("Tailscale-User-Login")
			r.Out.Header.Del("Tailscale-User-Name")
			r.Out.Header.Del("Tailscale-User-Profile-Pic")
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "local Hermetrix instance is unavailable"})
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/session", auth.session)
	mux.Handle("/", proxy)
	return boundary.middleware(auth.middleware(mux), auth), nil
}

package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"

	"hermetrix-harness/internal/identity"
)

type requestBoundary struct {
	allowedHosts map[string]struct{}
	scheme       string
	listenerPort string
}

func newRequestBoundary(listenerAddress string, tls bool, trustedHosts []string) (*requestBoundary, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listenerAddress))
	if err != nil || strings.TrimSpace(port) == "" {
		return nil, fmt.Errorf("invalid HTTP listener address %q", listenerAddress)
	}
	host = strings.TrimSpace(host)
	boundary := &requestBoundary{allowedHosts: map[string]struct{}{}, scheme: "http", listenerPort: port}
	if tls {
		boundary.scheme = "https"
	}
	boundary.addHost(host, port)
	if isLoopbackHost(host) {
		boundary.addHost("localhost", port)
		boundary.addHost("127.0.0.1", port)
		boundary.addHost("::1", port)
	}
	for _, trusted := range trustedHosts {
		trusted = strings.TrimSpace(trusted)
		if trusted == "" {
			continue
		}
		trustedHost, trustedPort, splitErr := net.SplitHostPort(trusted)
		if splitErr != nil {
			if strings.Contains(trusted, ":") {
				return nil, fmt.Errorf("invalid trusted host %q: include brackets around IPv6 addresses", trusted)
			}
			trustedHost, trustedPort = trusted, port
		}
		if strings.TrimSpace(trustedHost) == "" || strings.TrimSpace(trustedPort) == "" {
			return nil, fmt.Errorf("invalid trusted host %q", trusted)
		}
		boundary.addHost(trustedHost, trustedPort)
	}
	return boundary, nil
}

func (b *requestBoundary) addHost(host, port string) {
	b.allowedHosts[net.JoinHostPort(canonicalHostname(host), port)] = struct{}{}
}

func canonicalHostname(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}

func isLoopbackHost(host string) bool {
	host = canonicalHostname(host)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func canonicalAuthority(authority, defaultPort string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(authority))
	if err != nil {
		if strings.Contains(authority, ":") {
			return "", fmt.Errorf("invalid host authority %q", authority)
		}
		host, port = authority, defaultPort
	}
	if canonicalHostname(host) == "" || strings.TrimSpace(port) == "" {
		return "", fmt.Errorf("invalid host authority %q", authority)
	}
	return net.JoinHostPort(canonicalHostname(host), port), nil
}

func (b *requestBoundary) middleware(next http.Handler, auth *authenticator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defaultPort := "80"
		if b.scheme == "https" {
			defaultPort = "443"
		}
		authority, err := canonicalAuthority(r.Host, defaultPort)
		if err != nil {
			writeBoundaryError(w, http.StatusMisdirectedRequest, "untrusted_host", "request host is not allowed")
			return
		}
		if _, allowed := b.allowedHosts[authority]; !allowed {
			writeBoundaryError(w, http.StatusMisdirectedRequest, "untrusted_host", "request host is not allowed")
			return
		}
		if isMutationMethod(r.Method) {
			if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
				writeBoundaryError(w, http.StatusForbidden, "cross_site_request", "cross-site mutations are not allowed")
				return
			}
			if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
				if !b.sameOrigin(origin, authority) {
					writeBoundaryError(w, http.StatusForbidden, "origin_mismatch", "mutation origin does not match the request host")
					return
				}
			} else if auth != nil && auth.hasValidCookie(r) && !auth.hasValidBearer(r) {
				fetchSite := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
				if fetchSite != "same-origin" && fetchSite != "none" {
					writeBoundaryError(w, http.StatusForbidden, "missing_browser_provenance", "cookie-authenticated mutations require same-origin fetch metadata")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (b *requestBoundary) sameOrigin(origin, requestAuthority string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != b.scheme || parsed.User != nil || parsed.Host == "" ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	defaultPort := "80"
	if b.scheme == "https" {
		defaultPort = "443"
	}
	originAuthority, err := canonicalAuthority(parsed.Host, defaultPort)
	return err == nil && originAuthority == requestAuthority
}

func isMutationMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func writeBoundaryError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{
		"code": code, "message": message, "request_id": identity.New("request"),
	}})
}
func decodeJSONLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !isJSONMediaType(mediaType) {
		writeBoundaryError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json or application/*+json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeBoundaryError(w, http.StatusRequestEntityTooLarge, "request_body_too_large", "JSON request body exceeds the endpoint limit")
			return false
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON: " + err.Error()})
		return false
	}
	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			writeBoundaryError(w, http.StatusBadRequest, "multiple_json_values", "request body must contain exactly one JSON document")
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON: " + err.Error()})
		}
		return false
	}
	return true
}

func isJSONMediaType(mediaType string) bool {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	return mediaType == "application/json" || (strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json"))
}

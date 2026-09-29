package product

import (
	"net"
	"net/url"
	"strings"

	"hermetrix-harness/internal/agentruntime"
)

// auditBrowserInspection classifies evidence the tab already collected. An
// absent response header is reported only when a document response was seen;
// an unavailable header set is never treated as a missing control.
func auditBrowserInspection(pageURL string, view agentruntime.BrowserInspection) agentruntime.BrowserSecurityAudit {
	audit := agentruntime.BrowserSecurityAudit{
		Scope:    "Passive review of the current page and responses observed since this tab opened. Findings are signals to verify, not confirmed exploitable vulnerabilities. No endpoint probes or form submissions were made.",
		Findings: []agentruntime.BrowserSecurityFinding{},
	}
	page, err := url.Parse(pageURL)
	if err != nil || page.Host == "" {
		return audit
	}
	add := func(id, severity, title, evidence, meaning string) {
		if len(audit.Findings) < 20 {
			audit.Findings = append(audit.Findings, agentruntime.BrowserSecurityFinding{
				ID: id, Severity: severity, Title: title, Evidence: evidence, Meaning: meaning})
		}
	}
	if page.Scheme == "http" && !isLoopbackBrowserHost(page.Hostname()) {
		add("page_http", "medium", "Page loaded without HTTPS", inspectionURL(pageURL),
			"The observed page URL uses plaintext HTTP. Confirm whether users can reach this route without a secure redirect.")
	}
	for _, form := range view.Forms {
		if !form.HasPassword {
			continue
		}
		action, err := url.Parse(form.Action)
		if err == nil && action.Scheme == "http" && !isLoopbackBrowserHost(action.Hostname()) {
			add("password_form_http", "medium", "Password form targets HTTP", form.Action,
				"The form action is plaintext. Client-side handlers may intercept submission, so verify the actual login request before calling it an exposure.")
		}
	}
	var document *agentruntime.BrowserInspectionTraffic
	wanted := inspectionURL(pageURL)
	for index := range view.Traffic {
		item := &view.Traffic[index]
		if strings.EqualFold(item.Type, "Document") && item.URL == wanted && item.Status >= 200 && item.Status < 400 {
			document = item
		}
	}
	if document == nil {
		return audit
	}
	headers := document.SecurityHeaders
	if headers["content-security-policy"] == "" {
		if headers["content-security-policy-report-only"] != "" {
			add("csp_report_only", "low", "CSP is report-only", wanted,
				"A reporting policy was observed, but no enforcing Content-Security-Policy header was captured for this document.")
		} else {
			add("csp_absent", "low", "No enforcing CSP header observed", wanted,
				"Review the document response and application needs before adding a Content-Security-Policy.")
		}
	}
	if page.Scheme == "https" && headers["strict-transport-security"] == "" {
		add("hsts_absent", "low", "No HSTS header observed", wanted,
			"The observed HTTPS document response did not set Strict-Transport-Security.")
	}
	if headers["x-content-type-options"] == "" {
		add("nosniff_absent", "low", "No nosniff header observed", wanted,
			"The observed document response did not set X-Content-Type-Options: nosniff.")
	}
	if headers["x-frame-options"] == "" && !strings.Contains(strings.ToLower(headers["content-security-policy"]), "frame-ancestors") &&
		!strings.HasSuffix(headers["content-security-policy"], "[truncated]") {
		add("frame_policy_absent", "low", "No framing restriction observed", wanted,
			"Neither X-Frame-Options nor an enforcing CSP frame-ancestors directive was captured for this document.")
	}
	return audit
}

func isLoopbackBrowserHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
